-- +goose Up
-- +goose StatementBegin
ALTER TABLE vpn_sessions
    ADD COLUMN billed_at TIMESTAMPTZ;

-- The earnings batcher selects terminated-but-unbilled sessions every
-- ~5 min. Partial index keeps the scan O(unbilled) rather than O(all).
CREATE INDEX idx_sessions_unbilled
    ON vpn_sessions (terminated_at)
    WHERE terminated_at IS NOT NULL AND billed_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX idx_sessions_unbilled;
ALTER TABLE vpn_sessions DROP COLUMN billed_at;
-- +goose StatementEnd
