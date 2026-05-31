-- +goose Up
-- +goose StatementBegin
CREATE TABLE vpn_providers (
    id UUID PRIMARY KEY,
    region VARCHAR(50) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'healthy', -- healthy, degraded, offline
    session_count INT DEFAULT 0,
    last_seen_at TIMESTAMP NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_providers_region_status ON vpn_providers (region, status);
CREATE INDEX idx_providers_session_count ON vpn_providers (session_count);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE vpn_providers;
-- +goose StatementEnd
