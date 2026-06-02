-- +goose Up
-- +goose StatementBegin
--
-- Sign in with Apple (#582 / EPIC #581). The mobile iOS app authenticates
-- via Apple's native sheet (expo-apple-authentication) and POSTs the
-- resulting JWT to identity-svc. identity-svc validates the JWT against
-- Apple's JWKS and looks up the user by the SHA-256 hash of
-- (apple_sub + APPLE_SUB_SALT) — that's the column added here.
--
-- Why hash + salt instead of storing the raw Apple `sub`:
--   - The Apple sub is an opaque, stable per-user-per-team identifier.
--     A DB dump containing raw subs would let an attacker cross-reference
--     other Apple-team apps (e.g. a future iogrid macOS app) that might
--     receive the same sub when bound to the same team — at which point
--     the dump becomes a join table across apps in the team.
--   - The salt is per-deployment (env APPLE_SUB_SALT) so even another
--     iogrid environment running the same Apple team can't compare hashes
--     to ours.
--   - 32-byte BYTEA = exactly one SHA-256 digest; UNIQUE index lets the
--     find-or-create at sign-in race-safely return the same user across
--     concurrent first-launch attempts.
--
-- The Identifier row (kind='apple', subject=<raw apple sub>) remains the
-- canonical join target for the account-management surface (so the user
-- can remove the binding from /account/identifiers). The hash column on
-- users acts as a fast denormalised lookup path for the sign-in hot
-- path; we DON'T want sign-in to depend on a join across identifiers
-- every launch (50ms x N users in iOS background-launch storms).

ALTER TABLE users
    ADD COLUMN apple_sub_hash BYTEA;

-- Partial unique index — users authenticated via other providers (Google,
-- magic-link, Solana) will have NULL here. Only rows that ACTUALLY have a
-- bound Apple identity participate in the uniqueness constraint, so the
-- index size stays bounded by the iOS install base, not by total users.
CREATE UNIQUE INDEX users_apple_sub_hash_uniq
    ON users (apple_sub_hash)
    WHERE apple_sub_hash IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS users_apple_sub_hash_uniq;
ALTER TABLE users DROP COLUMN IF EXISTS apple_sub_hash;
-- +goose StatementEnd
