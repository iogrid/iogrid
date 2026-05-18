-- +goose Up
-- +goose StatementBegin

-- pgcrypto for gen_random_uuid(); CITEXT for case-insensitive email matching.
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;

-- users: the canonical, immutable account record. Identifiers come and go;
-- a User exists for the life of the account. Soft-delete via deleted_at.
CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    primary_email CITEXT NOT NULL,
    display_name  TEXT   NOT NULL DEFAULT '',
    picture_url   TEXT   NOT NULL DEFAULT '',
    roles         TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at TIMESTAMPTZ,
    deleted_at    TIMESTAMPTZ
);
CREATE INDEX users_primary_email_idx ON users (primary_email) WHERE deleted_at IS NULL;

-- identifiers: each row is one way the user can sign in. A user can have
-- many; auto-merge on Google verified-secondary-email match (see services
-- /auth/auto_merge.go). UNIQUE(kind, subject) enforces "one Google account
-- per identity row"; partial UNIQUE(kind, email) prevents two different
-- users from both binding the same email under the same kind.
CREATE TYPE identifier_kind AS ENUM ('google', 'magic_link', 'apple', 'github');

CREATE TABLE identifiers (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind          identifier_kind NOT NULL,
    subject       TEXT NOT NULL DEFAULT '',
    email         CITEXT,
    verified      BOOLEAN NOT NULL DEFAULT FALSE,
    hosted_domain TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Two Google accounts cannot share the same sub. Magic-link rows have
-- subject='' so we only enforce subject-uniqueness for OAuth kinds.
CREATE UNIQUE INDEX identifiers_kind_subject_uniq
    ON identifiers (kind, subject) WHERE subject <> '';
CREATE UNIQUE INDEX identifiers_kind_email_uniq
    ON identifiers (kind, email) WHERE email IS NOT NULL;
CREATE INDEX identifiers_user_id_idx ON identifiers (user_id);

-- sessions: server-side refresh-token records. The raw token is hashed
-- (SHA-256) so a DB dump cannot replay anyone's session. revoked_at +
-- expires_at gate liveness; ip / user_agent enable user-visible
-- "where am I signed in" listings.
CREATE TABLE sessions (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    refresh_token_hash  TEXT NOT NULL,
    ip                  INET,
    user_agent          TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at          TIMESTAMPTZ NOT NULL,
    revoked_at          TIMESTAMPTZ,
    -- step_up_until carries forward the "fresh auth in last 5 minutes"
    -- state required for payout / merge / delete operations.
    step_up_until       TIMESTAMPTZ
);
CREATE INDEX sessions_user_id_idx ON sessions (user_id) WHERE revoked_at IS NULL;
CREATE UNIQUE INDEX sessions_refresh_token_hash_uniq
    ON sessions (refresh_token_hash);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at)
    WHERE revoked_at IS NULL;

-- magic_link_tokens: one row per emailed link. We store the SHA-256 hash
-- of the raw token so a DB compromise cannot replay outstanding links.
-- intent gates which kind of session the link mints when redeemed.
CREATE TYPE magic_link_intent AS ENUM ('signin', 'step_up', 'merge');

CREATE TABLE magic_link_tokens (
    token_hash   TEXT PRIMARY KEY,
    email        CITEXT NOT NULL,
    intent       magic_link_intent NOT NULL,
    user_id      UUID REFERENCES users(id) ON DELETE CASCADE,
    return_to    TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ NOT NULL,
    used_at      TIMESTAMPTZ
);
CREATE INDEX magic_link_tokens_email_idx ON magic_link_tokens (email);
CREATE INDEX magic_link_tokens_expires_at_idx ON magic_link_tokens (expires_at)
    WHERE used_at IS NULL;

-- merge_audit: append-only log of every auto-merge. Both directions
-- (Google→magic-link and magic-link→Google) record the SURVIVING user
-- (primary_user_id) and the DELETED stub (merged_user_id, NULL when the
-- merge attached an identifier to a still-fresh primary without ever
-- materialising a stub User).
CREATE TABLE merge_audit (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    primary_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    merged_user_id  UUID,
    reason          TEXT NOT NULL,
    matched_email   CITEXT NOT NULL,
    matched_via     TEXT NOT NULL,
    merged_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX merge_audit_primary_user_id_idx ON merge_audit (primary_user_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS merge_audit;
DROP TABLE IF EXISTS magic_link_tokens;
DROP TYPE  IF EXISTS magic_link_intent;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS identifiers;
DROP TYPE  IF EXISTS identifier_kind;
DROP TABLE IF EXISTS users;
-- +goose StatementEnd
