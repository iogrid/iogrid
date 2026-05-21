// Package store is the providers-svc persistence layer. The default
// implementation is in-memory and is what the test suite and the local
// development binary use. A pgx-backed implementation lives in store_pg.go
// behind the `postgres` build tag — see internal/db/migrations for the
// schema it expects.
//
// All public types here are server-side projections (NOT the wire protos).
// Handlers translate between proto messages and these structs to keep the
// store DB-agnostic and easy to unit-test.
package store

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Errors returned by the store. Handlers map these to Connect codes.
var (
	ErrNotFound      = errors.New("store: not found")
	ErrTokenInvalid  = errors.New("store: pairing token invalid or already consumed")
	ErrAlreadyExists = errors.New("store: already exists")
)

// Platform mirrors providers/v1.Platform for storage purposes.
type Platform string

const (
	PlatformUnspecified Platform = ""
	PlatformMacOS       Platform = "macos"
	PlatformLinux       Platform = "linux"
	PlatformWindows     Platform = "windows"
)

// Status mirrors providers/v1.ProviderStatus.
type Status string

const (
	StatusActive       Status = "active"
	StatusOffline      Status = "offline"
	StatusSuspended    Status = "suspended"
	StatusDeactivated  Status = "deactivated"
	StatusUnspecified  Status = ""
)

// HostInfo is the persisted projection of providers/v1.HostInfo.
type HostInfo struct {
	Platform        Platform
	Architecture    string
	OSVersion       string
	DaemonVersion   string
	TotalMemoryMiB  uint64
	CPUModel        string
	CPULogicalCores uint32
	GPUModels       []string
	DockerAvailable bool
	TartAvailable   bool
}

// NetworkInfo is the persisted projection of providers/v1.NetworkInfo.
type NetworkInfo struct {
	PublicIP       string
	ASN            uint32
	ISP            string
	ThroughputMbps uint32
	LatencyMs      uint32
	RegionSlug     string
	RegionName     string
	CountryCode    string
}

// Capability is the persisted projection of providers/v1.CapabilityInventory.
type Capability struct {
	SupportedTypes  []string // e.g. "bandwidth", "docker", "gpu", "ios_build"
	GPUEnabled      bool
	IOSBuildEnabled bool
}

// Provider is the persisted projection used by the registration RPCs.
type Provider struct {
	ID            string
	OwnerUserID   string
	DisplayName   string
	Status        Status
	HostInfo      HostInfo
	NetworkInfo   NetworkInfo
	Capabilities  Capability
	RegisteredAt  time.Time
	LastSeenAt    time.Time
	PublicKey     []byte // DER-encoded SubjectPublicKeyInfo, captured at pairing time
	// IsPrimary marks the owner's elected default daemon for /provide/*
	// surfaces. At-most-one TRUE per owner_user_id (enforced by a partial
	// unique index in migration 0002). On PairDaemon: the first row for
	// an owner is promoted to primary; subsequent pairings stay false
	// until the owner promotes them via SetPrimaryProvider. See #325.
	IsPrimary bool
}

// SchedulingConfig is the persisted desired-state for a single provider.
type SchedulingConfig struct {
	ProviderID         string
	BandwidthCapGB     uint32
	CPUCapPct          uint32
	MemoryCapPct       uint32
	GPUCapWhenIdlePct  uint32
	GPUCapWhenActivePct uint32
	CalendarWindows    []CalendarWindow
	IdleEnabled        bool
	IdleThresholdSecs  uint32
	AllowedCategories  []string
	DisallowedCategories []string
	DestinationBlocklist []string
	PerCustomerMinutesCap uint32
	UpdatedAt          time.Time
	UpdatedByUserID    string
}

// CalendarWindow is the persisted form of providers/v1.CalendarWindow.
type CalendarWindow struct {
	DaysOfWeek []uint32
	StartLocal string // HH:MM
	EndLocal   string // HH:MM
	Timezone   string // IANA
}

// PairingToken is a one-time secret minted by the management plane (or by
// the test harness) and consumed by PairDaemon. Tokens are bound to a
// specific owner user; the store enforces single-use semantics.
type PairingToken struct {
	Token       string
	OwnerUserID string
	IssuedAt    time.Time
	ExpiresAt   time.Time
	ConsumedAt  *time.Time
}

// AuditEvent is the persisted projection used by the transparency feed.
type AuditEvent struct {
	ID                  string
	ProviderID          string
	Kind                string // matches providers/v1.EventKind enum slug
	OccurredAt          time.Time
	WorkloadType        string
	Category            string
	CustomerDisplayName string
	DestinationSummary  string
	Bytes               uint64
	Metadata            map[string]string
}

