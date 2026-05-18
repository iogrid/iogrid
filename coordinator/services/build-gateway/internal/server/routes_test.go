package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/auth"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/builds"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/s3artifact"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/server"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/store"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/webhook"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/workloadclient"
)

func newTestServer(t *testing.T) (*httptest.Server, *testHooks) {
	t.Helper()
	st := store.NewInMemory(nil)
	disp := workloadclient.NewInMemory(nil)
	storage := s3artifact.NewInMemory(nil, "")
	rec := webhook.NewRecorder()
	hub := builds.NewLogHub(64)
	svc := builds.NewService(builds.Options{
		Store:      st,
		Dispatcher: disp,
		Storage:    storage,
		Webhooks:   rec,
		Logs:       hub,
	})
	v := auth.NewStaticValidator()
	v.Add("api-key-free", auth.Identity{WorkspaceID: "ws-free", Plan: "free"})
	v.Add("api-key-pro", auth.Identity{WorkspaceID: "ws-pro", Plan: "pro"})
	v.Add("api-key-pro-2", auth.Identity{WorkspaceID: "ws-pro-2", Plan: "pro"})

	r := chi.NewRouter()
	mount := server.New(server.Deps{
		Service:       svc,
		Validator:     v,
		DispatchToken: "secret-dispatch-token",
	})
	mount(r)
	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)
	return ts, &testHooks{svc: svc, disp: disp, hub: hub, recorder: rec}
}

type testHooks struct {
	svc      *builds.Service
	disp     *workloadclient.InMemory
	hub      *builds.LogHub
	recorder *webhook.Recorder
}

func doReq(t *testing.T, ts *httptest.Server, method, path, apiKey, body string, headers map[string]string) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, ts.URL+path, rdr)
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do req: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func decodeBody[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return v
}

func validBody() string {
	return `{
		"git_url": "https://github.com/example/ios-app.git",
		"git_ref": "main",
		"xcode_version": "16.2",
		"build_command": "xcodebuild -scheme App archive"
	}`
}

