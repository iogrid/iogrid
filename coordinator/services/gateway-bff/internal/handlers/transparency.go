// Package handlers — transparency report endpoints.
//
// The antiabuse-svc CronJob (cronjob-transparency.yaml) POSTs a freshly
// generated report to /api/v1/transparency/publish. We cache it in
// memory keyed by (year, quarter) and serve back via two public
// endpoints:
//
//	GET /status/transparency           — index listing every cached report
//	GET /status/transparency/{year}/{quarter} — full JSON for one report
//
// These endpoints are intentionally unauthenticated — the docs/LEGAL.md
// commitment ("publish quarterly Phase 2 onward") is to publish them
// publicly. The marketing site /transparency page consumes them.
package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"sync"

	"github.com/go-chi/chi/v5"
)

// transparencyReport is the canonical envelope we cache + serve. The
// shape is deliberately untyped (json.RawMessage) so that the
// antiabuse-svc transparency package can evolve fields without
// requiring a coordinated BFF redeploy.
type transparencyReport struct {
	Year    int             `json:"year"`
	Quarter int             `json:"quarter"`
	Raw     json.RawMessage `json:"-"`
}

// TransparencyStore is the minimal cache interface. The default
// implementation is an in-memory map; production can swap in a Redis
// or DB-backed implementation if cold-start staleness matters.
type TransparencyStore interface {
	Put(year, quarter int, raw json.RawMessage) error
	Get(year, quarter int) (json.RawMessage, bool)
	List() []TransparencyIndex
}

// TransparencyIndex is the per-report summary surfaced by List().
type TransparencyIndex struct {
	Year    int `json:"year"`
	Quarter int `json:"quarter"`
}

// MemoryTransparencyStore is the default implementation. Safe for
// concurrent use.
type MemoryTransparencyStore struct {
	mu      sync.RWMutex
	reports map[int]map[int]json.RawMessage // year → quarter → raw JSON
}

// NewMemoryTransparencyStore returns an empty store.
func NewMemoryTransparencyStore() *MemoryTransparencyStore {
	return &MemoryTransparencyStore{
		reports: map[int]map[int]json.RawMessage{},
	}
}

// Put inserts or replaces a report.
func (s *MemoryTransparencyStore) Put(year, quarter int, raw json.RawMessage) error {
	if year <= 0 || quarter < 1 || quarter > 4 {
		return fmt.Errorf("transparency store: bad key year=%d quarter=%d", year, quarter)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.reports[year]
	if !ok {
		q = map[int]json.RawMessage{}
		s.reports[year] = q
	}
	// copy to detach from caller-owned slice
	cp := make(json.RawMessage, len(raw))
	copy(cp, raw)
	q[quarter] = cp
	return nil
}

// Get retrieves a cached report.
func (s *MemoryTransparencyStore) Get(year, quarter int) (json.RawMessage, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	q, ok := s.reports[year]
	if !ok {
		return nil, false
	}
	raw, ok := q[quarter]
	return raw, ok
}

// List returns every (year, quarter) pair present, newest first.
func (s *MemoryTransparencyStore) List() []TransparencyIndex {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []TransparencyIndex
	for y, qs := range s.reports {
		for q := range qs {
			out = append(out, TransparencyIndex{Year: y, Quarter: q})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Year != out[j].Year {
			return out[i].Year > out[j].Year
		}
		return out[i].Quarter > out[j].Quarter
	})
	return out
}

// PublishTransparencyReport accepts a POSTed JSON report from the
// antiabuse-svc CronJob and caches it for public retrieval.
//
// Auth: the route is mounted under the BFF's authenticated chain when
// a TRANSPARENCY_PUBLISH_TOKEN is configured; otherwise it mounts
// under the unauthenticated chain (suitable for in-cluster traffic
// guarded by NetworkPolicy).
func (a *API) PublishTransparencyReport(w http.ResponseWriter, r *http.Request) {
	if a.Transparency == nil {
		writeError(w, http.StatusServiceUnavailable, "transparency_store_unavailable", "transparency store not configured")
		return
	}
	// Read the body up to 1MiB. Reports are small (<10KiB typical).
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_body", err.Error())
		return
	}
	defer r.Body.Close()

	var meta struct {
		Year    int `json:"year"`
		Quarter int `json:"quarter"`
	}
	if err := json.Unmarshal(body, &meta); err != nil {
		writeError(w, http.StatusBadRequest, "decode", err.Error())
		return
	}
	if err := a.Transparency.Put(meta.Year, meta.Quarter, body); err != nil {
		writeError(w, http.StatusBadRequest, "store", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":  "stored",
		"year":    meta.Year,
		"quarter": meta.Quarter,
	})
}

// GetTransparencyReport serves a single cached report. Returns 404 if
// the requested year/quarter has never been published.
func (a *API) GetTransparencyReport(w http.ResponseWriter, r *http.Request) {
	if a.Transparency == nil {
		writeError(w, http.StatusServiceUnavailable, "transparency_store_unavailable", "transparency store not configured")
		return
	}
	year, err1 := strconv.Atoi(chi.URLParam(r, "year"))
	quarter, err2 := strconv.Atoi(chi.URLParam(r, "quarter"))
	if err1 != nil || err2 != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid year/quarter")
		return
	}
	raw, ok := a.Transparency.Get(year, quarter)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no report for that quarter")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

// ListTransparencyReports surfaces every cached report's (year, quarter)
// so the marketing /transparency page can render a navigation index.
func (a *API) ListTransparencyReports(w http.ResponseWriter, r *http.Request) {
	if a.Transparency == nil {
		writeError(w, http.StatusServiceUnavailable, "transparency_store_unavailable", "transparency store not configured")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"reports": a.Transparency.List(),
	})
}

// errTransparencyMissing is returned when the API is asked to look up
// a report but the store has not been configured. Kept exported so
// callers (tests) can type-assert.
var errTransparencyMissing = errors.New("transparency store unavailable")
