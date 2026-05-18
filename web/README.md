# iogrid — Web (Track 3)

Next.js 15 App Router management plane. TypeScript 5 strict, Tailwind 4,
NextAuth.js v5 (Google + magic-link email), TanStack Query, React Hook Form
+ Zod, Vitest + Playwright + Storybook.

## Routes

| Path        | Purpose                                        | Auth |
| ----------- | ---------------------------------------------- | ---- |
| `/`         | Landing — brand + nav                          | no   |
| `/account`  | Sign-in / sign-up                              | no   |
| `/provide`  | Provider dashboard (nodes, payouts)            | yes  |
| `/customer` | Customer dashboard (workloads, billing)        | yes  |
| `/vpn`      | Daemon download (macOS / Linux / Windows)      | no   |
| `/admin`    | Operator console (gated by `admin` role)       | yes  |

`/provide`, `/customer`, `/admin` are gated by `src/middleware.ts`. The
merged-identity contract — single user = both provider and customer —
lives in the coordinator; the JWT only carries the user id.

## i18n

Seven locales: `en`, `es`, `pt`, `de`, `fr`, `it`, `tr`. See
`src/i18n/config.ts`.

## Local development

```bash
pnpm install
pnpm dev
```

## Quality gates

```bash
pnpm typecheck   # tsc --noEmit
pnpm lint        # next lint
pnpm test        # vitest run
pnpm test:e2e    # playwright test
pnpm build       # next build (standalone output)
```

## Docker

```bash
docker build -t iogrid/web:dev .
docker run --rm -p 3000:3000 iogrid/web:dev
```

## CI

`.github/workflows/web-ci.yml` runs typecheck, lint, build, unit tests, and a
docker build on every push touching `web/`. The first run generates
`pnpm-lock.yaml`, which is committed back via a follow-up PR.
