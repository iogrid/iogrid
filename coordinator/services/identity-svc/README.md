# identity-svc

The iogrid coordinator microservice that owns user accounts, sign-in
flows, and JWT issuance for the entire control plane.

Two sign-in paths only — passwords are never stored:

1. **Google OAuth** (authorization-code + PKCE). We pull `sub`, `email`,
   `email_verified`, `hd`, plus the full verified-emails list via the
   People API. Verified secondaries enable auto-merge.
2. **Magic link** via Stalwart SMTP at `mail.openova.io`. One-shot,
   10-minute, SHA-256-hashed at rest. Rate-limited per-email + per-IP.

When Google's verified secondaries match an existing magic-link
identifier we **auto-merge silently** and audit the merge. See
`docs/TECH.md` for the model.

Tokens:
- **Access token** — RS256-signed, 15min, includes `sub` + `identifiers`
  + `primary_email` + `roles` + `step_up` claim
- **Refresh token** — opaque 32-byte random; SHA-256 hashed in DB;
  rotated on every refresh

## Endpoints (HTTP/JSON, Connect-Go bridge upcoming)

| Method | Path                                  | Auth        | Purpose                                  |
|--------|---------------------------------------|-------------|------------------------------------------|
| POST   | `/v1/auth/google/start`               | none        | Returns Google authorize URL + state     |
| POST   | `/v1/auth/google/complete`            | none        | Exchanges code → AuthBundle              |
| GET    | `/v1/auth/google/callback`            | none        | Browser-redirect form of Complete        |
| POST   | `/v1/auth/magic-link/request`         | none        | Sends an emailed link                    |
| POST   | `/v1/auth/magic-link/complete`        | none        | Redeems a link token → AuthBundle        |
| GET    | `/v1/auth/magic-link/complete`        | none        | Browser-click form (token in query)      |
| POST   | `/v1/auth/refresh`                    | none        | Rotates the refresh token                |
| POST   | `/v1/auth/sign-out`                   | none        | Revokes the session                      |
| POST   | `/v1/auth/step-up/request`            | bearer      | Sends step-up magic-link to primary email |
| POST   | `/v1/auth/step-up/complete`           | none        | Redeems a step-up link                   |
| GET    | `/v1/sessions/`                       | bearer      | Lists live sessions for caller           |
| DELETE | `/v1/sessions/{id}`                   | bearer      | Revokes one session                      |
| GET    | `/v1/users/{id}`                      | bearer      | Returns user + identifiers               |
| PATCH  | `/v1/users/{id}`                      | bearer      | Updates the caller's own profile         |
| GET    | `/v1/workspaces/`                     | bearer      | Lists workspaces the caller belongs to   |
| POST   | `/v1/workspaces/`                     | bearer      | Creates a workspace owned by caller      |
| GET    | `/v1/workspaces/{id}`                 | bearer      | Returns one workspace + caller role      |
| PATCH  | `/v1/workspaces/{id}`                 | bearer      | Renames or re-plans (OWNER/ADMIN)        |
| DELETE | `/v1/workspaces/{id}`                 | bearer+step-up | Soft-deletes (OWNER only)             |
| GET    | `/v1/workspaces/{id}/members`         | bearer      | Lists members of the workspace           |
| POST   | `/v1/workspaces/{id}/members`         | bearer      | Adds a member (or pending invite)        |
| PATCH  | `/v1/workspaces/{id}/members/{userID}`| bearer      | Changes a member's role (OWNER/ADMIN)    |
| DELETE | `/v1/workspaces/{id}/members/{userID}`| bearer      | Removes a member                         |

Plus the Connect-RPC handler at `/iogrid.identity.v1.WorkspaceService/*`
for service-to-service calls (gateway-bff, billing-svc).

## Workspace bounded-context

Every paid resource in iogrid (subscription, API keys, workloads, audit
log) is owned by a **Workspace**, not by a User directly. The Workspace
join lets one human belong to many tenants (think Slack workspaces) and
matches Stripe's "customer" granularity.

On first authentication identity-svc auto-mints a **personal workspace**
for the user with the user as `OWNER`. The user can rename it, invite
collaborators, upgrade the plan, or create additional workspaces from
the management plane.

### Schema

* `workspaces` — `owner_user_id` (FK), `name`, `plan`, `billing_customer_id_stripe`
* `workspace_members` — `(workspace_id, user_id)` PK, `role`
* `workspace_invites` — pending invites for not-yet-signed-up emails

