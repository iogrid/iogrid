# `admin/` — iogrid admin console

Independent Next.js 15 App Router for `admin.iogrid.org` (EPIC #422 Phase 1).

## What this is

Admin-only staff console. Surfaces:

- `/providers` — paired daemon roster + per-provider transparency audit.
- `/abuse` — antiabuse-svc filter ruleset and (soon) the pending review queue.
- `/billing` — KYC review, sanctions screening, payout audit (stub in Phase 1).
- `/health` — control-plane health and SLOs (stub in Phase 1).

This codebase is **deliberately separate** from `web/`. Strict-separation invariant: never renders `/provide` / `/customer` / `/vpn` — those live in the user-facing `web/` app on `iogrid.org` (Phase 3 will move user-facing from `iogrid.org` → `iogrid.org` apex).

Founder directive (verbatim, 2026-05-21):

> admin must be an idependent one admin.iogriod.org
> And admis app and user apps cannot be mixed to each other or instnace what is the point of showing the provide option to admin, he needs to access from teh eother indepent apps

## Isolation properties

| Property | admin/ | web/ |
|---|---|---|
| Host | `admin.iogrid.org` | `iogrid.org` → `iogrid.org` (Phase 3) |
| Image | `ghcr.io/iogrid/admin` | `ghcr.io/iogrid/web` |
| CI workflow | `.github/workflows/admin-ci.yml` | `.github/workflows/web-ci.yml` |
| K8s base | `infra/k8s/base/admin/` | `infra/k8s/base/web/` |
| NextAuth cookie domain | `admin.iogrid.org` (host-scoped) | `iogrid.org` (host-scoped) |
| Cookie name | `__Secure-iogrid-admin.session-token` | `next-auth.session-token` |

The host-scoped cookie domain means a session minted on `admin.iogrid.org` is **never** sent to `iogrid.org` / `iogrid.org` and vice-versa. Admins who also act as providers use TWO different cookies, TWO different sessions across the two hosts.

## Development

```bash
# from repo root:
pnpm install
cd admin
pnpm dev            # http://localhost:3001 (avoids web's 3000)
pnpm typecheck
pnpm build
```

The admin app uses port `3001` in dev (web/ uses `3000`) so both can run side-by-side.

## Environment

| Var | Purpose |
|---|---|
| `IOGRID_ADMIN_EMAILS` | Comma-separated allowlist of admin email addresses. Required. |
| `IOGRID_ADMIN_HOST` | The host this app is served from. Used for cookie-domain pinning. Default `admin.iogrid.org`. |
| `IOGRID_GATEWAY_BFF_URL` | Internal URL for gateway-bff. Default `http://gateway-bff.iogrid.svc.cluster.local:8080`. |
| `IOGRID_SERVICE_TOKEN` | Shared bearer token for the BFF identity-shim. |
| `IOGRID_PROVIDERS_RPC_URL` | (optional) Direct providers-svc RPC URL; otherwise falls back to gateway-bff. |
| `AUTH_SECRET` | NextAuth JWT signing secret. |
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` | Google OAuth credentials. |
| `EMAIL_SERVER_HOST` / `EMAIL_SERVER_PORT` / `EMAIL_SERVER_USER` / `EMAIL_SERVER_PASSWORD` / `EMAIL_FROM` | Magic-link SMTP creds. |
| `DATABASE_URL` | Postgres DSN for the NextAuth session/user store (same DB as web/; tables are shared). |
| `PGSSLMODE` | `disable` to skip TLS (dev only); anything else uses `prefer`. |

## What lands in Phase 2.3

Visual identity (Linear / Notion / Vercel premium-minimal aesthetic) lands in EPIC #422 Phase 2.3. Until then, the chrome here mirrors `web/`'s zinc-on-white shadcn defaults so the move feels lossless. Components in `src/components/ui/` and `src/components/layout/admin-shell.tsx` are the redesign surface.

## Refs

- EPIC #422 — drop `iogrid.org` + independent admin + UX revamp.
- #361 — original admin-split EPIC.
- #408 — the host-aware shim in web/ that this PR replaces.
