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

## Environment

```bash
LISTEN_ADDR=:8080                          # default
BASE_URL=https://api.iogrid.org/identity   # public base for magic links
DATABASE_URL=postgres://...                # required
REDIS_URL=redis://localhost:6379/0         # optional (in-memory fallback)

GOOGLE_CLIENT_ID=...                       # required for Google flow
GOOGLE_CLIENT_SECRET=...                   # required for Google flow
GOOGLE_REDIRECT_URL=https://.../v1/auth/google/callback

JWT_PRIVATE_KEY_PATH=/etc/iogrid/jwt/private.pem
JWT_PUBLIC_KEY_PATH=/etc/iogrid/jwt/public.pem
JWT_KEY_ID=primary                         # for kid rotation
JWT_ISSUER=https://api.iogrid.org/identity
JWT_AUDIENCE=gateway-bff,proxy-gateway     # comma-separated

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

```bash
# 1. Bring up Postgres + Redis (compose file in coordinator/ deploys)
docker compose -f ../../deploys/dev.yml up -d postgres redis

# 2. Generate a test RSA keypair (NEVER use in prod):
mkdir -p /tmp/iogrid-jwt
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 \
  -out /tmp/iogrid-jwt/private.pem
openssl rsa -pubout -in /tmp/iogrid-jwt/private.pem \
  -out /tmp/iogrid-jwt/public.pem

# 3. Run
export DATABASE_URL=postgres://postgres:secret@localhost:5432/identity?sslmode=disable
export GOOGLE_CLIENT_ID=...
export GOOGLE_CLIENT_SECRET=...
export JWT_PRIVATE_KEY_PATH=/tmp/iogrid-jwt/private.pem
export JWT_PUBLIC_KEY_PATH=/tmp/iogrid-jwt/public.pem
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