The `plan` and `role` columns are TEXT (not pg enums) so adding a new
tier in proto doesn't require a schema migration.

### Roles

Ordered most → least privileged: `OWNER > ADMIN > BILLING_ONLY ≈ READ_ONLY`.

* `OWNER`        — every operation incl. plan change + workspace delete
* `ADMIN`        — add/remove members, rename, change plan
* `BILLING_ONLY` — view + change payment methods; no workload visibility
* `READ_ONLY`    — view-only

A workspace MUST always have exactly one OWNER. The store layer blocks
the last-owner removal/demotion; promotion to OWNER goes through a
separate transfer-ownership flow (out of scope for issue #146).

### Pending invites

`AddMember` with an unknown email creates a row in `workspace_invites`
instead of `workspace_members`. The invite is consumed automatically on
the invitee's first sign-in via `ConsumeInvitesForEmail`, which the
auth flow calls inside the same transaction that mints the new User.
This is how a workspace OWNER can pre-provision a team before the team
has accounts.

## Environment

```bash
LISTEN_ADDR=:8080                          # default
BASE_URL=https://api.iogrid.org/identity   # public base for magic links
DATABASE_URL=postgres://...                # required
REDIS_URL=redis://localhost:6379/0         # optional (in-memory fallback)

GOOGLE_CLIENT_ID=...                       # required for Google flow
GOOGLE_CLIENT_SECRET=...                   # required for Google flow
GOOGLE_REDIRECT_URL=https://.../v1/auth/google/callback

JWT_PRIVATE_KEY_PATH=/etc/identity/jwt/private.pem   # prod: SealedSecret mount
JWT_PUBLIC_KEY_PATH=/etc/identity/jwt/public.pem
JWT_KEY_ID=primary                         # for kid rotation
JWT_ISSUER=https://api.iogrid.org/identity
JWT_AUDIENCE=gateway-bff,proxy-gateway     # comma-separated

# Dev / e2e only — autogen ephemeral keypair at boot (NEVER in prod).
JWT_KEYPAIR_AUTOGEN=false                  # set to true to enable
JWT_AUTOGEN_DIR=/tmp/jwt-keys              # writeable mount (emptyDir in k8s)

ACCESS_TOKEN_TTL=15m
REFRESH_TOKEN_TTL=720h                     # 30 days
STEP_UP_TTL=5m
MAGIC_LINK_TTL=10m
MAGIC_LINK_PER_EMAIL_PER_HOUR=3
MAGIC_LINK_PER_IP_PER_HOUR=10

SMTP_HOST=mail.openova.io
SMTP_PORT=587
SMTP_FROM=no-reply@iogrid.org
SMTP_FROM_NAME=iogrid
SMTP_STARTTLS=true
SMTP_USERNAME=                             # blank for internal Stalwart route
SMTP_PASSWORD=

ALLOWED_RETURN_HOSTS=iogrid.org,app.iogrid.org   # open-redirect allowlist
```

## Local development

Three ways to satisfy the RS256 JWT keypair requirement — pick whichever
matches your environment.

### Option 1 — committed dev fixture (preferred for unit / integration tests)

A static RSA-2048 keypair lives at
`internal/auth/fixtures/{jwt_test.key,jwt_test.pub}`. **Public, committed
to git, NEVER for prod.** See [`internal/auth/fixtures/README.md`](internal/auth/fixtures/README.md).

```bash
export JWT_PRIVATE_KEY_PATH=$(pwd)/internal/auth/fixtures/jwt_test.key
export JWT_PUBLIC_KEY_PATH=$(pwd)/internal/auth/fixtures/jwt_test.pub
go run ./cmd/identity-svc
```

### Option 2 — autogen ephemeral keypair (k8s e2e + transient dev)

Set `JWT_KEYPAIR_AUTOGEN=1` and the binary mints a fresh RSA-2048
keypair at boot, writes both PEM files under `$JWT_AUTOGEN_DIR` (default
`/tmp/jwt-keys`), logs a loud warning, and uses them for the lifetime of
the process. Tokens are **invalidated on pod restart**; downstream
verifiers that cached the previous public key will reject them.

```bash
export JWT_KEYPAIR_AUTOGEN=1
# (optional) export JWT_AUTOGEN_DIR=/tmp/jwt-keys
go run ./cmd/identity-svc
```

**`readOnlyRootFilesystem: true` trap.** The prod manifest at
`infra/k8s/base/identity-svc/deployment.yaml` ships
`readOnlyRootFilesystem: true`, so the autogen path cannot write to `/`,
`/etc`, or any other rootfs directory. The deployment defines an
`emptyDir { medium: Memory }` mount at `/tmp/jwt-keys` precisely so
autogen works under that security context. **If you swap mount paths,
keep the writeable emptyDir aligned with `JWT_AUTOGEN_DIR`.**

### Option 3 — production SealedSecret mount

In prod (`infra/k8s/base/identity-svc/deployment.yaml`) a SealedSecret
named `identity-svc-jwt` is mounted read-only at `/etc/identity/jwt`:

```yaml
volumes:
  - name: jwt
    secret:
      secretName: identity-svc-jwt
      optional: true        # `optional: true` lets dev pods boot without it,
                            # combined with JWT_KEYPAIR_AUTOGEN=1
```

The Secret carries two keys: `private.pem` + `public.pem` (PEM-encoded
RS256). Rotation playbook lives in `docs/RUNBOOKS.md` (see "JWT
key rotation"). For local docker-compose dev, manually generate the
keypair:

```bash
mkdir -p /tmp/iogrid-jwt
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 \
  -out /tmp/iogrid-jwt/private.pem
openssl rsa -pubout -in /tmp/iogrid-jwt/private.pem \
  -out /tmp/iogrid-jwt/public.pem
export JWT_PRIVATE_KEY_PATH=/tmp/iogrid-jwt/private.pem
export JWT_PUBLIC_KEY_PATH=/tmp/iogrid-jwt/public.pem
```

### Bringing up Postgres + Redis

```bash
docker compose -f ../../deploys/dev.yml up -d postgres redis
export DATABASE_URL=postgres://postgres:secret@localhost:5432/identity?sslmode=disable
export GOOGLE_CLIENT_ID=...
export GOOGLE_CLIENT_SECRET=...
go run ./cmd/identity-svc
```

Magic-link emails land in your Stalwart inbox. For pure-local dev,
substitute `mailpit` (`docker run -p 1025:1025 -p 8025:8025 axllent/mailpit`)
and set `SMTP_HOST=localhost SMTP_PORT=1025 SMTP_STARTTLS=false`.

## Testing

```bash
# Unit tests (no external deps)
go test ./...

# Integration tests (spins up Postgres via dockertest)
go test -tags=integration ./...
```

The default CI workflow `.github/workflows/coordinator-ci.yml` runs only
the unit suite. Integration tests run in the separate
`identity-svc-integration.yml` workflow with a Postgres service container.

## Auto-merge model

```
on Google sign-in (sub=X, email=Y, verified-secondaries=[Z1, Z2, ...]):
  if identifier exists with (kind=google, subject=X):
    use that user
  else:
    for each secondary in [Z1, Z2, ...]:
      if identifier exists with (kind=magic_link, email=secondary, verified=true):
        attach (google, X, Y) to that user
        log merge_audit
        notify both primary + secondary email
        return that user
    create fresh user with (google, X, Y)

on magic-link redemption (email=W):
  if identifier exists with (kind=magic_link, email=W):
    use that user
  else if identifier exists with (kind=google, email=W, verified=true):
    -- this row is created if W was previously a Google-verified secondary
    attach (magic_link, W) to that Google user
    log merge_audit
    return that user
  else:
    create fresh user with (magic_link, W)
```

Audit row format: `merge_audit (primary_user_id, merged_user_id, reason,
matched_email, matched_via, merged_at)`.

## Schema

```
users(id, primary_email, display_name, picture_url, roles[], …, deleted_at)
identifiers(id, user_id, kind, subject, email, verified, hosted_domain, …)
sessions(id, user_id, refresh_token_hash, ip, user_agent, expires_at, revoked_at, step_up_until)
magic_link_tokens(token_hash PK, email, intent, user_id?, return_to, expires_at, used_at)
merge_audit(id, primary_user_id, merged_user_id?, reason, matched_email, matched_via, merged_at)
```

See `internal/db/migrations/0001_init.sql` for the canonical DDL.

## Connect-Go bridge (planned)

A future PR will wire `coordinator/internal/pb/iogrid/identity/v1/identityv1connect/`
(generated from `proto/iogrid/identity/v1/{auth,identity}.proto` once the proto
contracts merge to main) onto the same `auth.Service` methods used by these
HTTP handlers. The HTTP/JSON surface stays for browser-friendly fallback.
