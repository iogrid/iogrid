// Package handlers contains the HTTP endpoints the Next.js management
// plane talks to. They are organised by user-facing area:
//
//	account.go   — /api/v1/account/*  and /api/v1/me
//	provide.go   — /api/v1/provide/* (provider dashboard)
//	customer.go  — /api/v1/customer/* (B2B customer dashboard)
//	admin.go     — /api/v1/admin/* (iogrid staff)
//	vpn.go       — /api/v1/vpn/* (consumer VPN)
//
// All handlers depend ONLY on the per-service interfaces in
// internal/clients — never on the concrete Connect clients. This is
// what lets the unit tests pass mocks without an HTTP server.
package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/clients"
)

// API is the wired HTTP surface. Constructed once at process boot and
// passed to chi.Router.Mount().
type API struct {
	Clients     *clients.Set
	Logger      *slog.Logger
	APIKeyStore APIKeyStore
	// OnboardStore backs the daemon-onboarding handshake handlers in
	// onboard.go. Optional — when nil the onboarding endpoints respond
	// 503 onboard_store_unavailable. Override with WithOnboardStore.
	OnboardStore OnboardStore
	// CustomerOnboardStore backs POST /api/v1/onboard/customer (self-service
	// B2B signup). Optional — when nil the endpoint responds 503
	// customer_onboard_store_unavailable. Override with
	// WithCustomerOnboardStore. Phase 0 deliverable B per docs/ROADMAP.md.
	CustomerOnboardStore CustomerOnboardStore
	// VPNGateway is the HTTP proxy to the consumer-VPN microservice.
	// Optional — when nil the /api/v1/vpn/config-for-platform endpoint
	// responds 503 vpn_gateway_unavailable.
	VPNGateway *VPNGatewayProxy
	// Workspaces is the proxy to identity-svc's WorkspaceService.
	// Optional — when nil the /api/v1/workspaces tree returns 503.
	Workspaces WorkspaceClient
	// Transparency backs the quarterly transparency-report endpoints.
	// Optional — when nil the /status/transparency endpoints respond
	// 503 transparency_store_unavailable. The default wiring in main.go
	// installs a MemoryTransparencyStore so the BFF can accept the
	// CronJob POST without extra configuration.
	Transparency TransparencyStore
	// updates is the in-memory backing store for the auto-update
	// preferences + state surfaced at /api/v1/account/updates*. Lazy-
	// initialised on first request via ensureUpdatesStore. Issue #59.
	updates     *updatesStore
	updatesOnce sync.Once
}

// New constructs an API. logger defaults to slog.Default(). store
// defaults to an in-memory implementation suitable for tests + Phase 0.
func New(c *clients.Set, store APIKeyStore, logger *slog.Logger) *API {
	if logger == nil {
		logger = slog.Default()
	}
	if store == nil {
		store = NewMemoryAPIKeyStore()
	}
	return &API{Clients: c, Logger: logger, APIKeyStore: store}
}

// --- JSON helpers --------------------------------------------------------

type errResp struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errResp{Code: code, Message: msg})
}

// decodeJSON deserialises the request body into out. Returns an
// HTTP-status-appropriate error envelope on failure.
func decodeJSON(r *http.Request, out any) error {
	if r.Body == nil {
		return errors.New("empty body")
	}
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(out)
}

// httpStatusForConnect maps a connect.Error code to the HTTP status
// our REST clients expect. For non-connect errors we return 500.
func httpStatusForConnect(err error) int {
	if err == nil {
		return http.StatusOK
	}
	var cerr *connect.Error
	for cur := err; cur != nil; {
		if c, ok := cur.(*connect.Error); ok {
			cerr = c
			break
		}
		type unwrap interface{ Unwrap() error }
		u, ok := cur.(unwrap)
		if !ok {
			break
		}
		cur = u.Unwrap()
	}
	if cerr == nil {
		return http.StatusInternalServerError
	}
	switch cerr.Code() {
	case connect.CodeInvalidArgument, connect.CodeOutOfRange, connect.CodeFailedPrecondition:
		return http.StatusBadRequest
	case connect.CodeUnauthenticated:
		return http.StatusUnauthorized
	case connect.CodePermissionDenied:
		return http.StatusForbidden
	case connect.CodeNotFound:
		return http.StatusNotFound
	case connect.CodeAlreadyExists, connect.CodeAborted:
		return http.StatusConflict
	case connect.CodeResourceExhausted:
		return http.StatusTooManyRequests
	case connect.CodeUnavailable:
		return http.StatusServiceUnavailable
	case connect.CodeDeadlineExceeded:
		return http.StatusGatewayTimeout
	case connect.CodeUnimplemented:
		return http.StatusNotImplemented
	default:
		return http.StatusInternalServerError
	}
}

// writeUpstreamError translates a downstream error into the matching
// REST status + JSON envelope.
func writeUpstreamError(w http.ResponseWriter, err error) {
	status := httpStatusForConnect(err)
	code := "upstream_error"
	switch status {
	case http.StatusBadRequest:
		code = "bad_request"
	case http.StatusUnauthorized:
		code = "unauthenticated"
	case http.StatusForbidden:
		code = "forbidden"
	case http.StatusNotFound:
		code = "not_found"
	case http.StatusConflict:
		code = "conflict"
	case http.StatusTooManyRequests:
		code = "rate_limited"
	case http.StatusServiceUnavailable:
		code = "unavailable"
	case http.StatusGatewayTimeout:
		code = "timeout"
	case http.StatusNotImplemented:
		code = "unimplemented"
	}
	writeError(w, status, code, err.Error())
}

// parseUUIDParam pulls a UUID URL param and writes a 400 if it's
// missing or malformed. Returns (id, true) on success.
func parseUUIDParam(w http.ResponseWriter, raw string, name string) (uuid.UUID, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		writeError(w, http.StatusBadRequest, "bad_request", name+" required")
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", name+" must be a UUID")
		return uuid.Nil, false
	}
	return id, true
}
