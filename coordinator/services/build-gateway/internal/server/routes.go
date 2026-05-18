// Package server is the HTTP surface of the build-gateway microservice.
//
// Customer-facing iOS-CI gateway: receives build jobs, schedules to Mac
// providers, exposes status / logs / artifacts. Mounted on :8080 in front
// of build.iogrid.org.
//
// Routes:
//
//	POST   /v1/builds                      submit a new iOS build
//	GET    /v1/builds                      list recent builds for caller's workspace
//	GET    /v1/builds/{id}                 status + artifact metadata
//	GET    /v1/builds/{id}/logs            SSE stream of stdout/stderr
//	DELETE /v1/builds/{id}                 cancel a running build
//	POST   /v1/builds/{id}/artifacts       INTERNAL — provider uploads an artifact
//	GET    /v1/builds/{id}/artifacts/{name} pre-signed S3 download URL
//	GET    /v1/xcode-versions              discovery — approved Xcode toolchains
//
// /v1/builds/{id}/artifacts (POST) is internal — see auth.go for the
// dispatch-token check. Every other route requires a customer API key.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/auth"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/builds"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/store"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/workloadclient"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/xcode"
)

// Deps bundles the runtime dependencies the handlers need. Pre-wired
// instances are passed in by main(); tests pass synthetic ones.
type Deps struct {
	Service       *builds.Service
	Validator     auth.Validator
	DispatchToken string // shared-secret guarding internal/artifacts
	Logger        *slog.Logger
}

// New builds a Mount function bound to deps. The returned function is what
// gets handed to sharedserver.Run via Options.Mount.
func New(deps Deps) func(r chi.Router) {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	h := &handlers{deps: deps}
	return h.mount
}

// Mount is the simple zero-deps variant kept for backwards compatibility
// with the build-gateway scaffold. It serves only the stub index handler;
// production wiring uses New() above.
func Mount(r chi.Router) {
	r.Route("/v1", func(r chi.Router) {
		r.Get("/", indexHandler)
	})
}

// indexHandler returns a stable JSON envelope identifying the service.
func indexHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"service": "build-gateway",
		"status":  "stub",
	})
}

// --- handler struct ---------------------------------------------------------

type handlers struct {
	deps Deps
}

func (h *handlers) mount(r chi.Router) {
	r.Get("/v1/xcode-versions", h.listXcodeVersions)

	// Customer-facing subtree under /v1/builds — wraps the auth middleware.
	r.Route("/v1/builds", func(r chi.Router) {
		// Internal upload route is mounted FIRST so its
		// dispatch-token middleware sees the request before the
		// customer API-key middleware would 401 it.
		r.Group(func(r chi.Router) {
			r.Use(h.dispatchTokenMiddleware)
			r.Post("/{id}/artifacts", h.uploadArtifact)
		})

		r.Group(func(r chi.Router) {
			r.Use(auth.Middleware(h.deps.Validator, writeError))
			r.Post("/", h.submitBuild)
			r.Get("/", h.listBuilds)
			r.Get("/{id}", h.getBuild)
			r.Delete("/{id}", h.cancelBuild)
			r.Get("/{id}/logs", h.streamLogs)
			r.Get("/{id}/artifacts/{name}", h.presignArtifact)
		})
	})

	// Bare /v1/ root: surface-area introspection for service-discovery
	// probes and the stub envelope that the scaffold tests already pin.
	r.Get("/v1", indexHandler)
	r.Get("/v1/", indexHandler)
}

// --- POST /v1/builds --------------------------------------------------------

type submitBody struct {
	GitURL        string            `json:"git_url"`
	GitRef        string            `json:"git_ref"`
	XcodeVersion  string            `json:"xcode_version,omitempty"`
	BuildCommand  string            `json:"build_command"`
	SigningTeamID string            `json:"signing_team_id,omitempty"`
	EnvVars       map[string]string `json:"env_vars,omitempty"`
	WebhookURL    string            `json:"webhook_url,omitempty"`
	WebhookSecret string            `json:"webhook_secret,omitempty"`
}

type submitResponse struct {
	BuildID   string         `json:"build_id"`
	Status    builds.Status  `json:"status"`
	StatusURL string         `json:"status_url"`
	LogsURL   string         `json:"logs_url"`
	Build     *builds.Build  `json:"build"`
}

