-- +goose Up
-- +goose StatementBegin

-- notification_prefs: per-user notification-channel preferences for the
-- /account/notifications surface (Refs #631). Stored as JSONB so adding
-- a new event category (e.g. a new payout type) never needs a schema
-- migration + cross-service redeploy — the web UI and identity-svc agree
-- on the object shape, Postgres just persists it.
--
-- Shape (all keys optional; absent key = fall back to the default):
--   {
--     "earnings_credited":  { "email": true,  "in_app": true  },
--     "payout_sent":        { "email": true,  "in_app": true  },
--     "security_alerts":    { "email": true,  "in_app": true  },
--     "product_updates":    { "email": false, "in_app": true  }
--   }
--
-- NULL = the user has never customised their preferences, so the web
-- surface renders the all-on-email defaults until they save once.

ALTER TABLE users
    ADD COLUMN notification_prefs JSONB;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE users DROP COLUMN IF EXISTS notification_prefs;

-- +goose StatementEnd