// EarningsEntry is a single credit against a provider, summed in
// GetEarningsSummary. Workload-type breakdown is computed by bucketing on
// WorkloadType.
type EarningsEntry struct {
	ProviderID   string
	WorkloadType string
	OccurredAt   time.Time
	Currency     string
	Micros       int64
}

// Store is the providers-svc persistence interface. The in-memory impl is
// returned by NewInMemory; tests construct one directly.
type Store interface {
	// Provider CRUD ------------------------------------------------------
	CreateProvider(ctx context.Context, p *Provider) error
	UpdateProvider(ctx context.Context, p *Provider) error
	GetProvider(ctx context.Context, id string) (*Provider, error)
	ListProviders(ctx context.Context, opts ListOptions) (providers []*Provider, nextToken string, err error)
	DeactivateProvider(ctx context.Context, id, reason string) error
	// UpdateLastSeen bumps providers.last_seen_at for one row. Called on
	// every heartbeat (#311) so /admin/providers can render a real
	// "last seen N seconds ago" recency signal. Returns ErrNotFound if
	// the id doesn't exist (which after pairing should not happen and is
	// surfaced as a warning by the caller).
	UpdateLastSeen(ctx context.Context, id string, at time.Time) error

	// SetPrimaryProvider atomically flips the per-owner primary flag:
	// the previous primary (if any) is cleared and providerID becomes the
	// new primary. Both rows MUST be owned by ownerUserID — mismatch
	// returns ErrNotFound (handlers translate to PermissionDenied to
	// avoid leaking provider existence to non-owners). Returns the
	// freshly-promoted Provider. See #325.
	SetPrimaryProvider(ctx context.Context, ownerUserID, providerID string) (*Provider, error)

	// GetProviderByOwnerAndDisplayName returns the existing row keyed by
	// (owner_user_id, display_name) — the natural dedupe key for re-pair
	// from the same host. The daemon self-reports its OS hostname as the
	// display_name on pair (see iogrid-transport::identity::
	// local_display_name); a fresh PairDaemon call from a machine that
	// was paired before resolves to the same row so /admin/providers
	// shows ONE row per host instead of accumulating a new ghost on
	// every daemon reinstall (Hatice's #327 bug).
	//
	// Lookup MUST be exact-match (no LIKE / ILIKE). Returns
	// (nil, ErrNotFound) when no row matches — callers turn that into
	// "first pair, INSERT a new row". Owner+display_name with empty
	// display_name returns ErrNotFound immediately so the empty-hostname
	// fallback path (legacy daemons that don't ship the field) is forced
	// to create a fresh row rather than colliding on the empty string.
	GetProviderByOwnerAndDisplayName(ctx context.Context, ownerUserID, displayName string) (*Provider, error)

	// SelectProviderForOwner returns the deterministic "which paired
	// daemon answers for this user" pick for /provide/* surfaces:
	//   ORDER BY is_primary DESC,
	//            last_seen_at DESC NULLS LAST,
	//            registered_at DESC
	//   LIMIT 1
	// Returns (nil, nil) when the owner has zero rows — that's the
	// "install daemon" empty-state path (#313), NOT an error. See #325
	// for why a separate method (rather than reusing ListProviders +
	// client-side sort) — the ordering is load-bearing and we want the
	// SQL planner to keep it on the server.
	SelectProviderForOwner(ctx context.Context, ownerUserID string) (*Provider, error)

	// Pairing -----------------------------------------------------------
	IssuePairingToken(ctx context.Context, ownerUserID string, ttl time.Duration) (string, error)
	ConsumePairingToken(ctx context.Context, token string) (PairingToken, error)

	// Scheduling --------------------------------------------------------
	GetSchedulingConfig(ctx context.Context, providerID string) (*SchedulingConfig, error)
	UpdateSchedulingConfig(ctx context.Context, cfg *SchedulingConfig) (*SchedulingConfig, error)

	// Audit + Earnings --------------------------------------------------
	AppendAuditEvent(ctx context.Context, e AuditEvent) error
	ListAuditEvents(ctx context.Context, providerID string, opts AuditQuery) ([]AuditEvent, string, error)
	SubscribeAuditEvents(providerID string) (<-chan AuditEvent, func())
	CreditEarnings(ctx context.Context, e EarningsEntry) error
	SumEarnings(ctx context.Context, providerID string, from, to time.Time) (total int64, byType map[string]int64, currency string, err error)
}

// ListOptions parameterises ListProviders.
type ListOptions struct {
	OwnerUserID  string
	Status       Status
	PageSize     int
	PageToken    string
}

