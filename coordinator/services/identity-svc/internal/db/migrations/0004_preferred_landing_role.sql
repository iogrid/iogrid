-- +goose Up
-- +goose StatementBegin

-- preferred_landing_role: the consumer-app persona the user picked on
-- first sign-in via the /welcome picker (EPIC #422 / PR #445).
--
-- NULL = never picked yet → web's auth middleware redirects to
--        /welcome on the next sign-in.
-- 'provider' / 'customer' / 'vpn' = direct redirect to that virtual app.
--
-- The /account persona is shared identity surface, so it is not a valid
-- preferred_landing_role value — users can always reach /account from
-- the persona rail regardless of which app they landed in.
--
-- Refs #422 (UX revamp), PR #445 (left-rail + welcome picker).

CREATE TYPE preferred_landing_role AS ENUM ('provider', 'customer', 'vpn');

ALTER TABLE users
    ADD COLUMN preferred_landing_role preferred_landing_role;

-- Partial index — only rows that have made a choice. NULL is the
-- expected initial state for every existing user post-migration; the
-- /welcome picker writes the chosen role on first interaction.
CREATE INDEX users_preferred_landing_role_idx
    ON users (preferred_landing_role)
    WHERE preferred_landing_role IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS users_preferred_landing_role_idx;
ALTER TABLE users DROP COLUMN IF EXISTS preferred_landing_role;
DROP TYPE IF EXISTS preferred_landing_role;

-- +goose StatementEnd
