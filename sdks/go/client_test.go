package iogrid_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	iogrid "github.com/iogrid/go-sdk"
)

// newTestServer wires an in-memory HTTP test server and a Client
// pointed at it. The handler is invoked for every SDK call so
// assertions can inspect headers / paths / bodies.
func newTestServer(t *testing.T, handler http.HandlerFunc) (*iogrid.Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := iogrid.NewClient(iogrid.Options{APIKey: "iog_test", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c, srv
}

func TestNewClient_RequiresAPIKey(t *testing.T) {
	if _, err := iogrid.NewClient(iogrid.Options{}); err == nil {
		t.Fatal("expected error when APIKey is empty")
	}
}

func TestCreateWorkload(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/workloads" {
			t.Errorf("path = %s, want /v1/workloads", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer iog_test" {
			t.Errorf("auth header = %q, want Bearer iog_test", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content-type = %q", got)
		}
		var body iogrid.CreateWorkloadRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Type != iogrid.WorkloadTypeBandwidth {
			t.Errorf("type = %s", body.Type)
		}
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(iogrid.Workload{ID: "w1", WorkspaceID: "ws", Type: "BANDWIDTH", Status: "queued"})
	})
	got, err := c.CreateWorkload(context.Background(), iogrid.CreateWorkloadRequest{
		Type:      iogrid.WorkloadTypeBandwidth,
		Bandwidth: &iogrid.BandwidthRequest{TargetURL: "https://example.com"},
	})
	if err != nil {
		t.Fatalf("CreateWorkload: %v", err)
	}
	if got.ID != "w1" {
		t.Errorf("id = %q", got.ID)
	}
}

func TestGetWorkload_EncodesPath(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workloads/abc%2Fdef" && r.URL.EscapedPath() != "/v1/workloads/abc%2Fdef" {
			t.Errorf("escaped path = %s", r.URL.EscapedPath())
		}
		_ = json.NewEncoder(w).Encode(iogrid.GetWorkloadResponse{Workload: iogrid.Workload{ID: "abc/def"}})
	})
	r, err := c.GetWorkload(context.Background(), "abc/def")
	if err != nil {
		t.Fatalf("GetWorkload: %v", err)
	}
	if r.Workload.ID != "abc/def" {
		t.Errorf("id = %q", r.Workload.ID)
	}
}

func TestListWorkloads_QueryParams(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("pageSize") != "50" || q.Get("type") != "DOCKER" {
			t.Errorf("query = %v", q)
		}
		if q.Has("status") {
			t.Errorf("unexpected status= in query")
		}
		_ = json.NewEncoder(w).Encode(iogrid.ListWorkloadsResponse{})
	})
	if _, err := c.ListWorkloads(context.Background(), iogrid.ListWorkloadsOptions{PageSize: 50, Type: iogrid.WorkloadTypeDocker}); err != nil {
		t.Fatalf("ListWorkloads: %v", err)
	}
}

func TestCancelWorkload_Reason(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s", r.Method)
		}
		if got := r.URL.Query().Get("reason"); got != "user requested" {
			t.Errorf("reason = %q", got)
		}
		_ = json.NewEncoder(w).Encode(iogrid.Workload{ID: "w1", Status: "cancelled"})
	})
	got, err := c.CancelWorkload(context.Background(), "w1", "user requested")
	if err != nil {
		t.Fatalf("CancelWorkload: %v", err)
	}
	if got.Status != "cancelled" {
		t.Errorf("status = %q", got.Status)
	}
}

func TestDeleteAPIKey_204(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	})
	if err := c.DeleteAPIKey(context.Background(), "k1"); err != nil {
		t.Fatalf("DeleteAPIKey: %v", err)
	}
}

func TestListAPIKeys(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(iogrid.ListAPIKeysResponse{
			Keys: []iogrid.APIKeyMetadata{{ID: "k1", Name: "ci", Prefix: "iog_abcd"}},
		})
	})
	keys, err := c.ListAPIKeys(context.Background())
	if err != nil {
		t.Fatalf("ListAPIKeys: %v", err)
	}
	if len(keys) != 1 || keys[0].Prefix != "iog_abcd" {
		t.Errorf("keys = %+v", keys)
	}
}

func TestError_OnNon2xx(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		_ = json.NewEncoder(w).Encode(iogrid.ErrorEnvelope{
			Code: "INVALID_ARGUMENT", Message: "bad target", FieldPath: "bandwidth.targetUrl", RequestID: "req-123",
		})
	})
	_, err := c.CreateWorkload(context.Background(), iogrid.CreateWorkloadRequest{Type: iogrid.WorkloadTypeBandwidth})
	if err == nil {
		t.Fatal("expected error")
	}
	var ie *iogrid.Error
	if !errors.As(err, &ie) {
		t.Fatalf("err is %T, want *iogrid.Error", err)
	}
	if ie.Status != 400 || ie.Code != "INVALID_ARGUMENT" || ie.FieldPath != "bandwidth.targetUrl" || ie.RequestID != "req-123" {
		t.Errorf("err = %+v", ie)
	}
}

