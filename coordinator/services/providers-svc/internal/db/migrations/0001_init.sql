-- providers-svc initial schema.
--
-- One DB per service (postgres-per-service); the physical CNPG cluster is
-- shared across the coordinator. Keep this file additive — every new
-- column/index lands as a separate numbered migration so rollouts can be
-- rolled back without losing data.

-- +goose Up
-- +goose StatementBegin

CREATE TABLE providers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_user_id   UUID NOT NULL,
    display_name    TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'active',
    platform        TEXT,
    architecture    TEXT,
    os_version      TEXT,
    daemon_version  TEXT,
    total_memory_mib BIGINT,
    cpu_model       TEXT,
    cpu_logical_cores INTEGER,
    gpu_models      TEXT[],
    docker_available BOOLEAN NOT NULL DEFAULT FALSE,
    tart_available  BOOLEAN NOT NULL DEFAULT FALSE,
    public_ip       INET,
    asn             INTEGER,
    isp             TEXT,
    throughput_mbps INTEGER,
    latency_ms      INTEGER,
    region_slug     TEXT,
    region_name     TEXT,
    country_code    CHAR(2),
    supported_types TEXT[] NOT NULL DEFAULT '{}',
    gpu_enabled     BOOLEAN NOT NULL DEFAULT FALSE,
    ios_build_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    public_key      BYTEA,
    registered_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX providers_owner_idx ON providers(owner_user_id);
CREATE INDEX providers_status_idx ON providers(status);
CREATE INDEX providers_region_idx ON providers(region_slug);

CREATE TABLE pairing_tokens (
    token           TEXT PRIMARY KEY,
    owner_user_id   UUID NOT NULL,
    issued_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ NOT NULL,
    consumed_at     TIMESTAMPTZ
);
CREATE INDEX pairing_tokens_owner_idx ON pairing_tokens(owner_user_id);

CREATE TABLE scheduling_configs (
    provider_id            UUID PRIMARY KEY,
    bandwidth_cap_gb       INTEGER NOT NULL DEFAULT 50,
    cpu_cap_pct            INTEGER NOT NULL DEFAULT 30,
    memory_cap_pct         INTEGER NOT NULL DEFAULT 25,
    gpu_cap_when_idle_pct  INTEGER NOT NULL DEFAULT 100,
    gpu_cap_when_active_pct INTEGER NOT NULL DEFAULT 0,
    calendar_json          JSONB NOT NULL DEFAULT '[]'::jsonb,
    idle_enabled           BOOLEAN NOT NULL DEFAULT TRUE,
    idle_threshold_secs    INTEGER NOT NULL DEFAULT 300,
    allowed_categories     TEXT[] NOT NULL DEFAULT '{}',
    disallowed_categories  TEXT[] NOT NULL DEFAULT '{}',
    destination_blocklist  TEXT[] NOT NULL DEFAULT '{}',
    per_customer_minutes_cap INTEGER NOT NULL DEFAULT 0,
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by_user_id     UUID
);

CREATE TABLE audit_events (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id             UUID NOT NULL,
    kind                    TEXT NOT NULL,
    occurred_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    workload_type           TEXT,
    category                TEXT,
    customer_display_name   TEXT,
    destination_summary     TEXT,
    bytes                   BIGINT NOT NULL DEFAULT 0,
    metadata                JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX audit_events_provider_time_idx ON audit_events(provider_id, occurred_at DESC);
CREATE INDEX audit_events_kind_idx ON audit_events(kind);

CREATE TABLE earnings_entries (
    id              BIGSERIAL PRIMARY KEY,
    provider_id     UUID NOT NULL,
    workload_type   TEXT NOT NULL,
    occurred_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    currency        CHAR(3) NOT NULL DEFAULT 'USD',
    micros          BIGINT NOT NULL
);
CREATE INDEX earnings_provider_time_idx ON earnings_entries(provider_id, occurred_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS earnings_entries;
DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS scheduling_configs;
DROP TABLE IF EXISTS pairing_tokens;
DROP TABLE IF EXISTS providers;
-- +goose StatementEnd