// AuditQuery parameterises ListAuditEvents.
type AuditQuery struct {
	From      time.Time
	To        time.Time
	Kinds     []string
	PageSize  int
	PageToken string
}

// --- in-memory implementation -----------------------------------------------

type memStore struct {
	mu        sync.RWMutex
	providers map[string]*Provider
	configs   map[string]*SchedulingConfig
	tokens    map[string]*PairingToken
	audits    []AuditEvent
	earnings  []EarningsEntry

	subsMu sync.Mutex
	subs   map[string][]chan AuditEvent
}

// NewInMemory builds a fresh in-memory store. Safe for concurrent use.
func NewInMemory() Store {
	return &memStore{
		providers: make(map[string]*Provider),
		configs:   make(map[string]*SchedulingConfig),
		tokens:    make(map[string]*PairingToken),
		subs:      make(map[string][]chan AuditEvent),
	}
}

func (m *memStore) CreateProvider(_ context.Context, p *Provider) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	if _, exists := m.providers[p.ID]; exists {
		return ErrAlreadyExists
	}
	if p.RegisteredAt.IsZero() {
		p.RegisteredAt = time.Now().UTC()
	}
	if p.LastSeenAt.IsZero() {
		p.LastSeenAt = p.RegisteredAt
	}
	if p.Status == StatusUnspecified {
		p.Status = StatusActive
	}
	// First daemon paired by this owner is auto-promoted to primary, so
	// single-daemon users (the overwhelming majority) never need to
	// touch the picker. Second-and-onwards default to false; the owner
	// flips them via SetPrimaryProvider. Mirrors pgStore.CreateProvider.
	if p.OwnerUserID != "" {
		hasAny := false
		for _, existing := range m.providers {
			if existing.OwnerUserID == p.OwnerUserID {
				hasAny = true
				break
			}
		}
		if !hasAny {
			p.IsPrimary = true
		}
	}
	copy := *p
	m.providers[p.ID] = &copy
	return nil
}

func (m *memStore) UpdateProvider(_ context.Context, p *Provider) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.providers[p.ID]; !exists {
		return ErrNotFound
	}
	copy := *p
	m.providers[p.ID] = &copy
	return nil
}

// UpdateLastSeen bumps the last_seen_at timestamp on a single provider.
// Hot path: invoked on every heartbeat tick (~5s per paired daemon), so
// it intentionally does NOT take the full Provider struct or rewrite all
// columns — just the recency stamp.
func (m *memStore) UpdateLastSeen(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.providers[id]
	if !ok {
		return ErrNotFound
	}
	p.LastSeenAt = at
	return nil
}