func (h *handlers) submitBuild(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no_identity", "identity missing")
		return
	}
	var body submitBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	// Premium-tier gate on webhooks — only "pro" and "enterprise" plans
	// may register a webhook target.
	if body.WebhookURL != "" && id.Plan != "pro" && id.Plan != "enterprise" {
		writeError(w, http.StatusForbidden, "plan_required",
			"webhooks require a pro or enterprise plan")
		return
	}
	req := builds.SubmitRequest{
		GitURL:        body.GitURL,
		GitRef:        body.GitRef,
		XcodeVersion:  body.XcodeVersion,
		BuildCommand:  body.BuildCommand,
		SigningTeamID: body.SigningTeamID,
		EnvVars:       body.EnvVars,
		WebhookURL:    body.WebhookURL,
		WebhookSecret: body.WebhookSecret,
	}
	b, err := h.deps.Service.Submit(r.Context(), id.WorkspaceID, id.UserID, id.Plan, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	resp := submitResponse{
		BuildID:   b.ID,
		Status:    b.Status,
		StatusURL: fmt.Sprintf("/v1/builds/%s", b.ID),
		LogsURL:   fmt.Sprintf("/v1/builds/%s/logs", b.ID),
		Build:     redact(b),
	}
	writeJSON(w, http.StatusAccepted, resp)
}

// --- GET /v1/builds (list) --------------------------------------------------

type listResponse struct {
	Builds []*builds.Build `json:"builds"`
	Count  int             `json:"count"`
}

func (h *handlers) listBuilds(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.IdentityFrom(r.Context())
	f := builds.ListFilter{WorkspaceID: id.WorkspaceID}
	if s := r.URL.Query().Get("status"); s != "" {
		f.Status = builds.Status(s)
	}
	if limStr := r.URL.Query().Get("limit"); limStr != "" {
		if n, err := strconv.Atoi(limStr); err == nil && n > 0 {
			f.Limit = n
		}
	}
	bs, err := h.deps.Service.List(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	out := make([]*builds.Build, 0, len(bs))
	for _, b := range bs {
		out = append(out, redact(b))
	}
	writeJSON(w, http.StatusOK, listResponse{Builds: out, Count: len(out)})
}

// --- GET /v1/builds/{id} ----------------------------------------------------

func (h *handlers) getBuild(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.IdentityFrom(r.Context())
	buildID := chi.URLParam(r, "id")
	b, err := h.deps.Service.Get(r.Context(), id.WorkspaceID, buildID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, redact(b))
}

// --- DELETE /v1/builds/{id} -------------------------------------------------

func (h *handlers) cancelBuild(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.IdentityFrom(r.Context())
	buildID := chi.URLParam(r, "id")
	reason := r.URL.Query().Get("reason")
	if reason == "" {
		reason = "customer_requested"
	}
	b, err := h.deps.Service.Cancel(r.Context(), id.WorkspaceID, buildID, reason)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, redact(b))
}

// --- GET /v1/builds/{id}/logs (SSE) -----------------------------------------

func (h *handlers) streamLogs(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.IdentityFrom(r.Context())
	buildID := chi.URLParam(r, "id")
	// Tenancy check: 404 if the caller doesn't own the build.
	if _, err := h.deps.Service.Get(r.Context(), id.WorkspaceID, buildID); err != nil {
		writeServiceError(w, err)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "no_flusher", "response writer cannot stream")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Determine resume point — clients use the standard Last-Event-ID
	// header.
	var fromSeq uint64
	if lid := r.Header.Get("Last-Event-ID"); lid != "" {
		if n, err := strconv.ParseUint(lid, 10, 64); err == nil {
			fromSeq = n + 1
		}
	}
	broker := h.deps.Service.LogHub().For(buildID)

	// Replay history.
	for _, line := range broker.Snapshot(fromSeq) {
		writeSSE(w, line)
	}
	flusher.Flush()

	// Tail live lines.
	ch := make(chan builds.LogLine, 64)
	cancel := broker.Subscribe(ch)
	defer cancel()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case <-heartbeat.C:
			// SSE comment line keeps intermediaries from closing the
			// connection.
			_, _ = io.WriteString(w, ": keepalive\n\n")
			flusher.Flush()
		case line, ok := <-ch:
			if !ok {
				// Build terminal — write a final event so the
				// client knows to stop, then exit.
				_, _ = io.WriteString(w, "event: end\ndata: {}\n\n")
				flusher.Flush()
				return
			}
			writeSSE(w, line)
			flusher.Flush()
		}
	}
}

