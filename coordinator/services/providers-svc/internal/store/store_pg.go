// Postgres-backed Store implementation for providers-svc.
//
// Mirrors the in-memory store in store.go method-for-method. Used when
// DATABASE_URL is set (production); the in-memory store remains the default
// for unit tests and local development.
//
// Design notes
//
//   - We use pgx v5 directly (pgxpool) — no database/sql wrapper on the
//     hot path. database/sql is only linked via pgx/stdlib so goose can run
//     migrations.
//   - Audit subscriptions are an in-process channel fan-out, exactly like
//     the in-memory store. Cross-pod broadcast lands in a later issue
//     (NATS topic per provider) — Phase 0 single-replica live-views are
//     fine with in-process subscribers because /admin/providers is the
//     only consumer and it's load-balanced behind a sticky session.
//   - Provider IDs and owner IDs are UUIDs in the schema. The Provider
//     struct keeps them as `string` for handler ergonomics; we parse with
//     uuid.Parse and surface friendlier errors.
//   - DeactivateProvider also writes an audit event — same behaviour as
//     the in-memory impl.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pgStore is the Postgres-backed implementation of Store.
type pgStore struct {
	pool *pgxpool.Pool

	// in-process audit subscriber fan-out. Mirrors memStore.subs.
	subsMu sync.Mutex
	subs   map[string][]chan AuditEvent
}

// NewPostgres returns a Store backed by Postgres. Caller owns the pool and
// is responsible for Close()ing it during shutdown.
func NewPostgres(pool *pgxpool.Pool) Store {
	return &pgStore{
		pool: pool,
		subs: make(map[string][]chan AuditEvent),
	}
}

// --- helpers ---------------------------------------------------------------

