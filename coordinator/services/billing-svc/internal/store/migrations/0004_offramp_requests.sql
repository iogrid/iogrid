-- +goose Up
-- +goose StatementBegin
--
-- offramp_requests — per-attempt record of a provider asking iogrid to
-- swap their $GRID earnings to fiat via a partner off-ramp (MoonPay,
-- Sociable Cash, future Coinbase, …).
--
-- Lifecycle (status column):
--
--   pending      — request created, redirect URL returned, browser sent
--   swapping     — partner is running the $GRID → USDC swap on chain
--   off-ramping  — partner is settling fiat to the user's bank
--   completed    — partner reported fiat hit the user's account
--   failed       — any terminal failure (swap rejected, KYC declined, …)
--
-- The row is created when gateway-bff calls billing-svc.StartOffRamp.
-- It transitions on partner webhook delivery
-- (POST /api/v1/webhooks/offramp/{provider_name}). The webhook handler
-- verifies the partner's signature THEN updates this row.

CREATE TABLE IF NOT EXISTS offramp_request (
    id                UUID PRIMARY KEY,
    user_id           UUID        NOT NULL,
    provider_name     TEXT        NOT NULL,
    provider_ref_id   TEXT        NULL,
    wallet_address    TEXT        NOT NULL,
    grid_amount       BIGINT      NOT NULL,
    fiat_amount       TEXT        NULL,
    fiat_currency     TEXT        NOT NULL DEFAULT 'USD',
    status            TEXT        NOT NULL DEFAULT 'pending',
    redirect_url      TEXT        NOT NULL,
    return_url        TEXT        NULL,
    txn_signature     TEXT        NULL,
    error_message     TEXT        NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at      TIMESTAMPTZ NULL,
    CHECK (grid_amount > 0),
    CHECK (status IN ('pending','swapping','off-ramping','completed','failed'))
);

-- One row per partner reference id so re-delivered webhooks can be
-- idempotently matched without scanning the table.
CREATE UNIQUE INDEX IF NOT EXISTS offramp_request_provider_ref_uniq
    ON offramp_request (provider_name, provider_ref_id)
    WHERE provider_ref_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS offramp_request_user_idx
    ON offramp_request (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS offramp_request_status_idx
    ON offramp_request (status, created_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS offramp_request_status_idx;
DROP INDEX IF EXISTS offramp_request_user_idx;
DROP INDEX IF EXISTS offramp_request_provider_ref_uniq;
DROP TABLE IF EXISTS offramp_request;
-- +goose StatementEnd