func TestRoutes_RequireAuth(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := doReq(t, ts, http.MethodPost, "/v1/builds", "", validBody(), nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestRoutes_RejectsUnknownKey(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := doReq(t, ts, http.MethodPost, "/v1/builds", "nope", validBody(), nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestSubmit_Accepts(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := doReq(t, ts, http.MethodPost, "/v1/builds", "api-key-free", validBody(), nil)
	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 202, got %d body=%s", resp.StatusCode, body)
	}
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["build_id"] == nil || got["build_id"] == "" {
		t.Fatalf("missing build_id: %+v", got)
	}
	if got["status_url"] == nil {
		t.Fatalf("missing status_url: %+v", got)
	}
}

func TestSubmit_RejectsBadJSON(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := doReq(t, ts, http.MethodPost, "/v1/builds", "api-key-free", `{"unknown_field": 1}`, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestSubmit_FreeTierCannotRegisterWebhook(t *testing.T) {
	ts, _ := newTestServer(t)
	body := `{
		"git_url": "https://github.com/example/ios-app.git",
		"git_ref": "main",
		"build_command": "xcodebuild",
		"webhook_url": "https://example.com/hook",
		"webhook_secret": "a-long-enough-secret-1234"
	}`
	resp := doReq(t, ts, http.MethodPost, "/v1/builds", "api-key-free", body, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestSubmit_ProTierWebhookOk(t *testing.T) {
	ts, hooks := newTestServer(t)
	body := `{
		"git_url": "https://github.com/example/ios-app.git",
		"git_ref": "main",
		"build_command": "xcodebuild",
		"webhook_url": "https://example.com/hook",
		"webhook_secret": "a-long-enough-secret-1234"
	}`
	resp := doReq(t, ts, http.MethodPost, "/v1/builds", "api-key-pro", body, nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}
	// Wait briefly for webhook event enqueue (synchronous in our test
	// recorder, but be defensive).
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && len(hooks.recorder.Events()) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if len(hooks.recorder.Events()) == 0 {
		t.Fatal("expected webhook event recorded")
	}
}

func TestSubmit_WebhookSecretNotEchoedBack(t *testing.T) {
	ts, _ := newTestServer(t)
	body := `{
		"git_url": "https://github.com/example/ios-app.git",
		"git_ref": "main",
		"build_command": "xcodebuild",
		"webhook_url": "https://example.com/hook",
		"webhook_secret": "a-long-enough-secret-1234"
	}`
	resp := doReq(t, ts, http.MethodPost, "/v1/builds", "api-key-pro", body, nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	if bytes.Contains(raw, []byte("a-long-enough-secret-1234")) {
		t.Fatalf("response leaked webhook secret: %s", raw)
	}
}

func TestGetBuild_TenancyScoped(t *testing.T) {
	ts, _ := newTestServer(t)
	// Create a build under ws-pro.
	resp := doReq(t, ts, http.MethodPost, "/v1/builds", "api-key-pro", validBody(), nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("submit: %d", resp.StatusCode)
	}
	var sb map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&sb); err != nil {
		t.Fatalf("decode: %v", err)
	}
	id, _ := sb["build_id"].(string)
	if id == "" {
		t.Fatal("missing build_id")
	}
	// Own workspace can read.
	resp2 := doReq(t, ts, http.MethodGet, "/v1/builds/"+id, "api-key-pro", "", nil)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp2.StatusCode)
	}
	// Different workspace gets 404.
	resp3 := doReq(t, ts, http.MethodGet, "/v1/builds/"+id, "api-key-pro-2", "", nil)
	if resp3.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 cross-tenant, got %d", resp3.StatusCode)
	}
}

func TestList_WorkspaceIsolated(t *testing.T) {
	ts, _ := newTestServer(t)
	for i := 0; i < 2; i++ {
		_ = doReq(t, ts, http.MethodPost, "/v1/builds", "api-key-pro", validBody(), nil)
	}
	_ = doReq(t, ts, http.MethodPost, "/v1/builds", "api-key-pro-2", validBody(), nil)

	resp := doReq(t, ts, http.MethodGet, "/v1/builds", "api-key-pro", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var got struct {
		Builds []map[string]any `json:"builds"`
		Count  int              `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Count != 2 {
		t.Fatalf("expected 2 builds, got %d", got.Count)
	}
}

func TestCancel_HappyPath(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := doReq(t, ts, http.MethodPost, "/v1/builds", "api-key-pro", validBody(), nil)
	var sb map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&sb)
	id, _ := sb["build_id"].(string)
	resp2 := doReq(t, ts, http.MethodDelete, "/v1/builds/"+id, "api-key-pro", "", nil)
	if resp2.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp2.StatusCode)
	}
}

func TestArtifactUpload_RequiresDispatchToken(t *testing.T) {
	ts, _ := newTestServer(t)
	// Submit so we have a real build id.
	resp := doReq(t, ts, http.MethodPost, "/v1/builds", "api-key-pro", validBody(), nil)
	var sb map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&sb)
	id, _ := sb["build_id"].(string)

	// No token → 401.
	resp2 := doReq(t, ts, http.MethodPost, "/v1/builds/"+id+"/artifacts?name=App.ipa", "", "BINARY", nil)
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp2.StatusCode)
	}
	// Bad token → 401.
	resp3 := doReq(t, ts, http.MethodPost, "/v1/builds/"+id+"/artifacts?name=App.ipa", "", "BINARY",
		map[string]string{"X-Iogrid-Dispatch-Token": "wrong"})
	if resp3.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp3.StatusCode)
	}
	// Good token → 201.
	resp4 := doReq(t, ts, http.MethodPost, "/v1/builds/"+id+"/artifacts?name=App.ipa", "", "BINARY",
		map[string]string{"X-Iogrid-Dispatch-Token": "secret-dispatch-token"})
	if resp4.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp4.Body)
		t.Fatalf("expected 201, got %d body=%s", resp4.StatusCode, body)
	}
}

func TestArtifact_PresignFlow(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := doReq(t, ts, http.MethodPost, "/v1/builds", "api-key-pro", validBody(), nil)
	var sb map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&sb)
	id, _ := sb["build_id"].(string)

	upResp := doReq(t, ts, http.MethodPost, "/v1/builds/"+id+"/artifacts?name=App.ipa", "", "BINARY-PAYLOAD",
		map[string]string{"X-Iogrid-Dispatch-Token": "secret-dispatch-token"})
	if upResp.StatusCode != http.StatusCreated {
		t.Fatalf("upload: %d", upResp.StatusCode)
	}
	pre := doReq(t, ts, http.MethodGet, "/v1/builds/"+id+"/artifacts/App.ipa", "api-key-pro", "", nil)
	if pre.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", pre.StatusCode)
	}
	var pr struct {
		URL       string    `json:"url"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(pre.Body).Decode(&pr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(pr.URL, "https://") {
		t.Fatalf("bad url: %s", pr.URL)
	}
	if pr.ExpiresAt.Before(time.Now()) {
		t.Fatalf("expires_at in the past: %s", pr.ExpiresAt)
	}
}

func TestArtifact_CrossTenantBlocked(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := doReq(t, ts, http.MethodPost, "/v1/builds", "api-key-pro", validBody(), nil)
	var sb map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&sb)
	id, _ := sb["build_id"].(string)
	_ = doReq(t, ts, http.MethodPost, "/v1/builds/"+id+"/artifacts?name=App.ipa", "", "BIN",
		map[string]string{"X-Iogrid-Dispatch-Token": "secret-dispatch-token"})
	pre := doReq(t, ts, http.MethodGet, "/v1/builds/"+id+"/artifacts/App.ipa", "api-key-pro-2", "", nil)
	if pre.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 cross-tenant, got %d", pre.StatusCode)
	}
}

func TestLogs_SSEStream(t *testing.T) {
	ts, hooks := newTestServer(t)
	resp := doReq(t, ts, http.MethodPost, "/v1/builds", "api-key-pro", validBody(), nil)
	var sb map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&sb)
	id, _ := sb["build_id"].(string)

	// Push log lines BEFORE the stream connects; SSE replay should
	// surface them.
	hooks.svc.AppendLog(id, "stdout", "alpha")
	hooks.svc.AppendLog(id, "stdout", "beta")

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/builds/"+id+"/logs", nil)
	req.Header.Set("Authorization", "Bearer api-key-pro")
	httpResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get logs: %v", err)
	}
	defer httpResp.Body.Close()
	if got := httpResp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("expected SSE content type, got %s", got)
	}
	// Read a small chunk — the two replayed lines should be in the
	// first read.
	buf := make([]byte, 4096)
	n, _ := httpResp.Body.Read(buf)
	body := string(buf[:n])
	if !strings.Contains(body, `"text":"alpha"`) || !strings.Contains(body, `"text":"beta"`) {
		t.Fatalf("expected replayed lines in SSE body: %s", body)
	}
}

func TestXcodeVersions_Endpoint(t *testing.T) {
	ts, _ := newTestServer(t)
	resp, err := http.Get(ts.URL + "/v1/xcode-versions")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	versions, ok := got["versions"].([]any)
	if !ok || len(versions) == 0 {
		t.Fatalf("expected versions array, got %+v", got)
	}
	if got["default"] != "latest" {
		t.Fatalf("expected default=latest, got %v", got["default"])
	}
}

func TestStub_BackwardsCompatMountStill(t *testing.T) {
	// The server.Mount entrypoint (no deps) survives so the smoke-test
	// envelope clients (and the proxy-gateway scaffold parity test)
	// don't break when wiring the real handler.
	r := chi.NewRouter()
	server.Mount(r)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "build-gateway") {
		t.Fatalf("expected service name in body, got %s", w.Body.String())
	}
}

// Sanity coverage helpers — keep coverage broad for the auth + webhook
// modules that aren't directly hit by HTTP tests.

func TestAuthHeaders_Extract(t *testing.T) {
	cases := []struct {
		name string
		set  func(*http.Request)
		want string
	}{
		{"bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer abc") }, "abc"},
		{"x-iogrid", func(r *http.Request) { r.Header.Set("X-Iogrid-Api-Key", "xyz") }, "xyz"},
		{"none", func(_ *http.Request) {}, ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			tc.set(req)
			if got := auth.ExtractAPIKey(req); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestWebhookSig_RoundTrip(t *testing.T) {
	body := []byte(`{"hello":"world"}`)
	sig := webhook.SignBody("supersecret", body)
	if !webhook.VerifySignatureHeader("supersecret", sig, body) {
		t.Fatal("signature verify should succeed")
	}
	if webhook.VerifySignatureHeader("other", sig, body) {
		t.Fatal("signature verify should fail with wrong secret")
	}
}

// ensure no goroutine leak in heartbeat path
func TestLogs_HeartbeatCancellation(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := doReq(t, ts, http.MethodPost, "/v1/builds", "api-key-pro", validBody(), nil)
	var sb map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&sb)
	id, _ := sb["build_id"].(string)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/v1/builds/%s/logs", ts.URL, id), nil)
	req.Header.Set("Authorization", "Bearer api-key-pro")
	httpResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	cancel()
	io.Copy(io.Discard, httpResp.Body)
	httpResp.Body.Close()
}
