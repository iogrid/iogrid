-- +goose Up
-- +goose StatementBegin
-- $GRID settlement table — one row per VPN session that incurred any
-- consumption. The settlement-worker (Track 5 / #598) batches rows by
-- provider wallet every 5 minutes and submits a single SPL TransferChecked
-- per (wallet, tick). burn meter (#597) writes the row at the end of each
-- session; commission goes into a separate grid_commission_balance row.
--
-- All atomic-unit columns are BIGINT (9-decimal $GRID lamports).
CREATE TABLE IF NOT EXISTS grid_settlement (
    id                  UUID PRIMARY KEY,
    session_id          UUID NOT NULL UNIQUE,
    customer_wallet     VARCHAR(64) NOT NULL,
    provider_wallet     VARCHAR(64),               -- nullable until provider's wallet binding is known
    provider_id         UUID,                       -- denormalised from vpn-svc for ops
    bytes_in            BIGINT NOT NULL DEFAULT 0,
    bytes_out           BIGINT NOT NULL DEFAULT 0,
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
    CONSTRAINT grid_settlement_atomic_nonneg CHECK (
        escrowed_atomic >= 0 AND consumed_atomic >= 0 AND refund_atomic >= 0
        AND provider_share >= 0 AND iogrid_share >= 0
    )
);

CREATE INDEX IF NOT EXISTS grid_settlement_provider_wallet_idx
    ON grid_settlement(provider_wallet)
    WHERE settled_at IS NULL AND provider_wallet IS NOT NULL;

CREATE INDEX IF NOT EXISTS grid_settlement_created_idx
    ON grid_settlement(created_at);

-- Faucet rate-limit table — one row per (wallet, day) of devnet faucet
-- claims, to enforce the 1/hour rule from #595 with a SELECT-MAX query.
CREATE TABLE IF NOT EXISTS grid_devnet_faucet_log (
    id              UUID PRIMARY KEY,
    wallet_address  VARCHAR(64) NOT NULL,
    minted_atomic   BIGINT NOT NULL,
    tx_signature    VARCHAR(128),
    claimed_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS grid_devnet_faucet_wallet_idx
    ON grid_devnet_faucet_log(wallet_address, claimed_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS grid_devnet_faucet_log;
DROP TABLE IF EXISTS grid_settlement;
-- +goose StatementEnd
