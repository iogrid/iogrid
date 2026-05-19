-- +goose Up
-- +goose StatementBegin

-- workspaces: the multi-tenant boundary that owns every paid resource
-- (subscription, API keys, workloads, audit log). On first auth we
-- auto-mint a personal workspace with the user as OWNER (see
-- store.EnsurePersonalWorkspace). Soft-delete via deleted_at preserves
-- historical billing references after a user leaves.
CREATE TABLE workspaces (
    id                          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_user_id               UUID         NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    name                        TEXT         NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    -- plan is the WorkspacePlan enum from proto. We use TEXT (not a
    -- pg enum) so adding new tiers in proto doesn't require a schema
    -- migration; the Go layer validates against the enum constants.
    plan                        TEXT         NOT NULL DEFAULT 'FREE',
    billing_customer_id_stripe  TEXT         NOT NULL DEFAULT '',
    created_at                  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at                  TIMESTAMPTZ
);
CREATE INDEX workspaces_owner_idx
    ON workspaces (owner_user_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX workspaces_stripe_customer_uniq
    ON workspaces (billing_customer_id_stripe)
    WHERE billing_customer_id_stripe <> '';

-- workspace_members: per-user role within a workspace. UNIQUE
-- (workspace_id, user_id) prevents duplicate rows; the OWNER role is
-- enforced as exactly one per workspace via a partial unique index.
CREATE TABLE workspace_members (
    workspace_id   UUID         NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id        UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role           TEXT         NOT NULL,
    joined_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, user_id)
);
-- At most one OWNER per workspace; promotion requires an explicit
-- transfer-ownership flow that demotes the current OWNER first.
CREATE UNIQUE INDEX workspace_members_one_owner
    ON workspace_members (workspace_id) WHERE role = 'OWNER';
CREATE INDEX workspace_members_user_idx
    ON workspace_members (user_id);

-- workspace_invites: pending invitations for users who don't have a
-- iogrid account yet. Redeemed via a single-use magic-link that
-- identity-svc auto-mints; once the invitee signs in, the row turns
-- into a workspace_members entry and the invite is consumed.
CREATE TABLE workspace_invites (
    id             UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id   UUID         NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    invitee_email  CITEXT       NOT NULL,
    role           TEXT         NOT NULL,
    invited_by     UUID         NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    consumed_at    TIMESTAMPTZ
);
CREATE UNIQUE INDEX workspace_invites_pending_uniq
    ON workspace_invites (workspace_id, invitee_email)
    WHERE consumed_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS workspace_invites;
DROP TABLE IF EXISTS workspace_members;
DROP TABLE IF EXISTS workspaces;
-- +goose StatementEnd
