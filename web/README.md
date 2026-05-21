# iogrid — Web (Track 3)

Next.js 15 App Router management plane. TypeScript 5 strict, Tailwind 4,
NextAuth.js v5 (Google + magic-link email), TanStack Query, React Hook Form
+ Zod, Vitest + Playwright + Storybook.

## Routes

| Path                | Purpose                                              | Auth |
| ------------------- | ---------------------------------------------------- | ---- |
| `/`                 | Landing — brand + nav                                | no   |
| `/account`          | Sign-in / sign-up                                    | no   |
| `/account/wallets`  | Solana wallet bindings (SIWS handshake)              | yes  |
| `/provide`          | Provider dashboard (nodes, payouts)                  | yes  |
| `/provide/staking`  | $GRID staking + locked positions                     | yes  |
| `/customer`         | Customer dashboard (workloads, billing)              | yes  |
| `/vpn`              | Daemon download (macOS / Linux / Windows)            | no   |
| `/admin`            | Operator console (gated by `admin` role)             | yes  |
| `/burn`             | Public $GRID burn dashboard                          | no   |

`/provide`, `/customer`, `/admin` are gated by `src/middleware.ts`. The
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
- `NEXT_PUBLIC_GRID_MINT_ADDRESS` — SPL mint for $GRID. Defaults to a
  placeholder so the build compiles before TGE; the balance widget
  reports zero until a real mint is configured.

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
