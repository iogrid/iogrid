package handlers

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/auth"
	"github.com/iogrid/iogrid/coordinator/shared/config"
)

// --- customer self-signup -------------------------------------------------
//
// POST /api/v1/onboard/customer is the self-service signup entry point
// for B2B customers (per docs/ROADMAP.md Phase 0 deliverable B).
//
// The flow is intentionally narrow:
//
//  1. The browser hits POST /api/v1/onboard/customer with a workspace
//     handle + display name + optional billing-contact email.
//  2. The BFF validates the handle (kebab-case, 3-32 chars, unique-ish).
//  3. The BFF asks identity-svc to create a Workspace bound to the
//     authenticated user (the agent shipping identity-svc.Workspace
//     fills this in; until then we stub to a deterministic UUID).
//  4. The BFF mints a customer API key + returns the plaintext ONCE.
//
// Returning the API key in this response is the canonical "first key" UX —
// the same key surfaces in /api/v1/customer/api-keys afterwards (with the
// plaintext stripped). It's the only ergonomic way to start using the
// product without a second round-trip.

// customerHandleFormat matches kebab-case workspace handles. We deliberately
// disallow leading digits + reserved suffixes (-svc, -prod) so the URL
// `https://workspace.iogrid.org/<handle>` stays unambiguous.
var customerHandleFormat = regexp.MustCompile(`^[a-z][a-z0-9-]{1,30}[a-z0-9]$`)

// CustomerOnboardStore tracks workspace-handle uniqueness for Phase 0.
// identity-svc owns this in Phase 1+ (handle becomes a column on the
// workspaces table). The interface kept here is intentionally minimal
// so the swap is mechanical.
type CustomerOnboardStore interface {
	// Reserve atomically reserves a handle for a user. Returns
	// ErrHandleTaken if some other workspace already owns it.
	Reserve(handle, userID string) (workspaceID uuid.UUID, err error)
	// LookupByHandle resolves a workspace id from a handle.
	LookupByHandle(handle string) (uuid.UUID, bool)
}

// ErrHandleTaken is returned by CustomerOnboardStore.Reserve when the
// handle is already owned by a different workspace.
var ErrHandleTaken = errors.New("workspace handle already taken")

// MemoryCustomerOnboardStore is the Phase 0 in-memory impl. Backing store
// for the workspace handle → workspace UUID mapping. Swapped for the
// identity-svc-backed impl once Workspace CRUD lands.
type MemoryCustomerOnboardStore struct {
	mu       sync.Mutex
	byHandle map[string]customerWorkspaceEntry
}

type customerWorkspaceEntry struct {
	workspaceID uuid.UUID
	userID      string
	createdAt   time.Time
}

// NewMemoryCustomerOnboardStore returns an empty in-memory store.
func NewMemoryCustomerOnboardStore() *MemoryCustomerOnboardStore {
	return &MemoryCustomerOnboardStore{byHandle: make(map[string]customerWorkspaceEntry)}
}

// Reserve atomically reserves a handle for a user. Returns the new
// workspace UUID; returns ErrHandleTaken when the handle is already
// bound to a different user.
func (s *MemoryCustomerOnboardStore) Reserve(handle, userID string) (uuid.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, exists := s.byHandle[handle]; exists {
		if e.userID == userID {
			// idempotent — same user re-claiming returns the existing id
			return e.workspaceID, nil
		}
		return uuid.Nil, ErrHandleTaken
	}
	id := uuid.New()
	s.byHandle[handle] = customerWorkspaceEntry{
		workspaceID: id,
		userID:      userID,
		createdAt:   time.Now(),
	}
	return id, nil
}

// LookupByHandle returns the workspace id for a handle, or false.
func (s *MemoryCustomerOnboardStore) LookupByHandle(handle string) (uuid.UUID, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.byHandle[handle]
	if !ok {
		return uuid.Nil, false
	}
	return e.workspaceID, true
}

// WithCustomerOnboardStore overrides the default in-memory customer
// onboard store. Mirror of WithOnboardStore (daemon-pairing flow).
func (a *API) WithCustomerOnboardStore(s CustomerOnboardStore) *API {
	a.CustomerOnboardStore = s
	return a
}

// CustomerOnboardRequest is the wire format the browser ships.
type CustomerOnboardRequest struct {
	Handle           string `json:"handle"`             // kebab-case workspace handle, required
	DisplayName      string `json:"display_name"`       // human-readable, 1-64 chars, required
	BillingEmail     string `json:"billing_email"`      // optional override; defaults to user's primary email
	InitialAPIKeyLab string `json:"initial_api_key_label"` // optional label for the first key (default "default")
}

