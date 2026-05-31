-- +goose Up
-- +goose StatementBegin
ALTER TABLE vpn_sessions
    ADD COLUMN provider_wg_public_key VARCHAR(64),
    ADD COLUMN customer_wg_public_key VARCHAR(64);

-- Daemon polls assigned-but-unbound sessions every ~5s; this index keeps
-- the WHERE current_provider_id=? AND provider_wg_public_key IS NULL scan cheap.
CREATE INDEX idx_sessions_provider_unbound
    ON vpn_sessions (current_provider_id)
    WHERE terminated_at IS NULL AND (provider_wg_public_key IS NULL OR provider_wg_public_key = '');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX idx_sessions_provider_unbound;
ALTER TABLE vpn_sessions
    DROP COLUMN provider_wg_public_key,
    DROP COLUMN customer_wg_public_key;
-- +goose StatementEnd
