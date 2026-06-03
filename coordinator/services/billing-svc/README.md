# billing-svc

Bounded context: prepaid **$GRID** session settlement (the core billing model — customers pre-fund a $GRID balance with a capped grace window), Stripe top-up / subscription checkout, provider payouts (Stripe Connect cash tier + Solana $GRID token tier), metering aggregation, quarterly 1099 generation.

$GRID is an SPL **Token-2022** mint with **9 decimals** (never 6). The mainnet mint is **not deployed**; devnet mint is `BaQvWwb1wUGvWJXPEUbLEwPeeYMd4sKvp2S7obzTWorR`. See `docs/BUSINESS-STRATEGY.md` §4 (Currency model) and `docs/ARCHITECTURE.md` §"Coordinator" for placement.

## What's wired

| Subsystem | Status | File |
|-----------|--------|------|
| $GRID session-settlement meter (`/v1/grid/session-end`, `/v1/grid/balance`) | Wired | `internal/grid/session_meter.go`, `internal/server/grid_handlers.go` |
| $GRID settlement-worker cron (batched SPL `TransferChecked` per provider wallet) | Wired (PENDING rows until mint deployed) | `internal/grid/settlement_cron.go` |
| Stripe Checkout (customer subscriptions / top-up) | Production-ready | `internal/stripeapi/stripe.go` |
| Stripe Customer Portal | Production-ready | `internal/stripeapi/stripe.go` |
| Stripe webhook router (`checkout.session.completed`, `customer.subscription.*`, `invoice.payment_*`) | Production-ready | `internal/stripeapi/stripe.go` |
| Stripe Connect Express onboarding | Production-ready | `internal/stripeapi/stripe.go` |
| Stripe Connect instant payout | Production-ready | `internal/stripeapi/stripe.go` |
| NATS JetStream metering consumer | Production-ready | `internal/metering/metering.go` |
| Daily metering rollup (per workspace + per provider) | Production-ready | `internal/store/store.go` (`RollupDay`) |
| Jupiter swap quote client (USD→$GRID) | Production-ready | `internal/solana/solana.go` |
| Solana daily payout + burn loop | **Stub when `GRID_TOKEN_MINT_ADDRESS` empty** | `internal/solana/solana.go` |
| Solana hot wallet (single keypair) | **Phase 0/1 only** — multisig (2-of-3 via Squads) deferred to Phase 2 | `internal/solana/solana.go` |
| Quarterly 1099-NEC PDF | Production-ready | `internal/tax/tax.go` |
| Quarterly $GRID 1099-equivalent PDF | Production-ready | `internal/tax/tax.go` |

## What's stubbed (and why)

- **On-chain SPL transfer**: we record the swap quote and persist the payout row with status `PENDING`, but the actual `solana_program::system_instruction::transfer` is not emitted. This will land alongside the Anchor program in issues #88-#91. In stub mode (no token mint configured) the rows are written with status `SKIPPED` so dashboards can still surface "would have paid out X".
- **Multisig hot wallet**: Phase 0/1 runs a single keypair loaded from `SOLANA_HOT_WALLET_KEYPAIR_PATH`. Pre-mainnet TGE we migrate to 2-of-3 multisig via Squads Protocol — tracked under issue #98.
- **IRS-filed 1099 transmission**: The PDFs we generate are personal records. Actual IRS filing routes through TaxBandits / Track1099 from a CSV export.
- **Webhook idempotency on Stripe duplicates**: Stripe occasionally re-delivers; the `stripe_subscription_id` UNIQUE constraint dedupes subscription rows. Idempotency keys on the Connect transfer call are a separate hardening pass.

## Environment variables

| Variable | Required | Default | Notes |
|----------|----------|---------|-------|
| `LISTEN_ADDR` | no | `:8080` | chi listener |
| `DATABASE_URL` | **yes** | — | libpq DSN for billing-svc's logical database |
| `NATS_URL` | no | — | When empty, metering consumer is disabled |
| `STRIPE_SECRET_KEY` | no* | — | When empty, Stripe routes return 503 |
| `STRIPE_WEBHOOK_SECRET` | no* | — | Required for webhook signature validation |
| `STRIPE_CONNECT_CLIENT_ID` | no | — | Reserved for OAuth Connect (not the Express flow) |
| `STRIPE_PRICE_PAYG` | no* | — | Stripe Price id for the PAYG plan |
| `STRIPE_PRICE_STARTER` | no* | — | Stripe Price id for the Starter plan |
| `STRIPE_PRICE_GROWTH` | no* | — | Stripe Price id for the Growth plan |
| `STRIPE_PRICE_ENTERPRISE` | no* | — | Stripe Price id for Enterprise |
| `WEB_BASE_URL` | no | `https://app.iogrid.org` † | Used to construct return URLs |
| `SOLANA_RPC_URL` | no | `https://mainnet.helius-rpc.com` | Helius / Triton RPC |
| `SOLANA_HOT_WALLET_KEYPAIR_PATH` | no | — | JSON array of 64 ints (Solana CLI format); mounted as a k8s secret |
| `GRID_TOKEN_MINT_ADDRESS` | no | — | Empty during Phase 0/1; Solana subsystem boots in stub mode |
| `JUPITER_API_URL` | no | `https://quote-api.jup.ag/v6` | DEX aggregator quote endpoint |
| `BURN_PERCENTAGE` | no | `2.0` | Fraction of daily revenue routed to burn |
| `INCINERATOR_ADDRESS` | no | `1nc1nerator11111111111111111111111111111111` | Solana well-known burn address |
| `DAILY_PAYOUT_CRON` | no | `5 0 * * *` | Reserved — cron is wired in the k8s CronJob, not the binary loop |

