-- +goose Up
-- +goose StatementBegin
--
-- payout_methods: per-user payout election surface for /provide/earnings.
--
-- Owned by billing-svc because the canonical payout-method state (cash
-- off-ramp / charity forward / VPN burn) is part of the money bounded
-- context. user_id is opaque from identity-svc; we treat it as the
-- primary key (one election per user — switching variants overwrites).
CREATE TABLE IF NOT EXISTS payout_methods (
    user_id              UUID         PRIMARY KEY,
    -- 'UNSPECIFIED' | 'CASH_USDC' | 'FREE_VPN' | 'CHARITY' — matches
    -- the proto enum tail. New enum members must be appended here too.
    kind                 TEXT         NOT NULL DEFAULT 'UNSPECIFIED',
    -- Set only when kind = 'CASH_USDC'.
    destination_address  TEXT         NOT NULL DEFAULT '',
    -- Set only when kind = 'CHARITY'.
    charity_id           TEXT         NOT NULL DEFAULT '',
    updated_at           TIMESTAMPTZ  NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS payout_methods;
-- +goose StatementEnd
