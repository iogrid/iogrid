# @iogrid/admin

iogrid staff console — a standalone Next.js 15 app served at
**admin.iogrid.org**.

Split out of `web/` in #361 (founder direction #8) so the admin surface
has independent deploy cadence, its own container image, and isolation
from end-user surfaces. mTLS / WireGuard hardening is layered in a
follow-up; today's gate is the `IOGRID_ADMIN_EMAILS` allowlist enforced
by the edge middleware (defense-in-depth alongside gateway-bff's
`RequireRole("ADMIN")` middleware).

## Layout

```
admin/
├── Dockerfile                  ghcr.io/iogrid/admin
├── next.config.ts              standalone output
├── postcss.config.mjs          Tailwind 4
├── src/
│   ├── auth.config.ts          edge-safe NextAuth config (Google + JWT only)
│   ├── middleware.ts           IOGRID_ADMIN_EMAILS gate + sign-in redirect
│   ├── app/
│   │   ├── layout.tsx          ThemeProvider + Toaster + globals.css
│   │   ├── page.tsx            / — staff console overview
│   │   ├── signin/             /signin (NextAuth credential entry)
│   │   ├── abuse/              /abuse — antiabuse-svc filter ruleset
│   │   ├── customers/          /customers — KYC review (placeholder)
│   │   ├── providers/          /providers — paired-pool list + audit lookup
│   │   ├── finops/             /finops (placeholder)
│   │   ├── settings/           /settings (placeholder)
│   │   ├── healthz/            /healthz — liveness probe
│   │   ├── readyz/             /readyz — readiness probe
│   │   └── api/v1/admin/       same-origin BFF proxy (mirrors web/)
│   ├── components/
│   │   ├── layout/admin-shell.tsx   chrome + section nav
│   │   ├── audit-event-card.tsx     read-only audit row
│   │   ├── theme-provider.tsx       next-themes wrapper
│   │   ├── theme-toggle.tsx         3-state theme cycle
│   │   └── ui/                      button / input / card (shadcn)
│   ├── db/                          drizzle PG schema for NextAuth
│   ├── lib/
│   │   ├── auth.ts                  node-only NextAuth (DrizzleAdapter + nodemailer)
│   │   ├── admin-allowlist.ts       parseAdminEmails / isAdminEmail
│   │   ├── api.ts                   thin fetch wrapper
│   │   ├── bff-proxy.ts             NextAuth cookie → gateway-bff service-token shim
│   │   ├── format.ts                formatRelativeTime
│   │   ├── sse.ts                   useSSE hook (mirrors web/)
│   │   ├── types.ts                 BFF JSON shapes
│   │   └── utils.ts                 cn (tailwind class merger)
│   └── test/                        Vitest unit tests
└── tsconfig.json
```

## Development

```bash
pnpm install          # at repo root or inside admin/
pnpm --filter @iogrid/admin dev
# → http://localhost:3001
```

The admin app shares the same Postgres `user` / `account` / `session` /
`verificationToken` tables as `web/`, so an operator who signs in at
`app.iogrid.org` is the same row as the one signing in at
`admin.iogrid.org`. Only the `IOGRID_ADMIN_EMAILS` allowlist gates
entry to the staff surface.

## Env vars

| Var | Purpose |
|---|---|
| `AUTH_SECRET` | NextAuth JWT signing secret (required) |
| `NEXTAUTH_URL` | Base URL of the admin app (e.g. `https://admin.iogrid.org`) |
| `IOGRID_ADMIN_EMAILS` | Comma-separated allowlist of admin emails |
| `DATABASE_URL` | Postgres connection string for NextAuth tables |
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` | Google OAuth |
| `EMAIL_SERVER_HOST` / `EMAIL_SERVER_PORT` / `EMAIL_SERVER_USER` / `EMAIL_SERVER_PASSWORD` / `EMAIL_FROM` | Magic-link SMTP |
| `IOGRID_GATEWAY_BFF_URL` | Upstream gateway-bff (in-cluster DNS) |
| `IOGRID_SERVICE_TOKEN` | Shared service token for the BFF shim |

## Deployment

`infra/k8s/base/admin/` hosts the Deployment + Service + ServiceAccount
+ HPA + NetworkPolicy + CiliumNetworkPolicy. The Traefik IngressRoute
that fronts the public admin.iogrid.org host lives at
`infra/k8s/traefik/ingressroute-admin.yaml` (#361). DNS is wired in
`infra/dynadot/iogrid-org-records.json`; the `admin` SAN is on the
`iogrid-org-tls` Certificate at `infra/k8s/certificates/iogrid-org-cert.yaml`.
