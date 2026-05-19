// Auto-update endpoints — proxy the operator's preferences to the
// daemon's UI bridge and surface the worker's UpdateState back to the
// web UI at /account/updates.
//
// In Phase 0 the BFF does NOT talk to the daemon directly (there's no
// cluster-side state for per-user daemon endpoints yet — daemons live
// on operator laptops behind NAT). Instead the BFF keeps an in-memory
// per-user preferences store and the daemon polls
// `/api/v1/account/updates/preferences` on its heartbeat to learn the
// current channel + auto-update flag. The state in the response is a
// stub (history: []) until the daemon-side push API ships.
//
// This handler is wired in routes.go under
//
//	GET  /api/v1/account/updates                 — state + preferences
//	POST /api/v1/account/updates/preferences     — save channel/auto flag
//	POST /api/v1/account/updates/check           — request immediate poll
//	POST /api/v1/account/updates/apply           — apply pending update
//	POST /api/v1/account/updates/rollback        — restore previous binary
//
// All endpoints require an authenticated user; the preferences map
// keys off the user id. Issue #59.

package handlers

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/auth"
)

// UpdatePreferences is the operator-controlled channel + auto-update
// toggle. Persisted per user.
type UpdatePreferences struct {
	Channel    string `json:"channel"`
	AutoUpdate bool   `json:"autoUpdate"`
}

// UpdateOutcome mirrors the Rust enum's serde representation
// (`status` tag + payload fields). We keep it as a generic map on the
// wire so the BFF doesn't need to know every variant — the web UI
// renders by reading the `status` discriminator.
type UpdateOutcome = map[string]any

// UpdateHistoryEntry is one row in the rolling poll-history ledger.
type UpdateHistoryEntry struct {
	At          string        `json:"at"`
	Channel     string        `json:"channel"`
	FromVersion string        `json:"fromVersion"`
	Outcome     UpdateOutcome `json:"outcome"`
}

// UpdateState mirrors the Rust supervisor's UpdateState.
type UpdateState struct {
	Enabled        bool                 `json:"enabled"`
	LastOutcome    UpdateOutcome        `json:"lastOutcome,omitempty"`
	PendingVersion string               `json:"pendingVersion,omitempty"`
	History        []UpdateHistoryEntry `json:"history"`
}

// updatesStore — in-memory per-user preferences + daemon-reported
// state. Production swaps this for an identity-svc backed store.
type updatesStore struct {
	mu    sync.RWMutex
	prefs map[string]UpdatePreferences
	state map[string]UpdateState
}

func newUpdatesStore() *updatesStore {
	return &updatesStore{
		prefs: map[string]UpdatePreferences{},
		state: map[string]UpdateState{},
	}
}

func (s *updatesStore) getPrefs(uid string) UpdatePreferences {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if p, ok := s.prefs[uid]; ok {
		return p
	}
	return UpdatePreferences{Channel: "stable", AutoUpdate: false}
}

func (s *updatesStore) setPrefs(uid string, p UpdatePreferences) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prefs[uid] = p
}

func (s *updatesStore) getState(uid string) UpdateState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if st, ok := s.state[uid]; ok {
		return st
	}
	prefs := s.getPrefsLocked(uid)
	return UpdateState{Enabled: prefs.AutoUpdate, History: []UpdateHistoryEntry{}}
}

func (s *updatesStore) getPrefsLocked(uid string) UpdatePreferences {
	if p, ok := s.prefs[uid]; ok {
		return p
	}
	return UpdatePreferences{Channel: "stable", AutoUpdate: false}
}

// appendHistory records a synthetic check outcome. Used by the
// /check endpoint to give the web UI immediate feedback in Phase 0,
// even while the daemon-side push hasn't shipped.
func (s *updatesStore) appendHistory(uid string, entry UpdateHistoryEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.state[uid]
	if !ok {
		prefs := s.getPrefsLocked(uid)
		st = UpdateState{Enabled: prefs.AutoUpdate, History: []UpdateHistoryEntry{}}
	}
	st.History = append([]UpdateHistoryEntry{entry}, st.History...)
	if len(st.History) > 50 {
		st.History = st.History[:50]
	}
	st.LastOutcome = entry.Outcome
	s.state[uid] = st
}

// validateChannel rejects values outside the supported set. The Rust
// daemon accepts `canary` as an alias for `edge`; we standardise on
// `canary` at the BFF layer because that's what the UI radio button
// displays.
func validateChannel(c string) error {
	switch strings.ToLower(strings.TrimSpace(c)) {
	case "stable", "beta", "canary":
		return nil
	default:
		return errors.New("channel must be stable, beta or canary")
	}
}

// GetUpdates returns the operator's current preferences + the most
// recent state snapshot the daemon has reported.
//
//	GET /api/v1/account/updates  -> 200 { state, preferences }
func (a *API) GetUpdates(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	uid := claims.UserID().String()
	store := a.ensureUpdatesStore()
	writeJSON(w, http.StatusOK, map[string]any{
		"preferences": store.getPrefs(uid),
		"state":       store.getState(uid),
	})
}

// SaveUpdatePreferences persists the channel + auto-update flag.
//
//	POST /api/v1/account/updates/preferences { channel, autoUpdate }
func (a *API) SaveUpdatePreferences(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	var body UpdatePreferences
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := validateChannel(body.Channel); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	body.Channel = strings.ToLower(strings.TrimSpace(body.Channel))
	store := a.ensureUpdatesStore()
	store.setPrefs(claims.UserID().String(), body)
	writeJSON(w, http.StatusOK, body)
}

// TriggerUpdateCheck queues an immediate manifest fetch. In Phase 0 we
// fabricate a synthetic "up_to_date" outcome and append it to the
// history so the UI has something to render — the daemon-side push
// API lands in a follow-up PR.
//
//	POST /api/v1/account/updates/check
func (a *API) TriggerUpdateCheck(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	uid := claims.UserID().String()
	store := a.ensureUpdatesStore()
	prefs := store.getPrefs(uid)
	outcome := UpdateOutcome{
		"status":  "up_to_date",
		"current": "0.1.0",
	}
	store.appendHistory(uid, UpdateHistoryEntry{
		At:          time.Now().UTC().Format(time.RFC3339),
		Channel:     prefs.Channel,
		FromVersion: "0.1.0",
		Outcome:     outcome,
	})
	writeJSON(w, http.StatusAccepted, map[string]any{
		"queued": true,
	})
}

// ApplyPendingUpdate signals the daemon to apply the staged update.
// Phase 0 is a no-op that records the request; the daemon-side IPC
// lands in a follow-up PR.
//
//	POST /api/v1/account/updates/apply
func (a *API) ApplyPendingUpdate(w http.ResponseWriter, r *http.Request) {
	_, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"queued": true})
}

// RollbackUpdate signals the daemon to restore the previous binary.
//
//	POST /api/v1/account/updates/rollback
func (a *API) RollbackUpdate(w http.ResponseWriter, r *http.Request) {
	_, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"queued": true})
}

// ensureUpdatesStore lazy-initialises the in-memory store on first
// access. Tests can override via WithUpdatesStore.
func (a *API) ensureUpdatesStore() *updatesStore {
	a.updatesOnce.Do(func() {
		if a.updates == nil {
			a.updates = newUpdatesStore()
		}
	})
	return a.updates
}
