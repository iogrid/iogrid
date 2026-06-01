-- +goose Up
-- +goose StatementBegin
-- The mobile-app roaming flow (#570 / #572) wants the provider's
-- WireGuard public key returned alongside its ICE candidate set so the
-- client can run latency probes BEFORE committing to a session. Until
-- now wg public keys were stored only per-session (via bind-provider on
-- migration 00004) — but the daemon has ONE static key per provider,
-- so we cache it on the provider row at register time.
ALTER TABLE vpn_providers
    ADD COLUMN wg_public_key VARCHAR(64);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE vpn_providers DROP COLUMN wg_public_key;
-- +goose StatementEnd