\* Required only when running Stripe-touching code paths. The service degrades cleanly with 503 when a key is missing rather than crashing on boot.

† The `app.iogrid.org` subdomain has been **dropped** in favour of the `iogrid.org` apex (301-redirected). Set `WEB_BASE_URL=https://iogrid.org` in new environments; the compiled-in default still points at the old subdomain (code follow-up flagged in the PR).

## Stripe setup runbook

1. **Get keys.** In Stripe Dashboard → Developers → API keys, copy the *Secret key* into `STRIPE_SECRET_KEY` (use the `sk_test_…` key for non-prod environments).
2. **Create Prices.** Stripe Dashboard → Products → for each tier (PAYG / Starter / Growth / Enterprise) create a Product and a recurring Price, then set the corresponding `STRIPE_PRICE_*` env var.
3. **Webhook endpoint.** Stripe Dashboard → Developers → Webhooks → add endpoint `https://api.iogrid.org/billing/v1/stripe/webhook` and subscribe to:
   - `checkout.session.completed`
   - `customer.subscription.created`
   - `customer.subscription.updated`
   - `customer.subscription.deleted`
   - `invoice.payment_succeeded`
   - `invoice.payment_failed`
   Copy the signing secret into `STRIPE_WEBHOOK_SECRET`.
4. **Stripe Connect.** Stripe Dashboard → Connect → enable *Express* accounts. The `transfers` capability is requested per-account at onboarding, no platform-level config required beyond enabling Connect.
5. **Smoke test.** `curl -X POST https://api.iogrid.org/billing/v1/subscriptions/<ws-uuid>/checkout -d '{"tier":"STARTER","success_url":"https://iogrid.org/billing/ok","cancel_url":"https://iogrid.org/billing/no"}'` — expect a `checkout_url` Stripe Checkout link.

## Solana hot-wallet provisioning checklist

> ⚠ This applies **only post-TGE** — until then the subsystem boots in stub mode.

1. **Generate keypair.** `solana-keygen new --outfile billing-svc-hotwallet.json --no-bip39-passphrase`. The output is the canonical JSON-array-of-64-ints format we expect.
2. **Mount as k8s secret.**
   ```yaml
   apiVersion: v1
   kind: Secret
   metadata:
     name: billing-svc-solana-hotwallet
     namespace: iogrid
   type: Opaque
   data:
     keypair.json: <base64>
   ```
   Mount at `/var/secrets/solana/keypair.json` and set `SOLANA_HOT_WALLET_KEYPAIR_PATH=/var/secrets/solana/keypair.json`.
3. **Initial funding.** Send a small amount of SOL (0.05 SOL is sufficient for ~1000 transactions) to the hot wallet from the Squads treasury. The hot wallet itself never holds significant funds — daily revenue is held in a separate Token-2022 account and only the day's payout amount is moved through.
4. **Multisig migration.** Pre-TGE the hot wallet is a single keypair. Pre-mainnet launch we migrate to Squads Protocol 2-of-3: founder + ops + automated-bot signers. The bot signer's private key remains in the k8s secret; the other two are hardware wallets. This is tracked separately under issue #98.
5. **Rotation cadence.** Hot-wallet keypair rotates every 90 days. Migration steps:
   1. Generate new keypair, mount under a new secret name.
   2. Squads multisig transfers all remaining balance to the new wallet.
   3. Delete old secret after one full payout cycle has succeeded.

## Tests

```sh
# Unit tests (fast, no network)
go test ./...

# Integration tests (httptest-faked Stripe + Jupiter; tagged)
go test -tags integration ./...
```

The Stripe handler unit tests use a `fakeBackend` implementing the `Backend` interface (no Stripe API calls). The Jupiter swap-decision tests use `httptest` to serve canned `/quote` responses. The PDF generator tests assert the resulting bytes carry the `%PDF-` magic header.

## Wire-up notes for ops

- Health probes: `/healthz` (liveness, always 200), `/readyz` (DB ping; 503 when Postgres is down).
- Metrics: Prometheus on `/metrics`, default-registry from `prometheus/client_golang`.
- OTLP traces: emitted via the shared `coordinator/shared/otel` package — set `OTEL_EXPORTER_OTLP_ENDPOINT` to e.g. `https://otel-collector.iogrid:4317`.
- Migrations: `internal/store/migrations/*.sql` is embedded into the binary. Run-up via `goose` (the shared `db.MigrateUp` helper) on first boot.
