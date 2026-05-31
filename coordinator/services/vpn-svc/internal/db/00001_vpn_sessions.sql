-- +goose Up
-- SQL in section 'Up' is executed when this migration is applied

CREATE TABLE IF NOT EXISTS vpn_sessions (
    id UUID PRIMARY KEY,
    customer_id UUID NOT NULL,
    region VARCHAR(32) NOT NULL,
    primary_provider_id UUID NOT NULL,
    current_provider_id UUID NOT NULL,
    state VARCHAR(32) NOT NULL DEFAULT 'CREATING', -- CREATING, ESTABLISHING, ACTIVE, ROAMING, FAILING_OVER, TERMINATING

    -- Metrics
    bytes_in BIGINT DEFAULT 0,
    bytes_out BIGINT DEFAULT 0,
    roaming_events INT DEFAULT 0,
    failover_count INT DEFAULT 0,

    -- ICE details
    ice_candidate_count INT DEFAULT 0,
    ice_time_ms INT DEFAULT 0,
    wg_establish_time_ms INT DEFAULT 0,

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    terminated_at TIMESTAMPTZ,
    last_activity_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Exit reason
    exit_reason VARCHAR(128),

    CONSTRAINT fk_customer CHECK (customer_id != '00000000-0000-0000-0000-000000000000'),
    CONSTRAINT fk_primary_provider CHECK (primary_provider_id != '00000000-0000-0000-0000-000000000000'),
    CONSTRAINT fk_current_provider CHECK (current_provider_id != '00000000-0000-0000-0000-000000000000')
);

CREATE INDEX idx_vpn_sessions_customer ON vpn_sessions(customer_id);
CREATE INDEX idx_vpn_sessions_provider ON vpn_sessions(current_provider_id);
CREATE INDEX idx_vpn_sessions_region ON vpn_sessions(region);
CREATE INDEX idx_vpn_sessions_state ON vpn_sessions(state);
CREATE INDEX idx_vpn_sessions_created ON vpn_sessions(created_at DESC);

-- +goose Down
-- SQL in section 'Down' is executed when this migration is rolled back

DROP TABLE IF EXISTS vpn_sessions;
