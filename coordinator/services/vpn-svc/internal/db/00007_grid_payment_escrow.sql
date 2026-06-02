-- +goose Up
-- +goose StatementBegin
-- $GRID payment escrow + nonce-replay protection (Track 5, #596).
--
-- vpn-svc accepts a signed payment authorization from the customer's
-- Solana wallet, holds an escrow row keyed by session_id, and decrements
-- it on each /heartbeat call. billing-svc reads `vpn_session_escrow` (via
-- the session-end webhook from #597) to compute provider + iogrid shares
-- and queue the on-chain settlement (#598).

CREATE TABLE IF NOT EXISTS vpn_session_escrow (
    session_id               UUID PRIMARY KEY REFERENCES vpn_sessions(id) ON DELETE CASCADE,
    customer_id              UUID NOT NULL,
    wallet_address           VARCHAR(64) NOT NULL,        -- base58 Solana pubkey, max 44 chars
    escrowed_grid_atomic     BIGINT NOT NULL,             -- atomic units (9 decimals)
    consumed_grid_atomic     BIGINT NOT NULL DEFAULT 0,
    max_grid_per_min_atomic  BIGINT NOT NULL,             -- client-enforced rate cap (anti-runaway)
    nonce                    VARCHAR(128) NOT NULL,
    started_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_heartbeat_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    settled_at               TIMESTAMPTZ,
    CONSTRAINT vpn_session_escrow_grid_nonneg CHECK (escrowed_grid_atomic >= 0),
    CONSTRAINT vpn_session_escrow_consumed_nonneg CHECK (consumed_grid_atomic >= 0)
);

CREATE INDEX IF NOT EXISTS vpn_session_escrow_customer_idx
    ON vpn_session_escrow(customer_id);
CREATE INDEX IF NOT EXISTS vpn_session_escrow_wallet_idx
    ON vpn_session_escrow(wallet_address);

-- Nonce-replay table. Caller supplies a per-request nonce inside the signed
-- message; we store (wallet_address, nonce) for 60s and reject duplicates.
-- A cron-style cleanup is added inline in the Go side via a periodic DELETE
-- (kept here as the storage layer).
CREATE TABLE IF NOT EXISTS vpn_payment_nonces (
    wallet_address  VARCHAR(64) NOT NULL,
    nonce           VARCHAR(128) NOT NULL,
    seen_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (wallet_address, nonce)
);

CREATE INDEX IF NOT EXISTS vpn_payment_nonces_seen_at_idx
    ON vpn_payment_nonces(seen_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS vpn_payment_nonces;
DROP TABLE IF EXISTS vpn_session_escrow;
-- +goose StatementEnd
