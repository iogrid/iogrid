-- +goose Up
-- SQL in section 'Up' is executed when this migration is applied

CREATE TABLE IF NOT EXISTS ice_candidates (
    id BIGSERIAL PRIMARY KEY,
    provider_id UUID NOT NULL,
    foundation VARCHAR(256) NOT NULL,
    component INT NOT NULL DEFAULT 1, -- RFC 8445
    transport VARCHAR(16) NOT NULL DEFAULT 'udp',
    priority INT NOT NULL,
    connection_address INET NOT NULL, -- IPv4 or IPv6
    connection_port INT NOT NULL,
    candidate_type VARCHAR(16) NOT NULL, -- host, srflx, prflx, relay
    related_address INET,
    related_port INT,
    latency_ms INT,

    -- Timestamps
    discovered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (NOW() + INTERVAL '5 minutes'),

    -- Optional linkage to session (if confirmed as working candidate)
    session_id UUID,
    is_preferred BOOLEAN DEFAULT FALSE,

    CONSTRAINT fk_provider CHECK (provider_id != '00000000-0000-0000-0000-000000000000'),
    CONSTRAINT valid_transport CHECK (transport IN ('udp')),
    CONSTRAINT valid_type CHECK (candidate_type IN ('host', 'srflx', 'prflx', 'relay')),
    CONSTRAINT valid_port CHECK (connection_port > 0 AND connection_port <= 65535)
);

CREATE INDEX idx_ice_candidates_provider ON ice_candidates(provider_id);
CREATE INDEX idx_ice_candidates_session ON ice_candidates(session_id) WHERE session_id IS NOT NULL;
CREATE INDEX idx_ice_candidates_expires ON ice_candidates(expires_at);
CREATE INDEX idx_ice_candidates_type ON ice_candidates(candidate_type);

-- +goose Down
-- SQL in section 'Down' is executed when this migration is rolled back

DROP TABLE IF EXISTS ice_candidates;
