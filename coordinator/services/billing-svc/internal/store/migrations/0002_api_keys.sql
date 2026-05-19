-- +goose Up
-- +goose StatementBegin
--
-- Customer API key table.
--
-- API keys are the credential customers present to the proxy-gateway
-- (SOCKS5 RFC 1929 password / HTTP CONNECT Proxy-Authorization) and to
-- the build-gateway (Authorization: Bearer). billing-svc is the source
-- of truth.
--
-- We store the SHA-256 hex of the plaintext key as `key_hash` so the
-- plaintext is unrecoverable from a database dump. ValidateApiKey
-- recomputes the hash on every call.

CREATE TABLE IF NOT EXISTS api_key (
    id              UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID          NOT NULL,
    label           TEXT          NOT NULL,
    -- SHA-256 hex of the plaintext token. UNIQUE so ValidateApiKey is O(1).
    key_hash        TEXT          NOT NULL UNIQUE,
    last_four       TEXT          NOT NULL,
    tier            TEXT          NOT NULL,
    -- Comma-separated list of allowed workload categories (e.g.
    -- 'scrape,bandwidth,build'). Empty = inherit workspace default.
    allowed_categories  TEXT      NOT NULL DEFAULT '',
    geo_target      TEXT          NOT NULL DEFAULT '',
    kyc_verified    BOOLEAN       NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    last_used_at    TIMESTAMPTZ   NULL,
    revoked_at      TIMESTAMPTZ   NULL
);
CREATE INDEX IF NOT EXISTS api_key_workspace_idx
    ON api_key (workspace_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS api_key;
-- +goose StatementEnd
