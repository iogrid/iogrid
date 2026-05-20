-- Per-owner primary provider selection (#325, family of #305).
--
-- Background
--   gateway-bff /provide/* resolves "which provider is THIS user's"
--   by listing the caller's owned providers and picking the first
--   ACTIVE one. With one daemon per user that's stable; with two or
--   more it became position-of-the-day: ListProviders ordered by id
--   ASC, so the daemon that happened to receive the smaller UUID
--   answered for the user. Hatice (manual-test + her real Mac) was
--   the operator who hit it on the EPIC #309 walk — the schedule UI
--   showed the wrong daemon's caps.
--
-- Fix
--   Introduce an explicit per-owner primary flag with a partial
--   unique index that enforces "at most one primary per owner_user_id".
--   The deterministic selector lives in providers-svc store + bff:
--     ORDER BY is_primary DESC, last_seen_at DESC NULLS LAST, registered_at DESC
--   so a freshly-paired second daemon never silently steals the
--   primary slot, and operators can re-assign at any time via the
--   SetPrimaryProvider RPC.
--
-- Backfill rule
--   For owners that already have rows: the row with the most recent
--   registered_at wins. Deterministic and matches the "newest paired
--   wins ties" intuition. Owners with zero rows are unaffected; their
--   next PairDaemon insert will mark itself primary.

-- +goose Up
-- +goose StatementBegin

ALTER TABLE providers
    ADD COLUMN is_primary BOOLEAN NOT NULL DEFAULT FALSE;

-- Partial unique index: enforces at-most-one primary per owner.
-- A partial index over WHERE is_primary skips non-primary rows entirely
-- so the constraint never collides on the (large, expected) set of
-- secondary daemons.
CREATE UNIQUE INDEX providers_primary_per_owner
    ON providers (owner_user_id)
    WHERE is_primary;

-- Backfill: one primary per owner = the row with the most recent
-- registered_at. Done in a single statement so it's atomic.
UPDATE providers
   SET is_primary = TRUE
  FROM (
        SELECT DISTINCT ON (owner_user_id) id
          FROM providers
         ORDER BY owner_user_id, registered_at DESC
       ) AS picks
 WHERE providers.id = picks.id;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS providers_primary_per_owner;
ALTER TABLE providers DROP COLUMN IF EXISTS is_primary;

-- +goose StatementEnd
