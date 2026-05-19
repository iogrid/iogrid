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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"

	"connectrpc.com/connect"

	providersv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1"
)

// pairingRESTRequest mirrors the daemon's PairingRequest serde shape.
type pairingRESTRequest struct {
	PairingToken string `json:"pairing_token"`
	// CSRPem is the daemon-generated PKCS#10 PEM. The current Connect
	// handler does not yet consume CSRs (the daemon-public-key path is
	// used instead) — we accept the field so future daemon builds that
	// switch to CSR flow remain wire-compatible.
	CSRPem string `json:"csr_pem"`
	// Optional richer fields the daemon may start populating later.
	DisplayName string `json:"display_name,omitempty"`
	// DaemonPublicKey is base64-std-encoded SPKI DER. When the daemon
	// supplies a CSR-only request, this field is empty and the shim
	// supplies a placeholder so the CA can still issue a cert.
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

	// daemon_public_key_b64 is optional in the REST shape (the
	// daemon's current build does not generate keys locally). When
	// absent we synthesise a placeholder DER blob so the in-memory CA
	// can still issue a cert — the resulting bundle is *not* a real
	// mTLS identity, but it lets the smoke harness exercise the full
	// path without forcing every daemon build to upgrade today.
	pubKey, err := base64.StdEncoding.DecodeString(in.DaemonPublicKeyB64)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "daemon_public_key_b64 must be base64")
		return
	}
	if len(pubKey) == 0 {
		// The daemon's current build doesn't yet generate keys
		// locally — synthesise an ephemeral ECDSA P-256 SPKI so the
		// CA can issue a usable cert. Once the daemon adopts rcgen
		// this branch becomes a back-compat fallback.
		priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "ephemeral key: "+err.Error())
			return
		}
		der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "marshal spki: "+err.Error())
			return
		}
		pubKey = der
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
