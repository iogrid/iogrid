-- +goose Up
-- +goose StatementBegin
--
-- Schema for the billing-svc bounded context.
-- Postgres-per-service (TECH.md §"Database strategy"): no cross-service
-- joins, the only foreign keys are intra-schema. The workspace_id /
-- user_id / provider_id columns are UUIDs minted upstream by
-- identity-svc; we treat them as opaque strings here.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ─── Customer subscriptions (Stripe) ──────────────────────────────────
CREATE TABLE IF NOT EXISTS subscription (
    id                      UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id            UUID         NOT NULL,
    tier                    TEXT         NOT NULL,
    status                  TEXT         NOT NULL,
    stripe_customer_id      TEXT         NOT NULL,
    stripe_subscription_id  TEXT         NOT NULL,
    current_period_start    TIMESTAMPTZ  NULL,
    current_period_end      TIMESTAMPTZ  NULL,
    created_at              TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ  NOT NULL DEFAULT now(),
    canceled_at             TIMESTAMPTZ  NULL,
    UNIQUE (stripe_subscription_id)
);
CREATE INDEX IF NOT EXISTS subscription_workspace_idx
    ON subscription (workspace_id);
CREATE INDEX IF NOT EXISTS subscription_customer_idx
    ON subscription (stripe_customer_id);

CREATE TABLE IF NOT EXISTS invoice (
    id                  UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id        UUID          NOT NULL,
    stripe_invoice_id   TEXT          NOT NULL UNIQUE,
    period_start        TIMESTAMPTZ   NOT NULL,
    period_end          TIMESTAMPTZ   NOT NULL,
    subtotal_cents      BIGINT        NOT NULL,
    tax_cents           BIGINT        NOT NULL DEFAULT 0,
    total_cents         BIGINT        NOT NULL,
    currency            TEXT          NOT NULL,
    status              TEXT          NOT NULL,
    hosted_invoice_url  TEXT          NULL,
    issued_at           TIMESTAMPTZ   NULL,
    paid_at             TIMESTAMPTZ   NULL
);
CREATE INDEX IF NOT EXISTS invoice_workspace_idx
    ON invoice (workspace_id);

-- ─── Provider payouts (Stripe Connect — cash tier) ────────────────────
CREATE TABLE IF NOT EXISTS payout_account (
    id                          UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                     UUID          NOT NULL UNIQUE,
    stripe_connect_account_id   TEXT          NOT NULL UNIQUE,
    status                      TEXT          NOT NULL,
    country_code                TEXT          NULL,
    default_currency            TEXT          NULL,
    onboarded_at                TIMESTAMPTZ   NULL,
    last_payout_at              TIMESTAMPTZ   NULL,
    created_at                  TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS payout (
    id                  UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID          NOT NULL,
    payout_account_id   UUID          NOT NULL REFERENCES payout_account(id) ON DELETE CASCADE,
    amount_cents        BIGINT        NOT NULL,
    currency            TEXT          NOT NULL,
    status              TEXT          NOT NULL,
    stripe_payout_id    TEXT          NULL,
    period_start        TIMESTAMPTZ   NOT NULL,
    period_end          TIMESTAMPTZ   NOT NULL,
    initiated_at        TIMESTAMPTZ   NOT NULL DEFAULT now(),
    settled_at          TIMESTAMPTZ   NULL,
    failure_reason      TEXT          NULL
);
CREATE INDEX IF NOT EXISTS payout_user_idx
    ON payout (user_id);

-- ─── Metering aggregation (per-customer, per-provider) ────────────────
CREATE TABLE IF NOT EXISTS usage_event (
    id               UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     UUID          NOT NULL,
    provider_id      UUID          NULL,
    workload_id      UUID          NOT NULL,
    workload_type    TEXT          NOT NULL,
    quantity         BIGINT        NOT NULL,
    cost_cents       BIGINT        NOT NULL,
    currency         TEXT          NOT NULL DEFAULT 'USD',
    recorded_at      TIMESTAMPTZ   NOT NULL,
    UNIQUE (workload_id)        -- dedupe at-least-once delivery
);
CREATE INDEX IF NOT EXISTS usage_event_workspace_day_idx
    ON usage_event (workspace_id, recorded_at);
CREATE INDEX IF NOT EXISTS usage_event_provider_day_idx
    ON usage_event (provider_id, recorded_at);

CREATE TABLE IF NOT EXISTS usage_aggregate_daily (
    day              DATE          NOT NULL,
    workspace_id     UUID          NULL,
    provider_id      UUID          NULL,
    workload_type    TEXT          NOT NULL,
    quantity         BIGINT        NOT NULL,
    cost_cents       BIGINT        NOT NULL,
    currency         TEXT          NOT NULL,
    rolled_up_at     TIMESTAMPTZ   NOT NULL DEFAULT now(),
    PRIMARY KEY (day, workspace_id, provider_id, workload_type)
);

-- ─── Solana payouts + burns ───────────────────────────────────────────
CREATE TABLE IF NOT EXISTS solana_payout (
    id              UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID          NOT NULL,
    wallet_address  TEXT          NOT NULL,
    amount_lamports BIGINT        NOT NULL,           -- $GRID in smallest unit
    usd_value_cents BIGINT        NOT NULL,
    tx_signature    TEXT          NULL,
    status          TEXT          NOT NULL,
    period_start    TIMESTAMPTZ   NOT NULL,
    period_end      TIMESTAMPTZ   NOT NULL,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    settled_at      TIMESTAMPTZ   NULL
);
CREATE INDEX IF NOT EXISTS solana_payout_user_idx
    ON solana_payout (user_id);

CREATE TABLE IF NOT EXISTS solana_burn (
    id              UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    period_start    TIMESTAMPTZ   NOT NULL,
    period_end      TIMESTAMPTZ   NOT NULL,
    usd_value_cents BIGINT        NOT NULL,
    amount_lamports BIGINT        NOT NULL,
    tx_signature    TEXT          NULL,
    status          TEXT          NOT NULL,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now()
);

-- ─── Tax reports ──────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS tax_report (
    id           UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID          NOT NULL,
    tax_year     INT           NOT NULL,
    quarter      INT           NOT NULL,             -- 1..4
    form_type    TEXT          NOT NULL,             -- '1099-NEC' | 'GRID-1099-equiv'
    cash_cents   BIGINT        NOT NULL DEFAULT 0,
    token_cents  BIGINT        NOT NULL DEFAULT 0,
    pdf_bytes    BYTEA         NULL,
    issued_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (user_id, tax_year, quarter, form_type)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS tax_report;
DROP TABLE IF EXISTS solana_burn;
DROP TABLE IF EXISTS solana_payout;
DROP TABLE IF EXISTS usage_aggregate_daily;
DROP TABLE IF EXISTS usage_event;
DROP TABLE IF EXISTS payout;
DROP TABLE IF EXISTS payout_account;
DROP TABLE IF EXISTS invoice;
DROP TABLE IF EXISTS subscription;
-- +goose StatementEnd