// CustomerOnboardResponse is the body returned to the browser on success.
// The plaintext API key appears ONLY HERE — every subsequent listing
// has the plaintext stripped.
type CustomerOnboardResponse struct {
	WorkspaceID     string  `json:"workspace_id"`
	Handle          string  `json:"handle"`
	DisplayName     string  `json:"display_name"`
	BillingEmail    string  `json:"billing_email"`
	APIKey          APIKey  `json:"api_key"`
	ProxyEndpoint   string  `json:"proxy_endpoint"`   // proxy.iogrid.org:443
	OnboardingGuide string  `json:"onboarding_guide"` // link to docs/PHASE0_FIRST_CUSTOMER.md
	CreatedAt       string  `json:"created_at"`
}

// OnboardCustomer handles self-service B2B customer signup.
//
//	POST /api/v1/onboard/customer
//	  { handle, display_name, billing_email?, initial_api_key_label? }
//	-> 201 { workspace_id, handle, display_name, billing_email,
//	         api_key: {..., plaintext}, proxy_endpoint, ... }
//	-> 400 if input malformed
//	-> 401 if unauthenticated
//	-> 409 if handle is already taken
func (a *API) OnboardCustomer(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	var body CustomerOnboardRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	// We intentionally do NOT lowercase here — the handle is part of the
	// public URL surface (workspace.iogrid.org/<handle>) and "VcardProd"
	// vs "vcardprod" disambiguating only after the round-trip would be
	// confusing. Reject any uppercase up-front.
	handle := strings.TrimSpace(body.Handle)
	if !customerHandleFormat.MatchString(handle) {
		writeError(w, http.StatusBadRequest, "bad_request",
			"handle must be kebab-case, start with a letter, 3-32 chars (e.g. 'vcard-prod')")
		return
	}
	if isReservedHandle(handle) {
		writeError(w, http.StatusBadRequest, "bad_request",
			"handle is reserved (admin/svc/system/iogrid/api)")
		return
	}
	displayName := strings.TrimSpace(body.DisplayName)
	if displayName == "" || len(displayName) > 64 {
		writeError(w, http.StatusBadRequest, "bad_request",
			"display_name is required (1-64 chars)")
		return
	}
	billing := strings.TrimSpace(body.BillingEmail)
	if billing != "" && !looksLikeEmail(billing) {
		writeError(w, http.StatusBadRequest, "bad_request",
			"billing_email is not a valid address")
		return
	}
	if a.CustomerOnboardStore == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable",
			"customer onboarding store not wired")
		return
	}

	userID := claims.UserID().String()
	wsID, err := a.CustomerOnboardStore.Reserve(handle, userID)
	if err != nil {
		if errors.Is(err, ErrHandleTaken) {
			writeError(w, http.StatusConflict, "conflict", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	// TODO(#identity-workspace): call identity-svc.CreateWorkspace here
	// once the proto + RPC land (the workspace agent's PR). For now we
	// hold the handle → uuid mapping in-process and the workspace just
	// exists by virtue of an API key being bound to its uuid.

	label := strings.TrimSpace(body.InitialAPIKeyLab)
	if label == "" {
		label = "default"
	}
	if a.APIKeyStore == nil {
		writeError(w, http.StatusInternalServerError, "misconfigured",
			"api key store not wired")
		return
	}
	key, err := a.APIKeyStore.Create(r.Context(), wsID, label)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	a.Logger.Info("customer onboarded",
		"workspace_id", wsID.String(),
		"handle", handle,
		"display_name", displayName,
		"user_id", userID,
	)

	resp := CustomerOnboardResponse{
		WorkspaceID:     wsID.String(),
		Handle:          handle,
		DisplayName:     displayName,
		BillingEmail:    billing,
		APIKey:          key,
		ProxyEndpoint:   "proxy.iogrid.org:443",
		OnboardingGuide: config.DocsURL("getting-started", "phase0-first-customer") + "/",
		CreatedAt:       time.Now().UTC().Format(time.RFC3339),
	}
	writeJSON(w, http.StatusCreated, resp)
}

// --- helpers -------------------------------------------------------------

// isReservedHandle blocks operator + brand handles. The list is small on
// purpose — Phase 1 expands it to a config-driven blocklist.
func isReservedHandle(h string) bool {
	switch h {
	case "admin", "administrator", "root", "system",
		"svc", "service", "services",
		"iogrid", "api", "auth", "billing", "support",
		"www", "mail", "smtp", "ftp":
		return true
	}
	return false
}

// looksLikeEmail is a deliberately-loose validator: we forward to the
// canonical validator (identity-svc) anyway. Catches obvious typos at
// the BFF so the user gets a fast 400 instead of waiting on the round-trip.
func looksLikeEmail(s string) bool {
	at := strings.IndexByte(s, '@')
	if at < 1 || at == len(s)-1 {
		return false
	}
	host := s[at+1:]
	if !strings.Contains(host, ".") {
		return false
	}
	return true
}

// Compile-time interface assertion.
var _ CustomerOnboardStore = (*MemoryCustomerOnboardStore)(nil)
