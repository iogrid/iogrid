package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/store"
)

// BuildGatewayForwarder pushes iOS-build status updates back to the
// build-gateway's internal callback API. The daemon reports build progress on
// the dispatch stream as WorkloadStatusUpdate frames; workloads-svc persists
// them (its own store) AND — for iOS builds — forwards them to the
// build-gateway so the customer-facing build record (status, exit code, SSE
// log tail) advances. Without this hop the build-gateway only ever knows
// "dispatched" and never "running"/"succeeded".
//
// The seam is the workload's "build_id" label, which the build-gateway stamps
// on every submission (see build-gateway workloadclient ConnectClient).
type BuildGatewayForwarder interface {
	// ForwardStatus POSTs a status transition to the build-gateway for the
	// given build id. providerID is the daemon that ran the attempt — the
	// gateway records it so a finished build can be metered to the right
	// provider's earnings (#744); "" when unknown. Best-effort: a forwarding
	// error is logged by the caller, never fails the daemon's stream.
	ForwardStatus(ctx context.Context, buildID, providerID, status, note string, exitCode int32) error
}

// HTTPBuildGatewayForwarder is the production BuildGatewayForwarder: it POSTs
// to {baseURL}/internal/v1/builds/{id}/status with the shared dispatch token.
type HTTPBuildGatewayForwarder struct {
	// BaseURL is the build-gateway internal base (e.g.
	// "http://build-gateway.iogrid.svc.cluster.local:8080"). No trailing slash.
	BaseURL string
	// DispatchToken is the shared secret guarding the build-gateway internal
	// routes (X-Iogrid-Dispatch-Token). May be empty in dev.
	DispatchToken string
	// Client is the HTTP client; nil uses a 5s-timeout default.
	Client *http.Client
}

// NewHTTPBuildGatewayForwarder wires a forwarder. Returns a true nil interface
// when baseURL is empty so callers can treat "not configured" as "no
// forwarding" with a plain `if fwd != nil` check (avoiding the typed-nil-in-
// interface trap).
func NewHTTPBuildGatewayForwarder(baseURL, dispatchToken string) BuildGatewayForwarder {
	if baseURL == "" {
		return nil
	}
	return &HTTPBuildGatewayForwarder{
		BaseURL:       baseURL,
		DispatchToken: dispatchToken,
		Client:        &http.Client{Timeout: 5 * time.Second},
	}
}

type buildGatewayStatusBody struct {
	Status     string `json:"status"`
	Note       string `json:"note,omitempty"`
	ExitCode   int32  `json:"exit_code,omitempty"`
	ProviderID string `json:"provider_id,omitempty"`
}

// ForwardStatus implements BuildGatewayForwarder.
func (f *HTTPBuildGatewayForwarder) ForwardStatus(ctx context.Context, buildID, providerID, status, note string, exitCode int32) error {
	if f == nil {
		return nil
	}
	client := f.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	url := fmt.Sprintf("%s/internal/v1/builds/%s/status", f.BaseURL, buildID)
	raw, err := json.Marshal(buildGatewayStatusBody{Status: status, Note: note, ExitCode: exitCode, ProviderID: providerID})
	if err != nil {
		return fmt.Errorf("build-gateway forward: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("build-gateway forward: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if f.DispatchToken != "" {
		req.Header.Set("X-Iogrid-Dispatch-Token", f.DispatchToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("build-gateway forward: post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		// 409 (invalid transition) and 404 (unknown build) are non-fatal —
		// the build-gateway may have already advanced or cancelled the
		// build. Surface the code so the caller can log at the right level.
		return fmt.Errorf("build-gateway forward: status %d", resp.StatusCode)
	}
	return nil
}

// buildIDFromWorkload returns the build-gateway build id stamped on the
// workload's labels at submission time, or "" if this isn't a gateway-
// originated iOS build.
func buildIDFromWorkload(w *store.Workload) string {
	if w == nil || w.Labels == nil {
		return ""
	}
	return w.Labels["build_id"]
}