// SetPrimaryProvider atomically swaps the per-owner primary flag. See
// the Store interface comment for the contract. In-memory: we hold the
// write lock for the duration so a concurrent CreateProvider for the
// same owner can't slip a second primary in between the clear + set.
func (m *memStore) SetPrimaryProvider(_ context.Context, ownerUserID, providerID string) (*Provider, error) {
	if ownerUserID == "" || providerID == "" {
		return nil, ErrNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	target, ok := m.providers[providerID]
	if !ok || target.OwnerUserID != ownerUserID {
		// Caller doesn't own this id (or id doesn't exist). Surfacing
		// ErrNotFound rather than ErrAlreadyExists keeps the handler's
		// error mapping uniform; the handler turns this into
		// PermissionDenied so non-owners can't probe ids by error code.
		return nil, ErrNotFound
	}
	for _, existing := range m.providers {
		if existing.OwnerUserID == ownerUserID {
			existing.IsPrimary = false
		}
	}
	target.IsPrimary = true
	out := *target
	return &out, nil
}

// SelectProviderForOwner returns the primary (or best-available)
// provider for the owner. (nil, nil) when the owner has zero rows.
// Mirrors the pgStore SQL: ORDER BY is_primary DESC, last_seen_at DESC,
// registered_at DESC — deterministic and stable across restarts so the
// same daemon answers for /provide/* until the owner re-elects.
func (m *memStore) SelectProviderForOwner(_ context.Context, ownerUserID string) (*Provider, error) {
	if ownerUserID == "" {
		return nil, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var best *Provider
	for _, existing := range m.providers {
		if existing.OwnerUserID != ownerUserID {
			continue
		}
		if best == nil || providerBeatsForOwner(existing, best) {
			best = existing
		}
	}
	if best == nil {
		return nil, nil
	}
	out := *best
	return &out, nil
}

// providerBeatsForOwner is the ranking predicate used by
// SelectProviderForOwner. cand "beats" cur when (in order):
//   1. cand.IsPrimary && !cur.IsPrimary
//   2. cand.LastSeenAt > cur.LastSeenAt
//   3. cand.RegisteredAt > cur.RegisteredAt
// Final tiebreaker: lexical ID (stable, deterministic).
func providerBeatsForOwner(cand, cur *Provider) bool {
	if cand.IsPrimary != cur.IsPrimary {
		return cand.IsPrimary
	}
	if !cand.LastSeenAt.Equal(cur.LastSeenAt) {
		return cand.LastSeenAt.After(cur.LastSeenAt)
	}
	if !cand.RegisteredAt.Equal(cur.RegisteredAt) {
		return cand.RegisteredAt.After(cur.RegisteredAt)
	}
	return cand.ID < cur.ID
}

// GetProviderByOwnerAndDisplayName implements the Store contract — see
// the interface comment. In-memory version: linear scan over the owner's
// rows, exact-match on display_name. The expected cardinality is
// ones-to-tens of providers per owner, so the linear cost is negligible.
func (m *memStore) GetProviderByOwnerAndDisplayName(_ context.Context, ownerUserID, displayName string) (*Provider, error) {
	if ownerUserID == "" || displayName == "" {
		// Documented contract: empty display_name MUST NOT collide on
		// the empty-string. Force the caller into the INSERT path so
		// legacy daemons that don't ship a hostname don't all stomp on
		// each other.
		return nil, ErrNotFound
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.providers {
		if p.OwnerUserID == ownerUserID && p.DisplayName == displayName {
			out := *p
			return &out, nil
		}
	}
	return nil, ErrNotFound
}

func (m *memStore) GetProvider(_ context.Context, id string) (*Provider, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.providers[id]
	if !ok {
		return nil, ErrNotFound
	}
	copy := *p
	return &copy, nil
}

func (m *memStore) ListProviders(_ context.Context, opts ListOptions) ([]*Provider, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]*Provider, 0, len(m.providers))
	for _, p := range m.providers {
		if opts.OwnerUserID != "" && p.OwnerUserID != opts.OwnerUserID {
			continue
		}
		if opts.Status != StatusUnspecified && p.Status != opts.Status {
			continue
		}
		c := *p
		out = append(out, &c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })

	size := opts.PageSize
	if size <= 0 || size > 200 {
		size = 50
	}
	start := 0
	if opts.PageToken != "" {
		for i, p := range out {
			if p.ID > opts.PageToken {
				start = i
				break
			}
			if i == len(out)-1 {
				start = len(out)
			}
		}
	}
	end := start + size
	next := ""
	if end < len(out) {
		next = out[end-1].ID
	} else {
		end = len(out)
	}
	return out[start:end], next, nil
}

func (m *memStore) DeactivateProvider(_ context.Context, id, reason string) error {
	m.mu.Lock()
	p, ok := m.providers[id]
	if !ok {
		m.mu.Unlock()
		return ErrNotFound
	}
	p.Status = StatusDeactivated
	m.mu.Unlock()

	// Audit on deactivate (best-effort; never block on subscribers).
	_ = m.AppendAuditEvent(context.Background(), AuditEvent{
		ID:         uuid.NewString(),
		ProviderID: id,
		Kind:       "EVENT_KIND_SCHEDULER_TRANSITION",
		OccurredAt: time.Now().UTC(),
		Metadata:   map[string]string{"reason": reason, "transition": "deactivated"},
	})
	return nil
}

func (m *memStore) IssuePairingToken(_ context.Context, ownerUserID string, ttl time.Duration) (string, error) {
	if ttl == 0 {
		ttl = 15 * time.Minute
	}
	tok := strings.ReplaceAll(uuid.NewString(), "-", "")
	now := time.Now().UTC()
	m.mu.Lock()
	m.tokens[tok] = &PairingToken{
		Token:       tok,
		OwnerUserID: ownerUserID,
		IssuedAt:    now,
		ExpiresAt:   now.Add(ttl),
	}
	m.mu.Unlock()
	return tok, nil
}

func (m *memStore) ConsumePairingToken(_ context.Context, token string) (PairingToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pt, ok := m.tokens[token]
	if !ok {
		return PairingToken{}, ErrTokenInvalid
	}
	if pt.ConsumedAt != nil {
		return PairingToken{}, ErrTokenInvalid
	}
	if time.Now().After(pt.ExpiresAt) {
		return PairingToken{}, ErrTokenInvalid
	}
	now := time.Now().UTC()
	pt.ConsumedAt = &now
	return *pt, nil
}

func (m *memStore) GetSchedulingConfig(_ context.Context, providerID string) (*SchedulingConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cfg, ok := m.configs[providerID]
	if !ok {
		return defaultConfig(providerID), nil
	}
	c := *cfg
	return &c, nil
}

func (m *memStore) UpdateSchedulingConfig(_ context.Context, cfg *SchedulingConfig) (*SchedulingConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg.UpdatedAt = time.Now().UTC()
	copy := *cfg
	m.configs[cfg.ProviderID] = &copy
	out := copy
	return &out, nil
}

// defaultConfig returns the documented out-of-the-box settings (see
// docs/TECH.md §Scheduling: Defaults).
func defaultConfig(providerID string) *SchedulingConfig {
	return &SchedulingConfig{
		ProviderID:           providerID,
		BandwidthCapGB:       50,
		CPUCapPct:            30,
		MemoryCapPct:         25,
		GPUCapWhenIdlePct:    100,
		GPUCapWhenActivePct:  0,
		CalendarWindows:      nil,
		IdleEnabled:          true,
		IdleThresholdSecs:    300,
		AllowedCategories:    []string{"e_commerce", "seo", "ad_verification", "ai_training_data", "iogrid_internal"},
		DisallowedCategories: nil,
		DestinationBlocklist: nil,
		PerCustomerMinutesCap: 0,
		UpdatedAt:            time.Now().UTC(),
	}
}

func (m *memStore) AppendAuditEvent(_ context.Context, e AuditEvent) error {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
	m.mu.Lock()
	m.audits = append(m.audits, e)
	m.mu.Unlock()

	m.subsMu.Lock()
	subs := append([]chan AuditEvent(nil), m.subs[e.ProviderID]...)
	m.subsMu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- e:
		default: // drop on slow subscriber rather than block writers
		}
	}
	return nil
}

