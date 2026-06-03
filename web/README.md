# iogrid — Web (Track 3)

Next.js 15 App Router management plane. TypeScript 5 strict, Tailwind 4,
NextAuth.js v5 (Google + magic-link email), TanStack Query, React Hook Form
+ Zod, Vitest + Playwright + Storybook.

All routes live under the **iogrid.org apex**. The old `app.iogrid.org`
subdomain is retired (301 → apex); the operator console is a **separate
app** at `admin.iogrid.org` and is no longer part of this codebase.

| Path                 | Purpose                                              | Auth |
| -------------------- | ---------------------------------------------------- | ---- |
| `/`                  | Landing — brand + nav                                | no   |
| `/account`           | Account hub (sign-in, profile, sessions)             | no   |
| `/account/wallets`   | Solana wallet bindings (SIWS handshake)              | yes  |
| `/provider`          | Provider dashboard (earnings, schedule, audit)       | yes  |
| `/provider/staking`  | $GRID staking + locked positions                     | yes  |
| `/customer`          | Customer dashboard (workloads, billing)              | yes  |
| `/install`           | Daemon installer landing (curl-pipe + double-click)  | no   |
| `/vpn`               | Daemon download (macOS / Linux / Windows)            | no   |
| `/token` · `/burn`   | $GRID token + public burn dashboard                  | no   |

`/provider` and `/customer` are gated by `src/middleware.ts`
(`PROTECTED_PREFIXES`); unauthenticated hits redirect to `/account`. The
merged-identity contract — single user = both provider and customer —
lives in the coordinator; the JWT only carries the user id.

## Solana wallet adapter

The app mounts `@solana/wallet-adapter-react` once at the root layout
(`src/lib/solana/provider.tsx`), so every client component can call
`useWallet()` / `useConnection()`. Supported adapters: Phantom,
Solflare, Trust, and any wallet that auto-registers via the Solana
Wallet Standard (Backpack, Glow, etc.).

Configuration:

- `NEXT_PUBLIC_SOLANA_RPC_URL` — RPC endpoint (default mainnet-beta).
- `NEXT_PUBLIC_GRID_MINT_ADDRESS` — SPL Token-2022 mint for $GRID
  (**9 decimals**). The **mainnet mint is not yet deployed**; the default
  is a pre-TGE placeholder so the build compiles and the balance widget
  reports zero until a real mint is configured. The devnet mint is
  `BaQvWwb1wUGvWJXPEUbLEwPeeYMd4sKvp2S7obzTWorR`.

See `docs/BUSINESS-STRATEGY.md` §4 (Currency model — $GRID + fiat hybrid) for the full token + staking + burn model.

## i18n

Seven locales: `en`, `es`, `pt`, `de`, `fr`, `it`, `tr`. See
`src/i18n/config.ts`.

## Local development

```bash
pnpm install
pnpm dev
```

### Optional: bun for faster local installs

`bun` is supported as a faster alternative to `pnpm` for local dev installs.
The Homebrew-core `bun` formula was removed upstream; install via the
official `oven-sh/bun` tap (or the curl installer):

```bash
# macOS / Linux (Homebrew) — tap-first, the bare formula no longer resolves
brew tap oven-sh/bun
brew install bun

# Alternative: official installer
curl -fsSL https://bun.sh/install | bash
```

Then `bun install && bun run dev` works in place of the pnpm commands. CI
and the committed `pnpm-lock.yaml` remain the source of truth — bun is a
local-dev convenience only, not a release path.

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
