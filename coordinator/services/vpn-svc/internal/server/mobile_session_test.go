package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	pb "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/vpn/v1"
	"github.com/iogrid/iogrid/coordinator/services/vpn-svc/internal/store"
)

// seedMobilePeer registers a provider + its WG public key + a fresh
// srflx ICE candidate. The /v1/vpn/sessions/mobile handler needs all
// three to build a complete RequestMobileVpnSessionResponse.
func seedMobilePeer(t *testing.T, st store.Store, region string) uuid.UUID {
	t.Helper()
	mem, ok := st.(*store.Memory)
	if !ok {
		t.Fatalf("memory store assertion failed")
	}
	pid := uuid.New()
	mem.SeedProvider(pid, region, "healthy")
	// Seed the WG public key + an ICE candidate so lookupProvider can
	// resolve the endpoint.
	_ = st.RegisterProvider(context.Background(), &store.ProviderInfo{
		ID:          pid,
		Region:      region,
		Status:      "healthy",
		WgPublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	})
	_ = st.RegisterCandidates(context.Background(), pid, []*pb.IceCandidate{
		{
			Foundation:        "1",
			Component:         1,
			Transport:         "udp",
			Priority:          100,
			ConnectionAddress: "203.0.113.42",
			ConnectionPort:    51820,
			CandidateType:     "srflx",
		},
	})
	return pid
}

func TestMobileSession_HappyPath(t *testing.T) {
	srv, st := boot(t)
	seedMobilePeer(t, st, "us-east-1")

	req := map[string]interface{}{
		"customer_id":       uuid.New().String(),
		"region":            "us-east-1",
		"client_public_key": "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=",
	}
	resp, body := postJSON(t, srv.URL+"/v1/vpn/sessions/mobile", req)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("mobile session: status=%d body=%s", resp.StatusCode, body)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode body: %v body=%s", err, body)
	}
	// DoD #588: response must contain peer_public_key, peer_endpoint,
	// customer_inner_cidr, allowed_ips, dns_servers, session_id,
	// expires_at.
	for _, k := range []string{
		"session_id", "peer_public_key", "peer_endpoint",
		"customer_inner_cidr", "allowed_ips", "dns_servers",
		"expires_at", "region", "quota_state",
	} {
		if _, ok := got[k]; !ok {
			t.Errorf("response missing field %q (body=%s)", k, body)
		}
	}
	if got["allowed_ips"].(string) != "0.0.0.0/0" {
		t.Errorf("allowed_ips: want 0.0.0.0/0 got %v", got["allowed_ips"])
	}
	dns, ok := got["dns_servers"].([]interface{})
	if !ok || len(dns) != 2 {
		t.Errorf("dns_servers: want 2 entries got %v", got["dns_servers"])
	}
	if !strings.HasPrefix(got["customer_inner_cidr"].(string), "10.66.") {
		t.Errorf("customer_inner_cidr: want 10.66.x.y/32 prefix got %v", got["customer_inner_cidr"])
	}
	if !strings.HasSuffix(got["customer_inner_cidr"].(string), "/32") {
		t.Errorf("customer_inner_cidr: want /32 suffix got %v", got["customer_inner_cidr"])
	}
	if got["peer_endpoint"].(string) != "203.0.113.42:51820" {
		t.Errorf("peer_endpoint: want 203.0.113.42:51820 got %v", got["peer_endpoint"])
	}
}

func TestMobileSession_AutoRegion(t *testing.T) {
	srv, st := boot(t)
	seedMobilePeer(t, st, "eu-west-1")

	req := map[string]interface{}{
		"customer_id":       uuid.New().String(),
		"region":            "auto",
		"client_public_key": "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC=",
	}
	resp, body := postJSON(t, srv.URL+"/v1/vpn/sessions/mobile", req)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("mobile session auto: status=%d body=%s", resp.StatusCode, body)
	}
	var got map[string]interface{}
	_ = json.Unmarshal(body, &got)
	if got["region"].(string) != "eu-west-1" {
		t.Errorf("auto region: want eu-west-1 got %v", got["region"])
	}
}

func TestMobileSession_NoPeer_503WithRetryAfter(t *testing.T) {
	srv, _ := boot(t)
	// No providers seeded — picker should return ErrNoPeer.
	req := map[string]interface{}{
		"customer_id":       uuid.New().String(),
		"region":            "us-east-1",
		"client_public_key": "DDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD=",
	}
	resp, body := postJSON(t, srv.URL+"/v1/vpn/sessions/mobile", req)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("no peer: want 503 got %d body=%s", resp.StatusCode, body)
	}
	if ra := resp.Header.Get("Retry-After"); ra == "" {
		t.Error("Retry-After header missing on 503")
	}
}

func TestMobileSession_AcceptsPaymentAuthorizationWithoutValidating(t *testing.T) {
	srv, st := boot(t)
	seedMobilePeer(t, st, "us-east-1")

	req := map[string]interface{}{
		"customer_id":       uuid.New().String(),
		"region":            "us-east-1",
		"client_public_key": "EEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEE=",
		// Track 5 (#596) will validate this; for now any opaque
		// payload is accepted.
		"payment_authorization": map[string]interface{}{
			"wallet_address": "0xDEADBEEF",
			"signature":      "0xCAFEBABE",
			"expected_burn":  "1000000",
		},
	}
	resp, body := postJSON(t, srv.URL+"/v1/vpn/sessions/mobile", req)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("with payment_authorization: want 201 got %d body=%s", resp.StatusCode, body)
	}
}

