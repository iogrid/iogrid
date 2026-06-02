-- +goose Up
-- +goose StatementBegin
--
-- 0005 — wallet_binding (#583 / #584 / Track 2 of EPIC #581)
--
-- Records a customer's chosen wallet for $GRID payments. Two providers
-- are supported v1:
--   - 'phantom'  → Phantom Wallet on Solana (deeplink + NaCl box)
--   - 'ping'     → openova-group's ping cash app (deeplink, Solana-backed)
--
-- Both wallets store $GRID (an SPL token on Solana mainnet/devnet) so the
-- address column itself is the SAME shape (base58 ed25519 pubkey) — the
-- provider discriminator only records WHICH client app the user paired
-- through, so the mobile app can deeplink back to the right one for
-- top-up / signing.
--
-- A separate `customer_wallet_bindings` table (rather than columns on
-- `users`) keeps the wallet primitive decoupled from the existing
-- identifier rows (the SIWS flow under internal/siws/ writes to the
-- `identifiers` table with kind=solana for provider sign-in; the binding
-- table here is the consumer-side surface for the mobile VPN app).
--
-- The user_id FK references users.id. A user is allowed at most ONE
-- bound wallet — switching wallets means deleting the row and binding a
-- fresh one (matches the wireframe Settings → Wallet "Switch" UX).

CREATE TYPE wallet_provider AS ENUM ('phantom', 'ping');

CREATE TABLE customer_wallet_bindings (
    user_id          UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    wallet_address   TEXT NOT NULL,
    wallet_provider  wallet_provider NOT NULL,
    bound_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_balance_at  TIMESTAMPTZ,
    last_balance_lamports BIGINT
);

-- One wallet address is bindable by at most one user (mirrors the
-- identifiers (kind, subject) uniqueness — a single Solana address is a
-- single identity globally).
CREATE UNIQUE INDEX customer_wallet_bindings_address_uk
    ON customer_wallet_bindings (wallet_address);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS customer_wallet_bindings_address_uk;
DROP TABLE IF EXISTS customer_wallet_bindings;
DROP TYPE IF EXISTS wallet_provider;

-- +goose StatementEnd
