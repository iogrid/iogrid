-- +goose Up
-- +goose StatementBegin
--
-- Add 'solana' to identifier_kind to back the Sign-In-With-Solana flow
-- (see internal/siws/). Providers must bind one or more Solana wallets
-- to receive native $GRID payouts per docs/TOKENOMICS.md.
--
-- Postgres requires ALTER TYPE ... ADD VALUE to run OUTSIDE a
-- transaction block. We declare NO TRANSACTION so goose runs the
-- statement directly. The companion partial UNIQUE index reuses the
-- existing identifiers.kind+subject uniqueness path (which already
-- gates Google subject collisions); for SOLANA rows the subject is the
-- base58 pubkey, so the existing index already enforces "one wallet per
-- kind globally".
-- +goose StatementEnd

-- +goose NO TRANSACTION
-- +goose StatementBegin
ALTER TYPE identifier_kind ADD VALUE IF NOT EXISTS 'solana';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- ALTER TYPE ... DROP VALUE is not supported by Postgres, so the Down
-- migration leaves the enum value in place. Code paths that surface
-- KindSolana are removed by reverting the application code, not by
-- mutating the schema.
SELECT 1;
-- +goose StatementEnd