func TestMobileSession_RejectsMissingClientPublicKey(t *testing.T) {
	srv, st := boot(t)
	seedMobilePeer(t, st, "us-east-1")

	req := map[string]interface{}{
		"customer_id": uuid.New().String(),
		"region":      "us-east-1",
		// client_public_key intentionally missing
	}
	resp, body := postJSON(t, srv.URL+"/v1/vpn/sessions/mobile", req)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing key: want 400 got %d body=%s", resp.StatusCode, body)
	}
}

func TestMobileSession_AllocatesUniqueInnerIPs(t *testing.T) {
	srv, st := boot(t)
	pid := seedMobilePeer(t, st, "us-east-1")

	innerIPs := map[string]struct{}{}
	for i := 0; i < 5; i++ {
		req := map[string]interface{}{
			"customer_id":       uuid.New().String(),
			"region":            "us-east-1",
			"client_public_key": uuid.New().String(),
		}
		resp, body := postJSON(t, srv.URL+"/v1/vpn/sessions/mobile", req)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("iter %d: status=%d body=%s", i, resp.StatusCode, body)
		}
		var got map[string]interface{}
		_ = json.Unmarshal(body, &got)
		ip := got["customer_inner_cidr"].(string)
		if _, dup := innerIPs[ip]; dup {
			t.Errorf("duplicate inner ip: %s", ip)
		}
		innerIPs[ip] = struct{}{}
	}
	// All 5 should share the same /24 prefix (provider's bucket).
	for ip := range innerIPs {
		want := "10.66." + bytePrefix(pid)
		if !strings.HasPrefix(ip, want) {
			t.Errorf("inner_ip %s missing expected /24 prefix %s", ip, want)
		}
	}
}

func bytePrefix(id uuid.UUID) string {
	// First UUID byte expanded into the third octet of 10.66.X.Y.
	return itoa(int(id[0])) + "."
}

func itoa(n int) string {
	return string(intToASCII(n))
}

func intToASCII(n int) []byte {
	if n == 0 {
		return []byte{'0'}
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return digits
}

func TestMobileHeartbeat_UpdatesByteCounters(t *testing.T) {
	srv, st := boot(t)
	seedMobilePeer(t, st, "us-east-1")

	// Create a mobile session to get a session ID.
	createReq := map[string]interface{}{
		"customer_id":       uuid.New().String(),
		"region":            "us-east-1",
		"client_public_key": "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF=",
	}
	cresp, cbody := postJSON(t, srv.URL+"/v1/vpn/sessions/mobile", createReq)
	if cresp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status=%d body=%s", cresp.StatusCode, cbody)
	}
	var created map[string]interface{}
	_ = json.Unmarshal(cbody, &created)
	sessionID := created["session_id"].(string)

	// Send heartbeat with byte counters.
	hbReq := map[string]interface{}{
		"bytes_in":                   uint64(12345),
		"bytes_out":                  uint64(67890),
		"last_handshake_age_seconds": 10,
		"path_latency_ms":            42,
	}
	buf := &bytes.Buffer{}
	_ = json.NewEncoder(buf).Encode(hbReq)
	resp, err := http.Post(srv.URL+"/v1/vpn/sessions/"+sessionID+"/heartbeat",
		"application/json", buf)
	if err != nil {
		t.Fatalf("heartbeat POST: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("heartbeat: status=%d body=%s", resp.StatusCode, body)
	}
	var ack map[string]interface{}
	if err := json.Unmarshal(body, &ack); err != nil {
		t.Fatalf("decode ack: %v", err)
	}
	if ack["session_id"].(string) != sessionID {
		t.Errorf("ack session_id mismatch: want %s got %v", sessionID, ack["session_id"])
	}
	if _, ok := ack["quota_state"]; !ok {
		t.Error("ack missing quota_state")
	}

	// Verify the bytes landed via GetSession.
	getResp, gerr := http.Get(srv.URL + "/v1/vpn/sessions/" + sessionID)
	if gerr != nil {
		t.Fatalf("get session: %v", gerr)
	}
	defer getResp.Body.Close()
	getBody, _ := io.ReadAll(getResp.Body)
	var sess map[string]interface{}
	_ = json.Unmarshal(getBody, &sess)
	if bi := sess["bytes_in"].(float64); uint64(bi) != 12345 {
		t.Errorf("bytes_in: want 12345 got %v", bi)
	}
	if bo := sess["bytes_out"].(float64); uint64(bo) != 67890 {
		t.Errorf("bytes_out: want 67890 got %v", bo)
	}
}

func TestMobileHeartbeat_RejectsUnknownSession(t *testing.T) {
	srv, _ := boot(t)
	buf := &bytes.Buffer{}
	_ = json.NewEncoder(buf).Encode(map[string]uint64{"bytes_in": 1, "bytes_out": 2})
	resp, err := http.Post(srv.URL+"/v1/vpn/sessions/"+uuid.New().String()+"/heartbeat",
		"application/json", buf)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown session: want 404 got %d", resp.StatusCode)
	}
}
