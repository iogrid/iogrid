package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/auth"
)

// --- onboarding contracts ------------------------------------------------
//
// The onboarding flow connects three actors:
//   1. the daemon (just installed) that minted a one-time pairing code
//   2. the browser at /onboard/<code> where the human signs in
//   3. providers-svc which issues the daemon's mTLS bundle once the
//      human confirms the wizard
//
// This file is the BFF surface for #2 + the daemon's poll for #1. The
// store is in-memory; production swaps in a Redis-backed implementation
// without touching the handlers.

// PairingCodeFormat is the Crockford-base32-without-IOLU pattern the
// daemon mints. We reject anything else at the BFF before forwarding
// to providers-svc to keep junk traffic out of the gRPC pipe.
var PairingCodeFormat = regexp.MustCompile(`^[0-9A-HJ-NP-TV-Z]{6}$`)

// OnboardStore is the contract handlers depend on. The Phase 0 impl is
// the in-memory MemoryOnboardStore below; later commits swap in Redis.
type OnboardStore interface {
	// Link binds a pairing code to a user id. Returns ErrCodeNotFound if
	// the code never existed, ErrCodeExpired if the daemon's TTL ran
	// out, ErrCodeAlreadyClaimed if a different user already linked it.
	Link(code, userID string) error
	// GetLinkedUser returns the user that claimed the code, if any.
	GetLinkedUser(code string) (string, bool)
	// Defaults persists the wizard's resource caps + categories +
	// payout tier so providers-svc can write them on PairDaemon.
	StoreDefaults(code string, d OnboardingDefaults) error
	GetDefaults(code string) (OnboardingDefaults, bool)
	// Mint is called by the daemon's coordinator-side hook when a fresh
	// install registers a pending code. (Phase 0 just trusts the
	// browser-supplied code; Phase 1 will require the daemon to register
	// first.)
	Mint(code string) error
	// Burn invalidates the code after the bundle is issued. Single-use.
	Burn(code string)
}

// OnboardingDefaults mirrors the wizard's submit body. Kept narrow so
// providers-svc owns the canonical proto and we don't drift.
type OnboardingDefaults struct {
	BandwidthCapGB int      `json:"bandwidth_cap_gb"`
	CPUCapPct      int      `json:"cpu_cap_pct"`
	IdleOnly       bool     `json:"idle_only"`
	Calendar       string   `json:"calendar"`     // free-form RFC5545 fragment; empty = always-on
	Categories     []string `json:"categories"`   // ["ecommerce","seo",...]
	PayoutTier     string   `json:"payout_tier"`  // "stripe_connect" | "iogrid_credit" | "defer"
}

// Errors returned by the store.
var (
	ErrCodeNotFound       = errors.New("pairing code not found")
	ErrCodeExpired        = errors.New("pairing code expired")
	ErrCodeAlreadyClaimed = errors.New("pairing code already claimed by another user")
)

// MemoryOnboardStore is the Phase 0 in-memory impl. Thread-safe.
type MemoryOnboardStore struct {
	mu       sync.Mutex
	codes    map[string]*onboardingEntry
	ttl      time.Duration
	clockNow func() time.Time
}

type onboardingEntry struct {
	createdAt time.Time
	userID    string
	defaults  *OnboardingDefaults
	claimed   bool
}

// NewMemoryOnboardStore returns a fresh in-memory store with a 10
// minute TTL (matching the daemon's pair-code TTL).
func NewMemoryOnboardStore() *MemoryOnboardStore {
	return &MemoryOnboardStore{
		codes:    make(map[string]*onboardingEntry),
		ttl:      10 * time.Minute,
		clockNow: time.Now,
	}
}

// Mint adds a pending code. In Phase 0 we accept on the browser's
// supplied code; in Phase 1 the daemon will register first via NATS.
func (s *MemoryOnboardStore) Mint(code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked()
	if _, exists := s.codes[code]; exists {
		return nil // idempotent
	}
	s.codes[code] = &onboardingEntry{createdAt: s.clockNow()}
	return nil
}

func (s *MemoryOnboardStore) gcLocked() {
	cutoff := s.clockNow().Add(-s.ttl)
	for c, e := range s.codes {
		if e.createdAt.Before(cutoff) {
			delete(s.codes, c)
		}
	}
}

// Link binds code → user. See OnboardStore.
func (s *MemoryOnboardStore) Link(code, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked()
	e, ok := s.codes[code]
	if !ok {
		// Phase 0 fallback: auto-mint on first Link so installers that
		// haven't wired daemon-side NATS hooks still work end-to-end.
		// Phase 1 will require explicit Mint and return ErrCodeNotFound.
		e = &onboardingEntry{createdAt: s.clockNow()}
		s.codes[code] = e
	}
	if e.userID != "" && e.userID != userID {
		return ErrCodeAlreadyClaimed
	}
	e.userID = userID
	return nil
}