func parseUUID(field, raw string) (uuid.UUID, error) {
	if raw == "" {
		return uuid.Nil, fmt.Errorf("store: %s is empty", field)
	}
	u, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("store: %s %q is not a UUID: %w", field, raw, err)
	}
	return u, nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullUUID(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// emptyStrings replaces a nil slice with [] so the TEXT[] columns
// never receive a NULL (the schema has NOT NULL DEFAULT '{}').
func emptyStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// --- providers -------------------------------------------------------------

func (p *pgStore) CreateProvider(ctx context.Context, pr *Provider) error {
	owner, err := parseUUID("ownerUserID", pr.OwnerUserID)
	if err != nil {
		return err
	}
	if pr.ID == "" {
		pr.ID = uuid.NewString()
	}
	id, err := parseUUID("id", pr.ID)
	if err != nil {
		return err
	}
	if pr.Status == StatusUnspecified {
		pr.Status = StatusActive
	}
	if pr.RegisteredAt.IsZero() {
		pr.RegisteredAt = time.Now().UTC()
	}
	if pr.LastSeenAt.IsZero() {
		pr.LastSeenAt = pr.RegisteredAt
	}

	// #325: first row for an owner is auto-promoted to primary so the
	// single-daemon case (every user except multi-machine power users)
	// never sees the picker AND /provide/* has a deterministic answer
	// from the first heartbeat. We compute the flag inside a single
	// transaction with the INSERT so a concurrent pair for the same
	// owner cannot race and produce two primaries (the partial unique
	// index would catch it, but we'd rather not surface 23505 to the
	// daemon installer).
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // committed below

	if !pr.IsPrimary {
		// Only auto-promote when the caller hasn't already asked for a
		// specific value. Lets future call sites (admin import, etc.)
		// preserve their intent.
		var count int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM providers WHERE owner_user_id = $1`, owner).Scan(&count); err != nil {
			return fmt.Errorf("count owner providers: %w", err)
		}
		if count == 0 {
			pr.IsPrimary = true
		}
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO providers (
			id, owner_user_id, display_name, status,
			platform, architecture, os_version, daemon_version,
			total_memory_mib, cpu_model, cpu_logical_cores, gpu_models,
			docker_available, tart_available,
			public_ip, asn, isp, throughput_mbps, latency_ms,
			region_slug, region_name, country_code,
			supported_types, gpu_enabled, ios_build_enabled,
			public_key, registered_at, last_seen_at,
			is_primary, host_macos_version
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7, $8,
			$9, $10, $11, $12,
			$13, $14,
			$15, $16, $17, $18, $19,
			$20, $21, $22,
			$23, $24, $25,
			$26, $27, $28,
			$29, $30
		)`,
		id, owner, pr.DisplayName, string(pr.Status),
		nullString(string(pr.HostInfo.Platform)), nullString(pr.HostInfo.Architecture), nullString(pr.HostInfo.OSVersion), nullString(pr.HostInfo.DaemonVersion),
		int64(pr.HostInfo.TotalMemoryMiB), nullString(pr.HostInfo.CPUModel), int32(pr.HostInfo.CPULogicalCores), emptyStrings(pr.HostInfo.GPUModels),
		pr.HostInfo.DockerAvailable, pr.HostInfo.TartAvailable,
		nullString(pr.NetworkInfo.PublicIP), int32(pr.NetworkInfo.ASN), nullString(pr.NetworkInfo.ISP), int32(pr.NetworkInfo.ThroughputMbps), int32(pr.NetworkInfo.LatencyMs),
		nullString(pr.NetworkInfo.RegionSlug), nullString(pr.NetworkInfo.RegionName), nullString(pr.NetworkInfo.CountryCode),
		emptyStrings(pr.Capabilities.SupportedTypes), pr.Capabilities.GPUEnabled, pr.Capabilities.IOSBuildEnabled,
		pr.PublicKey, pr.RegisteredAt, pr.LastSeenAt,
		pr.IsPrimary, int32(pr.Capabilities.HostMacosVersion),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return fmt.Errorf("insert provider: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit insert: %w", err)
	}
	return nil
}

// UpdateLastSeen bumps just providers.last_seen_at for one row. Called on
// every heartbeat (#311) — keep the hot path lean (one tiny UPDATE; no
// full-row rewrite, no transaction). Returns ErrNotFound when the row is
// missing so the StreamHeartbeats handler can log a warning instead of
// silently no-oping.
func (p *pgStore) UpdateLastSeen(ctx context.Context, idStr string, at time.Time) error {
	id, err := parseUUID("id", idStr)
	if err != nil {
		return ErrNotFound
	}
	tag, err := p.pool.Exec(ctx, `UPDATE providers SET last_seen_at = $2 WHERE id = $1`, id, at)
	if err != nil {
		return fmt.Errorf("update last_seen_at: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *pgStore) UpdateProvider(ctx context.Context, pr *Provider) error {
	id, err := parseUUID("id", pr.ID)
	if err != nil {
		return err
	}
	tag, err := p.pool.Exec(ctx, `
		UPDATE providers SET
			display_name = $2,
			status = $3,
			platform = $4, architecture = $5, os_version = $6, daemon_version = $7,
			total_memory_mib = $8, cpu_model = $9, cpu_logical_cores = $10, gpu_models = $11,
			docker_available = $12, tart_available = $13,
			public_ip = $14, asn = $15, isp = $16, throughput_mbps = $17, latency_ms = $18,
			region_slug = $19, region_name = $20, country_code = $21,
			supported_types = $22, gpu_enabled = $23, ios_build_enabled = $24,
			public_key = $25, last_seen_at = $26,
			host_macos_version = $27
		WHERE id = $1`,
		id, pr.DisplayName, string(pr.Status),
		nullString(string(pr.HostInfo.Platform)), nullString(pr.HostInfo.Architecture), nullString(pr.HostInfo.OSVersion), nullString(pr.HostInfo.DaemonVersion),
		int64(pr.HostInfo.TotalMemoryMiB), nullString(pr.HostInfo.CPUModel), int32(pr.HostInfo.CPULogicalCores), emptyStrings(pr.HostInfo.GPUModels),
		pr.HostInfo.DockerAvailable, pr.HostInfo.TartAvailable,
		nullString(pr.NetworkInfo.PublicIP), int32(pr.NetworkInfo.ASN), nullString(pr.NetworkInfo.ISP), int32(pr.NetworkInfo.ThroughputMbps), int32(pr.NetworkInfo.LatencyMs),
		nullString(pr.NetworkInfo.RegionSlug), nullString(pr.NetworkInfo.RegionName), nullString(pr.NetworkInfo.CountryCode),
		emptyStrings(pr.Capabilities.SupportedTypes), pr.Capabilities.GPUEnabled, pr.Capabilities.IOSBuildEnabled,
		pr.PublicKey, time.Now().UTC(), int32(pr.Capabilities.HostMacosVersion),
	)
	if err != nil {
		return fmt.Errorf("update provider: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// scanProvider reads a single row in the canonical SELECT order used by
// GetProvider and ListProviders.
func scanProvider(row pgx.Row) (*Provider, error) {
	var (
		id, owner            uuid.UUID
		displayName, status  string
		platform, arch       *string
		osVersion, daemonVer *string
		totalMemMiB          *int64
		cpuModel             *string
		cpuCores             *int32
		gpuModels            []string
		dockerAvail, tartAv  bool
		publicIP             *string
		asn                  *int32
		isp                  *string
		throughputMbps       *int32
		latencyMs            *int32
		regionSlug           *string
		regionName           *string
		countryCode          *string
		supportedTypes       []string
		gpuEnabled, iosBuild bool
		publicKey            []byte
		registeredAt         time.Time
		lastSeenAt           time.Time
		isPrimary            bool
		hostMacosVersion     int32
	)
	if err := row.Scan(
		&id, &owner, &displayName, &status,
		&platform, &arch, &osVersion, &daemonVer,
		&totalMemMiB, &cpuModel, &cpuCores, &gpuModels,
		&dockerAvail, &tartAv,
		&publicIP, &asn, &isp, &throughputMbps, &latencyMs,
		&regionSlug, &regionName, &countryCode,
		&supportedTypes, &gpuEnabled, &iosBuild,
		&publicKey, &registeredAt, &lastSeenAt,
		&isPrimary, &hostMacosVersion,
	); err != nil {
		return nil, err
	}
	p := &Provider{
		ID:           id.String(),
		OwnerUserID:  owner.String(),
		DisplayName:  displayName,
		Status:       Status(status),
		PublicKey:    publicKey,
		RegisteredAt: registeredAt,
		LastSeenAt:   lastSeenAt,
		IsPrimary:    isPrimary,
		HostInfo: HostInfo{
			Platform:        Platform(strPtr(platform)),
			Architecture:    strPtr(arch),
			OSVersion:       strPtr(osVersion),
			DaemonVersion:   strPtr(daemonVer),
			TotalMemoryMiB:  uint64Ptr(totalMemMiB),
			CPUModel:        strPtr(cpuModel),
			CPULogicalCores: uint32Ptr(cpuCores),
			GPUModels:       gpuModels,
			DockerAvailable: dockerAvail,
			TartAvailable:   tartAv,
		},
		NetworkInfo: NetworkInfo{
			PublicIP:       strPtr(publicIP),
			ASN:            uint32Ptr(asn),
			ISP:            strPtr(isp),
			ThroughputMbps: uint32Ptr(throughputMbps),
			LatencyMs:      uint32Ptr(latencyMs),
			RegionSlug:     strPtr(regionSlug),
			RegionName:     strPtr(regionName),
			CountryCode:    strPtr(countryCode),
		},
		Capabilities: Capability{
			SupportedTypes:   supportedTypes,
			GPUEnabled:       gpuEnabled,
			IOSBuildEnabled:  iosBuild,
			HostMacosVersion: uint32(hostMacosVersion),
		},
	}
	return p, nil
}

const selectProviderCols = `
	id, owner_user_id, display_name, status,
	platform, architecture, os_version, daemon_version,
	total_memory_mib, cpu_model, cpu_logical_cores, gpu_models,
	docker_available, tart_available,
	host(public_ip), asn, isp, throughput_mbps, latency_ms,
	region_slug, region_name, country_code,
	supported_types, gpu_enabled, ios_build_enabled,
	public_key, registered_at, last_seen_at,
	is_primary, host_macos_version`

func (p *pgStore) GetProvider(ctx context.Context, idStr string) (*Provider, error) {
	id, err := parseUUID("id", idStr)
	if err != nil {
		// Match in-memory behaviour: unknown id (including malformed)
		// surfaces as ErrNotFound to keep handler error mapping uniform.
		return nil, ErrNotFound
	}
	row := p.pool.QueryRow(ctx, `SELECT `+selectProviderCols+` FROM providers WHERE id = $1`, id)
	prov, err := scanProvider(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get provider: %w", err)
	}
	return prov, nil
}

// GetProviderByOwnerAndDisplayName implements the Store contract — see
// the interface comment in store.go. SQL filter is exact-match on both
// columns so a daemon that re-pairs from the same host (sending the
// same OS hostname as display_name) lands on its existing row instead
// of inserting a duplicate. Returns ErrNotFound when the display_name
// is empty (legacy / hostname-failure path), forcing the caller into
// the INSERT branch rather than colliding on the empty string.
//
// The lookup hits the existing providers_owner_idx and then filters in-
// memory on display_name; the per-owner cardinality is ones-to-tens so
// we keep the index footprint minimal rather than adding a composite
// index that would only ever be hit on this PairDaemon path.
func (p *pgStore) GetProviderByOwnerAndDisplayName(ctx context.Context, ownerUserID, displayName string) (*Provider, error) {
	if displayName == "" {
		return nil, ErrNotFound
	}
	owner, err := parseUUID("ownerUserID", ownerUserID)
	if err != nil {
		return nil, ErrNotFound
	}
	row := p.pool.QueryRow(ctx,
		`SELECT `+selectProviderCols+` FROM providers WHERE owner_user_id = $1 AND display_name = $2 LIMIT 1`,
		owner, displayName,
	)
	prov, err := scanProvider(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get provider by owner+display_name: %w", err)
	}
	return prov, nil
}

// GetProviderByOwnerAndPublicKey implements the Store contract — see
// the interface comment. SQL filter is exact bytes-match on
// (owner_user_id, public_key). Returns ErrNotFound when publicKey is
// empty or the owner_user_id fails UUID parse so the handler falls
// through cleanly to the display_name fallback path. Refs #502.
func (p *pgStore) GetProviderByOwnerAndPublicKey(ctx context.Context, ownerUserID string, publicKey []byte) (*Provider, error) {
	if len(publicKey) == 0 {
		return nil, ErrNotFound
	}
	owner, err := parseUUID("ownerUserID", ownerUserID)
	if err != nil {
		return nil, ErrNotFound
	}
	row := p.pool.QueryRow(ctx,
		`SELECT `+selectProviderCols+` FROM providers WHERE owner_user_id = $1 AND public_key = $2 LIMIT 1`,
		owner, publicKey,
	)
	prov, err := scanProvider(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get provider by owner+public_key: %w", err)
	}
	return prov, nil
}

func (p *pgStore) ListProviders(ctx context.Context, opts ListOptions) ([]*Provider, string, error) {
	args := []any{}
	where := []string{}
	if opts.OwnerUserID != "" {
		owner, err := parseUUID("ownerUserID", opts.OwnerUserID)
		if err != nil {
			return nil, "", err
		}
		args = append(args, owner)
		where = append(where, fmt.Sprintf("owner_user_id = $%d", len(args)))
	}
	if opts.Status != StatusUnspecified {
		args = append(args, string(opts.Status))
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	if opts.PageToken != "" {
		args = append(args, opts.PageToken)
		where = append(where, fmt.Sprintf("id > $%d", len(args)))
	}

	size := opts.PageSize
	if size <= 0 || size > 200 {
		size = 50
	}
	// Fetch size+1 so we can compute whether there's a next page.
	args = append(args, size+1)

	query := `SELECT ` + selectProviderCols + ` FROM providers`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += fmt.Sprintf(" ORDER BY id ASC LIMIT $%d", len(args))

	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list providers: %w", err)
	}
	defer rows.Close()

	out := make([]*Provider, 0, size)
	for rows.Next() {
		prov, err := scanProvider(rows)
		if err != nil {
			return nil, "", fmt.Errorf("scan provider: %w", err)
		}
		out = append(out, prov)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	next := ""
	if len(out) > size {
		next = out[size-1].ID
		out = out[:size]
	}
	return out, next, nil
}

// SetPrimaryProvider atomically swaps the per-owner primary flag in one
// transaction. Caller MUST own providerID — mismatch returns ErrNotFound
// (the handler turns this into PermissionDenied to avoid leaking
// existence to non-owners). See #325 for the multi-daemon ownership bug
// this closes.
//
// SQL contract:
//   1. UPDATE ... SET is_primary=FALSE WHERE owner=$1 AND is_primary
//      (clears any prior primary for this owner; no-op if none)
//   2. UPDATE ... SET is_primary=TRUE  WHERE id=$2 AND owner=$1
//      RETURNING ... (validates ownership in the WHERE clause; 0 rows ⇒
//      caller doesn't own it ⇒ ErrNotFound)
// Both run inside one tx so a concurrent SetPrimary for the same owner
// can't interleave and leave the row with is_primary=false. The partial
// unique index providers_primary_per_owner is a backstop — but the tx
// ordering means we never expect it to fire.
func (p *pgStore) SetPrimaryProvider(ctx context.Context, ownerUserID, providerID string) (*Provider, error) {
	owner, err := parseUUID("ownerUserID", ownerUserID)
	if err != nil {
		return nil, ErrNotFound
	}
	pid, err := parseUUID("providerID", providerID)
	if err != nil {
		return nil, ErrNotFound
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin set-primary tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // committed below

	if _, err := tx.Exec(ctx, `
		UPDATE providers
		   SET is_primary = FALSE
		 WHERE owner_user_id = $1
		   AND is_primary
		   AND id <> $2`, owner, pid); err != nil {
		return nil, fmt.Errorf("clear prior primary: %w", err)
	}

	tag, err := tx.Exec(ctx, `
		UPDATE providers
		   SET is_primary = TRUE
		 WHERE id = $1
		   AND owner_user_id = $2`, pid, owner)
	if err != nil {
		return nil, fmt.Errorf("set primary: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Caller does NOT own this provider id (or the row vanished).
		return nil, ErrNotFound
	}

	// Read back the freshly-promoted row via the canonical SELECT — keeps
	// scanProvider as the single source of truth for column ordering.
	row := tx.QueryRow(ctx, `SELECT `+selectProviderCols+` FROM providers WHERE id = $1`, pid)
	prov, err := scanProvider(row)
	if err != nil {
		return nil, fmt.Errorf("read back promoted row: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit set-primary: %w", err)
	}
	return prov, nil
}

// SelectProviderForOwner returns the deterministic "which paired daemon
// answers for this user" pick for /provide/* surfaces. Returns (nil, nil)
// when the owner has zero rows (the empty-state path, #313). See #325.
func (p *pgStore) SelectProviderForOwner(ctx context.Context, ownerUserID string) (*Provider, error) {
	if ownerUserID == "" {
		return nil, nil
	}
	owner, err := parseUUID("ownerUserID", ownerUserID)
	if err != nil {
		// Malformed user id from the auth layer is a 500-class bug, not
		// a user-visible state. Surfacing nil/nil would silently render
		// the empty-state instead of escalating.
		return nil, fmt.Errorf("select provider for owner: %w", err)
	}
	row := p.pool.QueryRow(ctx, `
		SELECT `+selectProviderCols+`
		  FROM providers
		 WHERE owner_user_id = $1
		 ORDER BY is_primary DESC,
		          last_seen_at DESC NULLS LAST,
		          registered_at DESC,
		          id ASC
		 LIMIT 1`, owner)
	prov, err := scanProvider(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select provider for owner: %w", err)
	}
	return prov, nil
}

func (p *pgStore) DeactivateProvider(ctx context.Context, idStr, reason string) error {
	id, err := parseUUID("id", idStr)
	if err != nil {
		return ErrNotFound
	}
	tag, err := p.pool.Exec(ctx, `UPDATE providers SET status = $2 WHERE id = $1`, id, string(StatusDeactivated))
	if err != nil {
		return fmt.Errorf("deactivate provider: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	// Audit event — best-effort, never block on subscribers.
	_ = p.AppendAuditEvent(context.Background(), AuditEvent{
		ID:         uuid.NewString(),
		ProviderID: idStr,
		Kind:       "EVENT_KIND_SCHEDULER_TRANSITION",
		OccurredAt: time.Now().UTC(),
		Metadata:   map[string]string{"reason": reason, "transition": "deactivated"},
	})
	return nil
}

// --- pairing tokens --------------------------------------------------------

func (p *pgStore) IssuePairingToken(ctx context.Context, ownerUserID string, ttl time.Duration) (string, error) {
	owner, err := parseUUID("ownerUserID", ownerUserID)
	if err != nil {
		return "", err
	}
	if ttl == 0 {
		ttl = 15 * time.Minute
	}
	tok := strings.ReplaceAll(uuid.NewString(), "-", "")
	now := time.Now().UTC()
	_, err = p.pool.Exec(ctx, `
		INSERT INTO pairing_tokens (token, owner_user_id, issued_at, expires_at)
		VALUES ($1, $2, $3, $4)`,
		tok, owner, now, now.Add(ttl))
	if err != nil {
		return "", fmt.Errorf("insert pairing token: %w", err)
	}
	return tok, nil
}

func (p *pgStore) ConsumePairingToken(ctx context.Context, token string) (PairingToken, error) {
	// Atomic single-use: UPDATE ... RETURNING gates double-consume and
	// expiry in one round trip.
	var (
		ownerID  uuid.UUID
		issued   time.Time
		expires  time.Time
		consumed time.Time
	)
	now := time.Now().UTC()
	err := p.pool.QueryRow(ctx, `
		UPDATE pairing_tokens
		   SET consumed_at = $2
		 WHERE token = $1
		   AND consumed_at IS NULL
		   AND expires_at > $2
		RETURNING owner_user_id, issued_at, expires_at, consumed_at`,
		token, now,
	).Scan(&ownerID, &issued, &expires, &consumed)
	if errors.Is(err, pgx.ErrNoRows) {
		return PairingToken{}, ErrTokenInvalid
	}
	if err != nil {
		return PairingToken{}, fmt.Errorf("consume pairing token: %w", err)
	}
	return PairingToken{
		Token:       token,
		OwnerUserID: ownerID.String(),
		IssuedAt:    issued,
		ExpiresAt:   expires,
		ConsumedAt:  &consumed,
	}, nil
}

// --- scheduling configs ----------------------------------------------------

func (p *pgStore) GetSchedulingConfig(ctx context.Context, providerID string) (*SchedulingConfig, error) {
	id, err := parseUUID("providerID", providerID)
	if err != nil {
		// Returning defaultConfig for malformed IDs matches the
		// in-memory store which never errors on missing rows.
		return defaultConfig(providerID), nil
	}
	var (
		bandwidthCapGB, cpuCapPct, memoryCapPct           int32
		gpuIdle, gpuActive                                int32
		calendarJSON                                      []byte
		idleEnabled                                       bool
		idleThreshSecs                                    int32
		allowed, disallowed, blocklist                    []string
		perCustCap                                        int32
		updatedAt                                         time.Time
		updatedByRaw                                      *uuid.UUID
	)
	err = p.pool.QueryRow(ctx, `
		SELECT bandwidth_cap_gb, cpu_cap_pct, memory_cap_pct,
		       gpu_cap_when_idle_pct, gpu_cap_when_active_pct,
		       calendar_json, idle_enabled, idle_threshold_secs,
		       allowed_categories, disallowed_categories, destination_blocklist,
		       per_customer_minutes_cap, updated_at, updated_by_user_id
		  FROM scheduling_configs
		 WHERE provider_id = $1`, id,
	).Scan(&bandwidthCapGB, &cpuCapPct, &memoryCapPct,
		&gpuIdle, &gpuActive,
		&calendarJSON, &idleEnabled, &idleThreshSecs,
		&allowed, &disallowed, &blocklist,
		&perCustCap, &updatedAt, &updatedByRaw,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return defaultConfig(providerID), nil
	}
	if err != nil {
		return nil, fmt.Errorf("get scheduling config: %w", err)
	}
	windows := []CalendarWindow{}
	if len(calendarJSON) > 0 {
		_ = json.Unmarshal(calendarJSON, &windows)
	}
	updatedBy := ""
	if updatedByRaw != nil {
		updatedBy = updatedByRaw.String()
	}
	return &SchedulingConfig{
		ProviderID:            providerID,
		BandwidthCapGB:        uint32(bandwidthCapGB),
		CPUCapPct:             uint32(cpuCapPct),
		MemoryCapPct:          uint32(memoryCapPct),
		GPUCapWhenIdlePct:     uint32(gpuIdle),
		GPUCapWhenActivePct:   uint32(gpuActive),
		CalendarWindows:       windows,
		IdleEnabled:           idleEnabled,
		IdleThresholdSecs:     uint32(idleThreshSecs),
		AllowedCategories:     allowed,
		DisallowedCategories:  disallowed,
		DestinationBlocklist:  blocklist,
		PerCustomerMinutesCap: uint32(perCustCap),
		UpdatedAt:             updatedAt,
		UpdatedByUserID:       updatedBy,
	}, nil
}

func (p *pgStore) UpdateSchedulingConfig(ctx context.Context, cfg *SchedulingConfig) (*SchedulingConfig, error) {
	id, err := parseUUID("providerID", cfg.ProviderID)
	if err != nil {
		return nil, err
	}
	calendarJSON, err := json.Marshal(emptyWindows(cfg.CalendarWindows))
	if err != nil {
		return nil, fmt.Errorf("marshal calendar: %w", err)
	}
	now := time.Now().UTC()
	cfg.UpdatedAt = now
	_, err = p.pool.Exec(ctx, `
		INSERT INTO scheduling_configs (
			provider_id,
			bandwidth_cap_gb, cpu_cap_pct, memory_cap_pct,
			gpu_cap_when_idle_pct, gpu_cap_when_active_pct,
			calendar_json, idle_enabled, idle_threshold_secs,
			allowed_categories, disallowed_categories, destination_blocklist,
			per_customer_minutes_cap, updated_at, updated_by_user_id
		) VALUES (
			$1,
			$2, $3, $4,
			$5, $6,
			$7, $8, $9,
			$10, $11, $12,
			$13, $14, $15
		)
		ON CONFLICT (provider_id) DO UPDATE SET
			bandwidth_cap_gb        = EXCLUDED.bandwidth_cap_gb,
			cpu_cap_pct             = EXCLUDED.cpu_cap_pct,
			memory_cap_pct          = EXCLUDED.memory_cap_pct,
			gpu_cap_when_idle_pct   = EXCLUDED.gpu_cap_when_idle_pct,
			gpu_cap_when_active_pct = EXCLUDED.gpu_cap_when_active_pct,
			calendar_json           = EXCLUDED.calendar_json,
			idle_enabled            = EXCLUDED.idle_enabled,
			idle_threshold_secs     = EXCLUDED.idle_threshold_secs,
			allowed_categories      = EXCLUDED.allowed_categories,
			disallowed_categories   = EXCLUDED.disallowed_categories,
			destination_blocklist   = EXCLUDED.destination_blocklist,
			per_customer_minutes_cap = EXCLUDED.per_customer_minutes_cap,
			updated_at              = EXCLUDED.updated_at,
			updated_by_user_id      = EXCLUDED.updated_by_user_id`,
		id,
		int32(cfg.BandwidthCapGB), int32(cfg.CPUCapPct), int32(cfg.MemoryCapPct),
		int32(cfg.GPUCapWhenIdlePct), int32(cfg.GPUCapWhenActivePct),
		calendarJSON, cfg.IdleEnabled, int32(cfg.IdleThresholdSecs),
		emptyStrings(cfg.AllowedCategories), emptyStrings(cfg.DisallowedCategories), emptyStrings(cfg.DestinationBlocklist),
		int32(cfg.PerCustomerMinutesCap), now, nullUUID(cfg.UpdatedByUserID),
	)
	if err != nil {
		return nil, fmt.Errorf("upsert scheduling config: %w", err)
	}
	out := *cfg
	return &out, nil
}

// --- audit + earnings ------------------------------------------------------

func (p *pgStore) AppendAuditEvent(ctx context.Context, e AuditEvent) error {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	eid, err := parseUUID("id", e.ID)
	if err != nil {
		return err
	}
	pid, err := parseUUID("providerID", e.ProviderID)
	if err != nil {
		return err
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
	metaJSON, err := json.Marshal(emptyMap(e.Metadata))
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	_, err = p.pool.Exec(ctx, `
		INSERT INTO audit_events (
			id, provider_id, kind, occurred_at,
			workload_type, category, customer_display_name, destination_summary,
			bytes, metadata
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7, $8,
			$9, $10
		)`,
		eid, pid, e.Kind, e.OccurredAt,
		nullString(e.WorkloadType), nullString(e.Category), nullString(e.CustomerDisplayName), nullString(e.DestinationSummary),
		int64(e.Bytes), metaJSON,
	)
	if err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}

	// Best-effort fan-out to in-process subscribers (mirrors memStore).
	p.subsMu.Lock()
	subs := append([]chan AuditEvent(nil), p.subs[e.ProviderID]...)
	p.subsMu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- e:
		default:
		}
	}
	return nil
}

func (p *pgStore) ListAuditEvents(ctx context.Context, providerID string, q AuditQuery) ([]AuditEvent, string, error) {
	pid, err := parseUUID("providerID", providerID)
	if err != nil {
		return nil, "", err
	}
	args := []any{pid}
	where := []string{fmt.Sprintf("provider_id = $%d", len(args))}
	if !q.From.IsZero() {
		args = append(args, q.From)
		where = append(where, fmt.Sprintf("occurred_at >= $%d", len(args)))
	}
	if !q.To.IsZero() {
		args = append(args, q.To)
		where = append(where, fmt.Sprintf("occurred_at < $%d", len(args)))
	}
	if len(q.Kinds) > 0 {
		args = append(args, q.Kinds)
		where = append(where, fmt.Sprintf("kind = ANY($%d)", len(args)))
	}
	if q.PageToken != "" {
		args = append(args, q.PageToken)
		where = append(where, fmt.Sprintf("id::text > $%d", len(args)))
	}

	size := q.PageSize
	if size <= 0 || size > 500 {
		size = 100
	}
	args = append(args, size+1)

	query := `
		SELECT id, provider_id, kind, occurred_at,
		       workload_type, category, customer_display_name, destination_summary,
		       bytes, metadata
		  FROM audit_events
		 WHERE ` + strings.Join(where, " AND ") + `
		 ORDER BY occurred_at ASC, id ASC
		 LIMIT $` + fmt.Sprint(len(args))

	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()

	out := make([]AuditEvent, 0, size)
	for rows.Next() {
		var (
			id, prov                                            uuid.UUID
			kind                                                string
			occurredAt                                          time.Time
			workloadType, category, customerName, destSummary   *string
			bytesCount                                          int64
			metaJSON                                            []byte
		)
		if err := rows.Scan(&id, &prov, &kind, &occurredAt,
			&workloadType, &category, &customerName, &destSummary,
			&bytesCount, &metaJSON); err != nil {
			return nil, "", fmt.Errorf("scan audit event: %w", err)
		}
		meta := map[string]string{}
		if len(metaJSON) > 0 {
			_ = json.Unmarshal(metaJSON, &meta)
		}
		out = append(out, AuditEvent{
			ID:                  id.String(),
			ProviderID:          prov.String(),
			Kind:                kind,
			OccurredAt:          occurredAt,
			WorkloadType:        strPtr(workloadType),
			Category:            strPtr(category),
			CustomerDisplayName: strPtr(customerName),
			DestinationSummary:  strPtr(destSummary),
			Bytes:               uint64(bytesCount),
			Metadata:            meta,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	next := ""
	if len(out) > size {
		next = out[size-1].ID
		out = out[:size]
	}
	return out, next, nil
}

func (p *pgStore) SubscribeAuditEvents(providerID string) (<-chan AuditEvent, func()) {
	ch := make(chan AuditEvent, 64)
	p.subsMu.Lock()
	p.subs[providerID] = append(p.subs[providerID], ch)
	p.subsMu.Unlock()

	cancel := func() {
		p.subsMu.Lock()
		defer p.subsMu.Unlock()
		remaining := p.subs[providerID][:0]
		for _, c := range p.subs[providerID] {
			if c != ch {
				remaining = append(remaining, c)
			}
		}
		p.subs[providerID] = remaining
		close(ch)
	}
	return ch, cancel
}

func (p *pgStore) CreditEarnings(ctx context.Context, e EarningsEntry) error {
	pid, err := parseUUID("providerID", e.ProviderID)
	if err != nil {
		return err
	}
	if e.Currency == "" {
		// Phase-0 native ledger currency is $GRID (Solana SPL). Off-ramp
		// to USD/EUR/etc happens at withdraw time via billing-svc's
		// monthly cron — never at credit time. Storing USD by default
		// here would lie about the unit and break the headline rendering
		// in /provide/earnings (#312).
		e.Currency = "GRID"
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
	_, err = p.pool.Exec(ctx, `
		INSERT INTO earnings_entries (provider_id, workload_type, occurred_at, currency, micros)
		VALUES ($1, $2, $3, $4, $5)`,
		pid, e.WorkloadType, e.OccurredAt, e.Currency, e.Micros)
	if err != nil {
		return fmt.Errorf("insert earnings: %w", err)
	}
	return nil
}

func (p *pgStore) SumEarnings(ctx context.Context, providerID string, from, to time.Time) (int64, map[string]int64, string, error) {
	pid, err := parseUUID("providerID", providerID)
	if err != nil {
		return 0, nil, "GRID", err
	}
	args := []any{pid}
	where := []string{fmt.Sprintf("provider_id = $%d", len(args))}
	if !from.IsZero() {
		args = append(args, from)
		where = append(where, fmt.Sprintf("occurred_at >= $%d", len(args)))
	}
	if !to.IsZero() {
		args = append(args, to)
		where = append(where, fmt.Sprintf("occurred_at < $%d", len(args)))
	}

	rows, err := p.pool.Query(ctx, `
		SELECT workload_type, currency, SUM(micros)::BIGINT AS total
		  FROM earnings_entries
		 WHERE `+strings.Join(where, " AND ")+`
		 GROUP BY workload_type, currency`, args...)
	if err != nil {
		return 0, nil, "GRID", fmt.Errorf("sum earnings: %w", err)
	}
	defer rows.Close()

	var total int64
	byType := make(map[string]int64)
	currency := ""
	for rows.Next() {
		var (
			wt, cur string
			sum     int64
		)
		if err := rows.Scan(&wt, &cur, &sum); err != nil {
			return 0, nil, "GRID", fmt.Errorf("scan earnings: %w", err)
		}
		total += sum
		byType[wt] += sum
		if currency == "" {
			currency = cur
		}
	}
	if err := rows.Err(); err != nil {
		return 0, nil, "GRID", err
	}
	if currency == "" {
		// Empty result set (Phase 0: no workload completions credited
		// yet for this provider). The native ledger currency is $GRID,
		// not USD — USD would mis-label the headline card in
		// /provide/earnings and break the 0-$GRID empty-state copy
		// the founder DoD calls for (#312).
		currency = "GRID"
	}
	return total, byType, currency, nil
}

// --- small helpers ---------------------------------------------------------

func strPtr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func uint32Ptr(p *int32) uint32 {
	if p == nil {
		return 0
	}
	return uint32(*p)
}
func uint64Ptr(p *int64) uint64 {
	if p == nil {
		return 0
	}
	return uint64(*p)
}

func emptyWindows(in []CalendarWindow) []CalendarWindow {
	if in == nil {
		return []CalendarWindow{}
	}
	return in
}

func emptyMap(in map[string]string) map[string]string {
	if in == nil {
		return map[string]string{}
	}
	return in
}

// isUniqueViolation returns true for Postgres unique-constraint errors
// (SQLSTATE 23505). Used to translate INSERT collisions into ErrAlreadyExists.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// pgx wraps errors via the pgconn.PgError type. Avoid importing pgconn
	// just for this check; string-match the SQLSTATE which is stable.
	return strings.Contains(err.Error(), "SQLSTATE 23505") ||
		strings.Contains(err.Error(), "23505")
}