func (m *memStore) ListAuditEvents(_ context.Context, providerID string, q AuditQuery) ([]AuditEvent, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	filtered := make([]AuditEvent, 0)
	for _, e := range m.audits {
		if e.ProviderID != providerID {
			continue
		}
		if !q.From.IsZero() && e.OccurredAt.Before(q.From) {
			continue
		}
		if !q.To.IsZero() && !e.OccurredAt.Before(q.To) {
			continue
		}
		if len(q.Kinds) > 0 && !containsString(q.Kinds, e.Kind) {
			continue
		}
		filtered = append(filtered, e)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].OccurredAt.Before(filtered[j].OccurredAt)
	})

	size := q.PageSize
	if size <= 0 || size > 500 {
		size = 100
	}
	start := 0
	if q.PageToken != "" {
		for i, e := range filtered {
			if e.ID > q.PageToken {
				start = i
				break
			}
		}
	}
	end := start + size
	next := ""
	if end < len(filtered) {
		next = filtered[end-1].ID
	} else {
		end = len(filtered)
	}
	return filtered[start:end], next, nil
}

func (m *memStore) SubscribeAuditEvents(providerID string) (<-chan AuditEvent, func()) {
	ch := make(chan AuditEvent, 64)
	m.subsMu.Lock()
	m.subs[providerID] = append(m.subs[providerID], ch)
	m.subsMu.Unlock()

	cancel := func() {
		m.subsMu.Lock()
		defer m.subsMu.Unlock()
		remaining := m.subs[providerID][:0]
		for _, c := range m.subs[providerID] {
			if c != ch {
				remaining = append(remaining, c)
			}
		}
		m.subs[providerID] = remaining
		close(ch)
	}
	return ch, cancel
}

func (m *memStore) CreditEarnings(_ context.Context, e EarningsEntry) error {
	if e.Currency == "" {
		// Match the pgStore default — Phase-0 native ledger is $GRID
		// (see store_pg.go::CreditEarnings, #312).
		e.Currency = "GRID"
	}
	m.mu.Lock()
	m.earnings = append(m.earnings, e)
	m.mu.Unlock()
	return nil
}

func (m *memStore) SumEarnings(_ context.Context, providerID string, from, to time.Time) (int64, map[string]int64, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var total int64
	byType := make(map[string]int64)
	currency := ""
	for _, e := range m.earnings {
		if e.ProviderID != providerID {
			continue
		}
		if !from.IsZero() && e.OccurredAt.Before(from) {
			continue
		}
		if !to.IsZero() && !e.OccurredAt.Before(to) {
			continue
		}
		total += e.Micros
		byType[e.WorkloadType] += e.Micros
		if currency == "" {
			currency = e.Currency
		}
	}
	if currency == "" {
		// Phase-0 native ledger currency is $GRID; see store_pg.go
		// SumEarnings for the rationale (#312).
		currency = "GRID"
	}
	return total, byType, currency, nil
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