// --- POST /v1/builds/{id}/artifacts (internal) ------------------------------

type uploadResponse struct {
	BuildID  string `json:"build_id"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Bucket   string `json:"bucket"`
	Key      string `json:"key"`
	Recorded bool   `json:"recorded"`
}

func (h *handlers) uploadArtifact(w http.ResponseWriter, r *http.Request) {
	buildID := chi.URLParam(r, "id")
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing_name", "?name=... required")
		return
	}
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	defer r.Body.Close()
	updated, obj, err := h.deps.Service.UploadArtifact(r.Context(), buildID, name, contentType, r.Body)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, uploadResponse{
		BuildID:  updated.ID,
		Name:     name,
		Size:     obj.Size,
		Bucket:   obj.Bucket,
		Key:      obj.Key,
		Recorded: true,
	})
}

// --- GET /v1/builds/{id}/artifacts/{name} -----------------------------------

type presignResponse struct {
	BuildID   string    `json:"build_id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (h *handlers) presignArtifact(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.IdentityFrom(r.Context())
	buildID := chi.URLParam(r, "id")
	name := chi.URLParam(r, "name")
	url, err := h.deps.Service.PresignArtifact(r.Context(), id.WorkspaceID, buildID, name)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, presignResponse{
		BuildID:   buildID,
		Name:      name,
		URL:       url,
		ExpiresAt: time.Now().Add(15 * time.Minute),
	})
}

// --- GET /v1/xcode-versions -------------------------------------------------

func (h *handlers) listXcodeVersions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"versions": xcode.Approved(),
		"default":  xcode.DefaultVersion,
	})
}

// --- dispatch token guard ---------------------------------------------------

// dispatchTokenMiddleware gates internal endpoints. Provider daemons /
// workloads-svc reach these with the dispatch token configured at deploy
// time (BUILD_GATEWAY_DISPATCH_TOKEN). If the gateway is started without a
// token set, internal endpoints are exposed without auth — this is intended
// only for local dev / unit tests.
func (h *handlers) dispatchTokenMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.deps.DispatchToken == "" {
			next.ServeHTTP(w, r)
			return
		}
		got := r.Header.Get("X-Iogrid-Dispatch-Token")
		if got == "" || got != h.deps.DispatchToken {
			writeError(w, http.StatusUnauthorized, "bad_dispatch_token",
				"missing or invalid X-Iogrid-Dispatch-Token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- helpers ----------------------------------------------------------------

// writeJSON serialises v as JSON with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError emits a stable error envelope. All gateway errors share this
// shape so customer-side clients can branch on `code`.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{
		"error":   code,
		"message": message,
	})
}

// writeServiceError translates Service-layer errors into HTTP responses.
func writeServiceError(w http.ResponseWriter, err error) {
	var ve *builds.ErrValidation
	switch {
	case errors.As(err, &ve):
		writeError(w, http.StatusBadRequest, "validation_failed", ve.Error())
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "build not found")
	case errors.Is(err, builds.ErrInvalidTransition):
		writeError(w, http.StatusConflict, "invalid_transition", err.Error())
	case errors.Is(err, workloadclient.ErrAlreadyTerminal):
		writeError(w, http.StatusConflict, "already_terminal", "build already terminal")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
	}
}

func decodeJSON(r *http.Request, into any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(into)
}

// writeSSE emits a single LogLine as an SSE event. The Seq is used as the
// SSE `id:` so reconnecting clients can resume via Last-Event-ID.
func writeSSE(w http.ResponseWriter, line builds.LogLine) {
	payload, err := json.Marshal(line)
	if err != nil {
		return
	}
	// Sanitise text — SSE forbids CR/LF in data: lines but JSON-encoded
	// payloads can't contain a bare newline anyway.
	body := strings.ReplaceAll(string(payload), "\n", " ")
	_, _ = fmt.Fprintf(w, "id: %d\nevent: log\ndata: %s\n\n", line.Seq, body)
}

// redact returns a shallow copy of b with secret-bearing fields cleared so
// they never leak back over JSON. Webhook.Secret is the canonical example.
func redact(b *builds.Build) *builds.Build {
	if b == nil {
		return nil
	}
	clone := *b
	if clone.Webhook != nil {
		w := *clone.Webhook
		w.Secret = ""
		clone.Webhook = &w
	}
	return &clone
}
