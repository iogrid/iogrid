// rest_pair.go implements the legacy REST shim for daemon pairing.
//
// The Rust daemon's iogrid-transport identity module POSTs JSON to
// `/api/v1/providers/pair` (see daemon/crates/transport/src/identity.rs).
// The canonical Connect-RPC handler lives under the
// `/iogrid.providers.v1.ProviderRegistrationService/PairDaemon` path
// (mounted by routes.go).
//
// This shim translates the daemon's lean PairingRequest JSON shape
// into the richer providersv1.PairDaemonRequest proto, invokes the
// in-process Connect handler, and projects the response back into the
// daemon's PairingResponse JSON shape.
//
// Keeping the shim in-process (rather than as a separate caller against
// the Connect endpoint) avoids a self-loopback HTTP hop and keeps a
// single source of truth for pairing — the RegistrationHandler.
package handlers

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"

	"connectrpc.com/connect"

	providersv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1"
)

// pairingRESTRequest mirrors the daemon's PairingRequest serde shape.
type pairingRESTRequest struct {
	PairingToken string `json:"pairing_token"`
	// CSRPem is the daemon-generated PKCS#10 PEM. As of #235 this
	// field is REQUIRED: the daemon now mints its own ECDSA-P256
	// keypair locally via rcgen and ships only the CSR over the wire.
	// providers-svc parses the CSR, verifies its self-signature, and
	// signs the embedded public key — it never generates a keypair on
	// behalf of the daemon (doing so would force the daemon to trust a
	// server-side private key it never sees, the original #235 bug).
	CSRPem string `json:"csr_pem"`
	// Optional richer fields the daemon may start populating later.
	DisplayName string `json:"display_name,omitempty"`
	// DaemonPublicKeyB64 is base64-std-encoded SPKI DER. RETAINED for
	// back-compat with older test harnesses + the Connect-RPC path —
	// when the JSON shim receives a CSR, the SPKI is extracted from it
	// and this field is ignored.
	DaemonPublicKeyB64 string `json:"daemon_public_key_b64,omitempty"`
}

// pairingRESTResponse mirrors the daemon's PairingResponse serde shape.
type pairingRESTResponse struct {
	CertPEM     string `json:"cert_pem"`
	ProviderID  string `json:"provider_id"`
	ServerCAPem string `json:"server_ca_pem"`
}

// PairDaemonREST exposes the Connect PairDaemon RPC over a flat JSON
// REST endpoint at /api/v1/providers/pair. Returns 4xx on validation
// errors and 5xx on internal failures; the body is always a
// {"error":"..."} envelope on failure.
func (h *RegistrationHandler) PairDaemonREST(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var in pairingRESTRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeJSONError(w, http.StatusBadRequest, "malformed json: "+err.Error())
		return
	}
	if in.PairingToken == "" {
		writeJSONError(w, http.StatusBadRequest, "pairing_token required")
		return
	}

	// #235: the daemon now generates its own keypair locally and ships
	// the CSR. Extract the embedded public key + verify the CSR's
	// self-signature so the issued cert binds to a key the daemon
	// actually holds.
	//
	// The legacy daemon_public_key_b64 fallback path is preserved only
	// when csr_pem is absent so that older test fixtures (and the
	// Connect-RPC handler tests) keep working — production builds of
	// the daemon always send csr_pem.
	var pubKey []byte
	switch {
	case in.CSRPem != "":
		spki, err := publicKeyFromCSRPEM(in.CSRPem)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "csr_pem: "+err.Error())
			return
		}
		pubKey = spki
	case in.DaemonPublicKeyB64 != "":
		var err error
		pubKey, err = base64.StdEncoding.DecodeString(in.DaemonPublicKeyB64)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "daemon_public_key_b64 must be base64")
			return
		}
		if len(pubKey) == 0 {
			writeJSONError(w, http.StatusBadRequest, "daemon_public_key_b64 was empty")
			return
		}
	default:
		// Hard error: silently fabricating a keypair server-side is
		// exactly the #235 bug. Require the daemon to supply one path
		// or the other.
		writeJSONError(w, http.StatusBadRequest,
			"csr_pem required (daemon must generate its own keypair); "+
				"daemon_public_key_b64 accepted as legacy fallback")
		return
	}

	req := &providersv1.PairDaemonRequest{
		PairingToken:    in.PairingToken,
		DaemonPublicKey: pubKey,
		DisplayName:     in.DisplayName,
	}

	resp, err := h.PairDaemon(r.Context(), connect.NewRequest(req))
	if err != nil {
		var ce *connect.Error
		if errors.As(err, &ce) {
			writeJSONError(w, connectCodeToHTTP(ce.Code()), ce.Message())
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	out := pairingRESTResponse{
		CertPEM:     string(resp.Msg.GetDaemonCertificate()),
		ProviderID:  resp.Msg.GetProvider().GetId().GetValue(),
		ServerCAPem: string(resp.Msg.GetServerCaBundle()),
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

// publicKeyFromCSRPEM parses a PEM-encoded PKCS#10 CSR, verifies the
// embedded self-signature, and returns the DER-encoded SubjectPublicKey
// (SPKI) ready to hand to ca.IssueDaemonCert. Returns a descriptive
// error on any malformed input so the daemon-side log makes the cause
// obvious; the bytes are NEVER consumed unverified — a CSR whose
// signature does not match its public key is rejected.
func publicKeyFromCSRPEM(pemText string) ([]byte, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, errors.New("not a PEM block")
	}
	if block.Type != "CERTIFICATE REQUEST" && block.Type != "NEW CERTIFICATE REQUEST" {
		return nil, fmt.Errorf("unexpected PEM type %q; want CERTIFICATE REQUEST", block.Type)
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("verify self-signature: %w", err)
	}
	spki, err := x509.MarshalPKIXPublicKey(csr.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("marshal SPKI: %w", err)
	}
	return spki, nil
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// connectCodeToHTTP maps Connect codes to the closest HTTP status the
// daemon (which only speaks REST) understands.
func connectCodeToHTTP(c connect.Code) int {
	switch c {
	case connect.CodeInvalidArgument:
		return http.StatusBadRequest
	case connect.CodeUnauthenticated:
		return http.StatusUnauthorized
	case connect.CodePermissionDenied:
		return http.StatusForbidden
	case connect.CodeNotFound:
		return http.StatusNotFound
	case connect.CodeAlreadyExists:
		return http.StatusConflict
	case connect.CodeResourceExhausted:
		return http.StatusTooManyRequests
	case connect.CodeFailedPrecondition:
		return http.StatusPreconditionFailed
	case connect.CodeDeadlineExceeded:
		return http.StatusGatewayTimeout
	case connect.CodeUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
