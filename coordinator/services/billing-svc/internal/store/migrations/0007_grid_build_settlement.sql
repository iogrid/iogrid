-- +goose Up
-- +goose StatementBegin
-- $GRID BUILD settlement table — the provider-earnings ledger for iOS
-- builds run through iogrid (G2/G3). Mirrors grid_settlement but keyed on
-- (build_id, attempt_id) instead of a session id. The build meter (#700/
-- #707) writes one row per build attempt that incurred consumption; the
-- SAME settlement-worker drains it (batched by provider wallet) with the
-- same locked 85/15 split. All atomic columns are BIGINT (9-decimal $GRID).
CREATE TABLE IF NOT EXISTS grid_build_settlement (
    id                  UUID PRIMARY KEY,
    build_id            UUID NOT NULL,
    attempt_id          UUID NOT NULL,
    customer_wallet     VARCHAR(64) NOT NULL,
    provider_wallet     VARCHAR(64),               -- nullable until the provider's wallet binding is known
    provider_id         UUID,                       -- denormalised for ops
    escrowed_atomic     BIGINT NOT NULL,
    consumed_atomic     BIGINT NOT NULL,
    refund_atomic       BIGINT NOT NULL,
    provider_share      BIGINT NOT NULL,
    iogrid_share        BIGINT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    settled_at          TIMESTAMPTZ,
    tx_signature        VARCHAR(128),
    settle_attempts     INT NOT NULL DEFAULT 0,
    last_error          TEXT,
    -- one settlement per build attempt — a build-gateway retry POSTing the
    -- same (build_id, attempt_id) is an idempotent no-op (can't double-pay).
    CONSTRAINT grid_build_settlement_attempt_uniq UNIQUE (build_id, attempt_id),
    CONSTRAINT grid_build_settlement_atomic_nonneg CHECK (
        escrowed_atomic >= 0 AND consumed_atomic >= 0 AND refund_atomic >= 0
        AND provider_share >= 0 AND iogrid_share >= 0
    )
);

CREATE INDEX IF NOT EXISTS grid_build_settlement_provider_wallet_idx
    ON grid_build_settlement(provider_wallet)
    WHERE settled_at IS NULL AND provider_wallet IS NOT NULL;

CREATE INDEX IF NOT EXISTS grid_build_settlement_created_idx
    ON grid_build_settlement(created_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS grid_build_settlement;
-- +goose StatementEnd
