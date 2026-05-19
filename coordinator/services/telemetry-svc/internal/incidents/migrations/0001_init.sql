-- +goose Up
-- +goose StatementBegin

-- pgcrypto for gen_random_uuid().
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- incidents: one row per operator-curated incident on the public
-- status page. We model the StatusPage.io classic lifecycle:
--   investigating → identified → monitoring → resolved
-- plus an open `impact` axis (none / minor / major / critical) that the
-- frontend uses to colour-code the headline.
--
-- An incident may affect one or more services (string keys matching the
-- SLO catalogue's `service` field). We keep `affected_services` as a
-- TEXT[] rather than a join table — the cardinality is tiny (handful of
-- services per incident) and the query path is "list all incidents
-- touching service X" which is a single GIN-indexable predicate.
CREATE TYPE incident_impact AS ENUM ('none', 'minor', 'major', 'critical');
CREATE TYPE incident_status AS ENUM ('investigating', 'identified', 'monitoring', 'resolved');

CREATE TABLE incidents (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title              TEXT NOT NULL,
    body               TEXT NOT NULL DEFAULT '',
    status             incident_status NOT NULL DEFAULT 'investigating',
    impact             incident_impact NOT NULL DEFAULT 'minor',
    affected_services  TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    started_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at        TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX incidents_active_idx ON incidents (started_at DESC) WHERE resolved_at IS NULL;
CREATE INDEX incidents_recent_idx ON incidents (started_at DESC);
CREATE INDEX incidents_services_idx ON incidents USING GIN (affected_services);

-- incident_updates: append-only chronological updates per incident.
-- The status page renders these as a "history strip" under the incident
-- card.
CREATE TABLE incident_updates (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    incident_id UUID NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
    status      incident_status NOT NULL,
    body        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX incident_updates_incident_idx ON incident_updates (incident_id, created_at DESC);

-- subscriptions: email subscribers for incident notifications. The
-- /status/subscribe endpoint upserts here. Email delivery is wired
-- elsewhere (notification-svc) — this is just the registry.
CREATE TABLE status_subscriptions (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email          TEXT NOT NULL,
    verified       BOOLEAN NOT NULL DEFAULT FALSE,
    verify_token   TEXT NOT NULL DEFAULT '',
    services_filter TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    verified_at    TIMESTAMPTZ,
    unsubscribed_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX status_subscriptions_email_uq
    ON status_subscriptions (LOWER(email))
    WHERE unsubscribed_at IS NULL;

-- uptime_samples: daily roll-up of per-service "did we breach SLO this
-- day?" used to render the 90-day calendar heatmap. The columns are
-- intentionally small — a synthetic worker writes one row per
-- (service, day) at UTC midnight by querying Mimir's
-- slo:burn_rate:long:* recording rules.
--
-- `state` is one of:
--   'op'    — fully operational, all SLOs within budget
--   'deg'   — degraded, at least one SLO burning at >2x
--   'down'  — major outage, at least one SLO burning at >14x for >5m
--   'maint' — planned maintenance window (operator-set)
--   ''      — no data (white square on the heatmap)
CREATE TABLE uptime_samples (
    service  TEXT NOT NULL,
    day      DATE NOT NULL,
    state    TEXT NOT NULL DEFAULT 'op',
    sli_pct  DOUBLE PRECISION NOT NULL DEFAULT 100.0,
    PRIMARY KEY (service, day)
);
CREATE INDEX uptime_samples_day_idx ON uptime_samples (day DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS uptime_samples;
DROP TABLE IF EXISTS status_subscriptions;
DROP TABLE IF EXISTS incident_updates;
DROP TABLE IF EXISTS incidents;
DROP TYPE  IF EXISTS incident_status;
DROP TYPE  IF EXISTS incident_impact;
-- +goose StatementEnd
