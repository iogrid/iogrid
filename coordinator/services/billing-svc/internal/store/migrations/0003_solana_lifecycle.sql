-- +goose Up
-- +goose StatementBegin
--
-- Extends `solana_payout` and `solana_burn` with the columns billing-svc
-- needs to drive a *real* on-chain payout lifecycle:
--
--   * `swap_signature`  — sig of the USDC→$GRID swap tx (burns + payouts)
--   * `error_message`   — human-readable failure reason (when status='FAILED')
--   * `submitted_at`    — wall-clock when `sendTransaction` returned a sig
--   * `confirmed_at`    — wall-clock when confirmation polled green
--   * `realised_out`    — actual lamports out of the swap (vs. quoted estimate)
--
-- All columns nullable so the migration is non-destructive on legacy rows.

ALTER TABLE solana_payout
    ADD COLUMN IF NOT EXISTS swap_signature TEXT NULL,
    ADD COLUMN IF NOT EXISTS error_message  TEXT NULL,
    ADD COLUMN IF NOT EXISTS submitted_at   TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS confirmed_at   TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS realised_out   BIGINT NULL;

ALTER TABLE solana_burn
    ADD COLUMN IF NOT EXISTS swap_signature TEXT NULL,
    ADD COLUMN IF NOT EXISTS error_message  TEXT NULL,
    ADD COLUMN IF NOT EXISTS submitted_at   TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS confirmed_at   TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS realised_out   BIGINT NULL;

-- Indexes for the cron worker that picks up PENDING / SUBMITTED rows.
CREATE INDEX IF NOT EXISTS solana_payout_status_idx
    ON solana_payout (status, created_at);
CREATE INDEX IF NOT EXISTS solana_burn_status_idx
    ON solana_burn (status, created_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS solana_payout_status_idx;
DROP INDEX IF EXISTS solana_burn_status_idx;

ALTER TABLE solana_payout
    DROP COLUMN IF EXISTS swap_signature,
    DROP COLUMN IF EXISTS error_message,
    DROP COLUMN IF EXISTS submitted_at,
    DROP COLUMN IF EXISTS confirmed_at,
    DROP COLUMN IF EXISTS realised_out;

ALTER TABLE solana_burn
    DROP COLUMN IF EXISTS swap_signature,
    DROP COLUMN IF EXISTS error_message,
    DROP COLUMN IF EXISTS submitted_at,
    DROP COLUMN IF EXISTS confirmed_at,
    DROP COLUMN IF EXISTS realised_out;
-- +goose StatementEnd