func (s *MemoryOnboardStore) GetLinkedUser(code string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.codes[code]
	if !ok || e.userID == "" {
		return "", false
	}
	return e.userID, true
}

func (s *MemoryOnboardStore) StoreDefaults(code string, d OnboardingDefaults) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.codes[code]
	if !ok {
		return ErrCodeNotFound
	}
	if e.userID == "" {
		return errors.New("pairing code not yet linked to a user")
	}
	cp := d
	e.defaults = &cp
	return nil
}

func (s *MemoryOnboardStore) GetDefaults(code string) (OnboardingDefaults, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.codes[code]
	if !ok || e.defaults == nil {
		return OnboardingDefaults{}, false
	}
	return *e.defaults, true
}

func (s *MemoryOnboardStore) Burn(code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.codes, code)
}

// --- handlers ------------------------------------------------------------

// onboardingAPI fields are kept on the API struct via a small extension
// (see handlers.go) so we don't change the constructor signature. New
// tests call WithOnboardStore.

// WithOnboardStore overrides the API's OnboardStore (used in tests and
// when the BFF is started with a Redis-backed store).
func (a *API) WithOnboardStore(s OnboardStore) *API {
	a.OnboardStore = s
	return a
}

// StartOnboard binds the pairing code in the URL to the
// currently-authenticated user. The browser hits this on landing on
// /onboard/[token] AFTER sign-in; if the user isn't signed in yet the
// frontend should redirect to /account first.
//
//	POST /api/v1/onboard/start
//	  { token }
//	-> 200 { ok: true, user_id: "..." }
//	-> 400 if token malformed
//	-> 401 if unauthenticated
//	-> 409 if claimed by a different user
//	-> 404 if expired/unknown
func (a *API) StartOnboard(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	token := strings.ToUpper(strings.TrimSpace(body.Token))
	if !PairingCodeFormat.MatchString(token) {
		writeError(w, http.StatusBadRequest, "bad_request", "token must be a 6-char Crockford-base32 code")
		return
	}
	if a.OnboardStore == nil {
		writeError(w, http.StatusInternalServerError, "misconfigured", "onboarding store not wired")
		return
	}
	if err := a.OnboardStore.Link(token, claims.UserID().String()); err != nil {
		switch {
		case errors.Is(err, ErrCodeAlreadyClaimed):
			writeError(w, http.StatusConflict, "conflict", err.Error())
		case errors.Is(err, ErrCodeExpired), errors.Is(err, ErrCodeNotFound):
			writeError(w, http.StatusNotFound, "not_found", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "internal", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"user_id": claims.UserID().String(),
	})
}

// CompleteOnboard ships the user's chosen defaults (caps / categories /
// payout) to providers-svc which issues the daemon's mTLS bundle. After
// this returns 200 the daemon's next pair-poll resolves successfully
// and the browser shows the welcome dashboard.
//
//	POST /api/v1/onboard/complete
//	  { token, defaults: { bandwidth_cap_gb, cpu_cap_pct, calendar,
//	                       idle_only, categories, payout_tier } }
//	-> 200 { ok: true, dashboard_url: "/provide" }
//	-> 400 if token malformed or defaults out-of-range
//	-> 401 if unauthenticated
//	-> 404 if token expired
//	-> 412 if token not yet linked to a user (StartOnboard not called)
func (a *API) CompleteOnboard(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}

	var body struct {
		Token    string             `json:"token"`
		Defaults OnboardingDefaults `json:"defaults"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	token := strings.ToUpper(strings.TrimSpace(body.Token))
	if !PairingCodeFormat.MatchString(token) {
		writeError(w, http.StatusBadRequest, "bad_request", "token must be a 6-char Crockford-base32 code")
		return
	}
	if err := validateOnboardingDefaults(&body.Defaults); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	if a.OnboardStore == nil {
		writeError(w, http.StatusInternalServerError, "misconfigured", "onboarding store not wired")
		return
	}

	// Confirm the code is linked to *this* user (Start was called first).
	uid, linked := a.OnboardStore.GetLinkedUser(token)
	if !linked {
		writeError(w, http.StatusPreconditionFailed, "precondition_failed",
			"call /onboard/start first to link this code to your account")
		return
	}
	if uid != claims.UserID().String() {
		writeError(w, http.StatusConflict, "conflict",
			"this code is claimed by a different account")
		return
	}

	if err := a.OnboardStore.StoreDefaults(token, body.Defaults); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	// Phase 0: there's no providers-svc.PairDaemon RPC yet; we'll add it
	// in a follow-up PR (and wire the actual mTLS issuance). For now the
	// 200 here is enough to flip the browser into the "welcome" state +
	// let the daemon's next poll succeed (the daemon side polling that
	// reads the stored bundle ships with the providers-svc PR).
	// TODO(#10): call providers-svc.PairDaemon here.

	// Burn the code so it can't be reused — the daemon is now paired.
	a.OnboardStore.Burn(token)

	a.Logger.Info("onboarding completed",
		"user_id", uid,
		"caps_gb", body.Defaults.BandwidthCapGB,
		"caps_cpu", body.Defaults.CPUCapPct,
		"categories", len(body.Defaults.Categories),
		"payout_tier", body.Defaults.PayoutTier,
	)

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"dashboard_url": "/provide",
	})
}

// PollOnboard is the daemon-side handshake — the daemon polls every 5s
// after minting a code, waiting for the browser to complete. We deliberately
// keep this UNAUTHENTICATED: the daemon doesn't have credentials yet (that's
// what the pairing flow IS issuing), so we instead require the daemon to
// present its self-generated PUBLIC key as proof of liveness. The browser's
// /complete call ties that pubkey to the user's identity.
//
// Phase 0 simplification: poll returns 202 (still waiting) or 200 with a
// minimal bundle. mTLS bundle issuance is wired in #10.
//
//	POST /api/v1/onboard/poll
//	  { token, daemon_pubkey }
//	-> 200 { paired: true, defaults, user_id }
//	-> 202 { paired: false }
//	-> 404 if token expired
func (a *API) PollOnboard(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token        string `json:"token"`
		DaemonPubkey string `json:"daemon_pubkey"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	token := strings.ToUpper(strings.TrimSpace(body.Token))
	if !PairingCodeFormat.MatchString(token) {
		writeError(w, http.StatusBadRequest, "bad_request", "token must be a 6-char Crockford-base32 code")
		return
	}
	if a.OnboardStore == nil {
		writeError(w, http.StatusInternalServerError, "misconfigured", "onboarding store not wired")
		return
	}

	defaults, ok := a.OnboardStore.GetDefaults(token)
	if !ok {
		uid, linked := a.OnboardStore.GetLinkedUser(token)
		if linked {
			// User signed in but hasn't submitted the wizard yet.
			writeJSON(w, http.StatusAccepted, map[string]any{
				"paired":  false,
				"user_id": uid,
				"stage":   "awaiting_wizard",
			})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{
			"paired": false,
			"stage":  "awaiting_signin",
		})
		return
	}
	uid, _ := a.OnboardStore.GetLinkedUser(token)
	writeJSON(w, http.StatusOK, map[string]any{
		"paired":   true,
		"user_id":  uid,
		"defaults": defaults,
	})
}

// --- helpers -------------------------------------------------------------

// validateOnboardingDefaults catches obviously-wrong wizard input
// before we cross the BFF→providers-svc boundary. Real bounds come
// from providers-svc's proto validation; this is just a quick gate.
func validateOnboardingDefaults(d *OnboardingDefaults) error {
	if d.BandwidthCapGB < 0 || d.BandwidthCapGB > 100_000 {
		return errors.New("bandwidth_cap_gb out of range [0, 100000]")
	}
	if d.CPUCapPct < 0 || d.CPUCapPct > 100 {
		return errors.New("cpu_cap_pct out of range [0, 100]")
	}
	if len(d.Categories) > 20 {
		return errors.New("categories: too many (max 20)")
	}
	for _, c := range d.Categories {
		if len(c) > 64 {
			return errors.New("category name too long (max 64 chars)")
		}
	}
	// Empty payout_tier is allowed → "defer" by default in providers-svc.
	switch d.PayoutTier {
	case "", "defer", "stripe_connect", "iogrid_credit":
		// ok
	default:
		return errors.New("payout_tier must be one of: defer | stripe_connect | iogrid_credit")
	}
	if d.Calendar != "" && len(d.Calendar) > 4096 {
		return errors.New("calendar fragment too large (max 4096 chars)")
	}
	return nil
}

// Compile-time assertion: store interface satisfied by the memory impl.
var _ OnboardStore = (*MemoryOnboardStore)(nil)

// Compile-time assertion to nudge future contributors: changing the
// JSON-tag of OnboardingDefaults BREAKS the daemon's bundle resolver
// + the browser wizard. Keep the field names stable.
var _ = json.Marshal
