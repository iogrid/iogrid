-- +goose Up
-- +goose StatementBegin
-- Track 3 (#588) — extend vpn_sessions with the mobile-PacketTunnelProvider
-- session-config fields. The mobile client (PacketTunnelProvider running
-- inside a NetworkExtension process) needs the full WG peer config + an
-- allocated inner IPv4 + a session TTL returned from POST /v1/vpn/sessions.
--
-- Inner IPv4 allocation (#588 DoD): each peer owns a /24 inside 10.66.0.0/16
-- and we allocate .2 + increment per session. The full /32 is stored here
-- so a heartbeat / heartbeat-followup can re-emit the same value without
-- recomputing the allocation; the constraint UNIQUE (current_provider_id,
-- inner_ip) catches double-allocations at the DB layer (race condition
-- safety net — the picker takes a row-level lock to serialise normally).
--
-- client_public_key is the customer's WG public key (base64). Track 4 / 5
-- will source this from the secure-enclave-derived account identity; for
-- now the mobile app generates it at startup + sends it in the session
-- request.
--
-- expires_at is the wall-clock TTL on the session. Mobile picks a
-- 24-hour default; vpn-svc enforces by terminating expired sessions in
-- the periodic cleanup tick.
--
-- payment_authorization is the Track 5 ($GRID) hook — we accept the JSON
-- payload here for forward-compat but don't validate yet (per task DoD:
-- "Accept (don't yet validate) payment_authorization field — Track 5
-- #596 will validate"). JSONB shape is opaque to vpn-svc; #596 lands a
-- ValidatePayment(ctx, payload) interface that we'll plug into the
-- handler at validation time.
ALTER TABLE vpn_sessions
    ADD COLUMN client_public_key       VARCHAR(64),
    ADD COLUMN inner_ip                INET,
    ADD COLUMN expires_at              TIMESTAMPTZ,
    ADD COLUMN payment_authorization   JSONB;

CREATE UNIQUE INDEX idx_vpn_sessions_provider_inner_ip
    ON vpn_sessions(current_provider_id, inner_ip)
    WHERE inner_ip IS NOT NULL AND terminated_at IS NULL;

-- Track the highest inner-IP suffix allocated per provider so we can
-- O(1) incrementally allocate without a SELECT MAX scan. Bootstrap rows
-- are created on first session for each provider.
CREATE TABLE IF NOT EXISTS vpn_provider_inner_ip_alloc (
    provider_id  UUID PRIMARY KEY,
    -- Last allocated suffix in the 10.66.X.Y space. We bootstrap at 1
    -- (so the first allocation produces .2 = next_suffix + 1).
    next_suffix  INT  NOT NULL DEFAULT 1,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS vpn_provider_inner_ip_alloc;
DROP INDEX IF EXISTS idx_vpn_sessions_provider_inner_ip;
ALTER TABLE vpn_sessions
    DROP COLUMN IF EXISTS payment_authorization,
    DROP COLUMN IF EXISTS expires_at,
    DROP COLUMN IF EXISTS inner_ip,
    DROP COLUMN IF EXISTS client_public_key;
-- +goose StatementEnd