func TestStreamWorkloadEvents(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		for _, status := range []string{"queued", "running", "succeeded"} {
			ev := iogrid.WorkloadEvent{WorkloadID: "w1", NewStatus: status, OccurredAt: time.Now().UTC()}
			b, _ := json.Marshal(ev)
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(b)
			_, _ = w.Write([]byte("\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
		}
	})
	events, errs, err := c.StreamWorkloadEvents(context.Background(), "w1")
	if err != nil {
		t.Fatalf("StreamWorkloadEvents: %v", err)
	}
	var got []string
	for ev := range events {
		got = append(got, ev.NewStatus)
	}
	if e := <-errs; e != nil {
		t.Fatalf("stream errored: %v", e)
	}
	want := []string{"queued", "running", "succeeded"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestStreamWorkloadEvents_4xx(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		_ = json.NewEncoder(w).Encode(iogrid.ErrorEnvelope{Code: "NOT_FOUND", Message: "x"})
	})
	_, _, err := c.StreamWorkloadEvents(context.Background(), "nope")
	if err == nil {
		t.Fatal("expected error")
	}
	var ie *iogrid.Error
	if !errors.As(err, &ie) || ie.Code != "NOT_FOUND" {
		t.Errorf("err = %+v", err)
	}
}

func TestRequestMobileSession(t *testing.T) {
	expiresAt := time.Date(2026, 6, 4, 0, 0, 0, 0, time.UTC)
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/vpn/sessions/mobile" {
			t.Errorf("path = %s, want /v1/vpn/sessions/mobile", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer iog_test" {
			t.Errorf("auth header = %q", got)
		}
		var body iogrid.RequestMobileSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.CustomerID != "11111111-2222-3333-4444-555555555555" {
			t.Errorf("CustomerID = %q", body.CustomerID)
		}
		if body.ClientPublicKey != "wg-pubkey-b64" {
			t.Errorf("ClientPublicKey = %q", body.ClientPublicKey)
		}
		if body.Region != "auto" {
			t.Errorf("Region = %q", body.Region)
		}
		// Verify snake_case on the wire by inspecting the raw query of the
		// already-decoded struct fields via re-encode.
		raw, _ := json.Marshal(body)
		if !strings.Contains(string(raw), `"client_public_key"`) {
			t.Errorf("expected client_public_key in body, got %s", raw)
		}
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(iogrid.RequestMobileSessionResponse{
			SessionID:         "sess-1",
			PeerPublicKey:     "peer-pubkey-b64",
			PeerEndpoint:      "203.0.113.7:51820",
			CustomerInnerCIDR: "10.244.7.4/32",
			AllowedIPs:        "0.0.0.0/0",
			DNSServers:        []string{"1.1.1.1", "1.0.0.1"},
			Region:            "eu-central",
			ExpiresAt:         expiresAt,
			QuotaState:        iogrid.QuotaStateHealthy,
		})
	})
	got, err := c.RequestMobileSession(context.Background(), iogrid.RequestMobileSessionRequest{
		CustomerID:      "11111111-2222-3333-4444-555555555555",
		Region:          "auto",
		ClientPublicKey: "wg-pubkey-b64",
	})
	if err != nil {
		t.Fatalf("RequestMobileSession: %v", err)
	}
	if got.SessionID != "sess-1" {
		t.Errorf("SessionID = %q", got.SessionID)
	}
	if got.PeerEndpoint != "203.0.113.7:51820" {
		t.Errorf("PeerEndpoint = %q", got.PeerEndpoint)
	}
	if got.QuotaState != iogrid.QuotaStateHealthy {
		t.Errorf("QuotaState = %q", got.QuotaState)
	}
	if !got.ExpiresAt.Equal(expiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, expiresAt)
	}
	if len(got.DNSServers) != 2 || got.DNSServers[0] != "1.1.1.1" {
		t.Errorf("DNSServers = %v", got.DNSServers)
	}
}

func TestRequestMobileSession_503NoPeer(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "15")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"no_peer_available","detail":"no healthy peer","region":"us-west","retry_after_sec":15}`))
	})
	_, err := c.RequestMobileSession(context.Background(), iogrid.RequestMobileSessionRequest{
		CustomerID:      "11111111-2222-3333-4444-555555555555",
		ClientPublicKey: "wg-pubkey-b64",
	})
	if err == nil {
		t.Fatal("expected 503 error")
	}
	var ie *iogrid.Error
	if !errors.As(err, &ie) || ie.Status != 503 {
		t.Errorf("err = %+v, want Status=503", err)
	}
}

func TestRequestMobileSession_ValidatesRequiredFields(t *testing.T) {
	c, err := iogrid.NewClient(iogrid.Options{APIKey: "iog_test", BaseURL: "http://invalid"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.RequestMobileSession(context.Background(), iogrid.RequestMobileSessionRequest{
		ClientPublicKey: "wg",
	}); err == nil {
		t.Error("expected error when CustomerID empty")
	}
	if _, err := c.RequestMobileSession(context.Background(), iogrid.RequestMobileSessionRequest{
		CustomerID: "x",
	}); err == nil {
		t.Error("expected error when ClientPublicKey empty")
	}
}

func TestUserAgentHeader(t *testing.T) {
	var seenUA string
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		seenUA = r.Header.Get("User-Agent")
		_ = json.NewEncoder(w).Encode(iogrid.ListWorkloadsResponse{})
	})
	if _, err := c.ListWorkloads(context.Background(), iogrid.ListWorkloadsOptions{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(seenUA, "iogrid-sdk-go/") {
		t.Errorf("ua = %q", seenUA)
	}
}

func TestRetryAfterSeconds(t *testing.T) {
	e := &iogrid.Error{Metadata: map[string]string{"retry_after_seconds": "12"}}
	got, ok := iogrid.RetryAfterSeconds(e)
	if !ok || got != 12 {
		t.Errorf("got=%d ok=%v", got, ok)
	}
	_, ok = iogrid.RetryAfterSeconds(errors.New("not iogrid"))
	if ok {
		t.Error("expected ok=false for non-iogrid error")
	}
}
