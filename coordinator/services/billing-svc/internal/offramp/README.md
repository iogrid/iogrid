# Off-ramp adapter abstraction

This package defines the provider-agnostic contract that lets iogrid plug into
any fiat off-ramp partner — MoonPay (default real implementation), Sociable
Cash (cross-org contract stub), and Coinbase (placeholder for the post-Wormhole
Base bridge).

Architectural decision is recorded in
[`docs/BUSINESS-STRATEGY.md` §5 (Off-ramp partner integrations)](../../../../../docs/BUSINESS-STRATEGY.md#5-off-ramp-partner-integrations)
and the originating EPIC at
[`iogrid/iogrid#167`](https://github.com/iogrid/iogrid/issues/167).
This README is the implementer's guide.

---

## Why an adapter abstraction?

Provider payouts are denominated in $GRID. Providers that want bank-deposit-able
fiat need a partner to:

1. take custody of the $GRID (briefly, atomically),
2. swap it to USDC (Jupiter / Raydium CLMM), and
3. settle fiat to the user's bank / GCash / M-Pesa / SEPA.

iogrid does NOT do any of those three steps in-house — every off-ramp is a
partner integration. Per founder direction 2026-05-19 (issue #167), iogrid +
Sociable Cash are loosely coupled: independent products, independent tokens,
separate legal entities. The right abstraction is a REST contract, not shared
code.

The `Provider` interface in [`offramp.go`](./offramp.go) is the smallest
contract that captures every partner's needs:

```go
type Provider interface {
    Name() string
    BuildRedirectURL(req OffRampRequest) (string, error)
    VerifyWebhookSignature(payload []byte, signature string) bool
    ParseWebhook(payload []byte) (*OffRampStatus, error)
}
```

Anything else (KYC, swap execution, fiat rails, partner account management)
lives behind the redirect URL on the partner's side.

---

## End-to-end flow

```
┌──────────────────────────────────────────────────────────────────────┐
│ 1. Provider clicks "Withdraw" in /provide/earnings                   │
│                                                                      │
│ 2. web → gateway-bff POST /api/v1/offramp/start                      │
│      → billing-svc StartOffRamp(provider_name, …)                    │
│           a. Resolve Provider via registry.GetProvider(name)         │
│           b. Persist offramp_request row (status='pending')          │
│           c. Provider.BuildRedirectURL(req) → signed partner URL     │
│        ← { request_id, redirect_url }                                │
│                                                                      │
│ 3. browser redirects to partner URL; partner handles KYC + swap      │
│                                                                      │
│ 4. Partner POSTs back:                                               │
│      /api/v1/webhooks/offramp/{provider_name}                        │
│      → billing-svc HandleWebhook(provider_name, body, signature)     │
│           a. Provider.VerifyWebhookSignature(body, sig)              │
│           b. Provider.ParseWebhook(body) → OffRampStatus             │
│           c. Update offramp_request row + emit telemetry             │
│                                                                      │
│ 5. UI polls /api/v1/offramp/status/{request_id} until status=        │
│    'completed' OR shows partner-side redirect-back banner.           │
└──────────────────────────────────────────────────────────────────────┘
```

---

## How to add a new provider

1. **Create a subpackage** under `internal/offramp/<provider>/`.
2. **Implement `offramp.Provider`** — four methods, no extra deps allowed
   between adapters.
3. **Add config knobs** to `internal/config/config.go` — typically
   `<PROVIDER>_API_KEY`, `<PROVIDER>_WEBHOOK_SECRET`, `<PROVIDER>_BASE_URL`.
4. **Wire it in `cmd/billing-svc/main.go#buildOffRampService`** — add a new
   case to the registry-building switch.
5. **Surface it in the web picker** at `web/src/app/provide/earnings/withdraw.tsx`
   — add an entry to `KNOWN_PROVIDERS`.
6. **Document the partner contract** in `docs/BUSINESS-STRATEGY.md` §5 (Off-ramp
   partner integrations) — redirect URL spec, webhook payload, signature scheme.
7. **Write tests** — at minimum `Name()`, redirect-URL builder, signature
   verification, status parsing.

The MoonPay adapter is the reference implementation; the Sociable Cash adapter
is the documented contract stub.

---

## Status lifecycle

Constants live in [`offramp.go`](./offramp.go); the persistence-layer enum
matches in `internal/store/migrations/0004_offramp_requests.sql`.

| Status        | When set                                              |
|---------------|-------------------------------------------------------|
| `pending`     | Row created; redirect URL issued.                     |
| `swapping`    | Partner is running the on-chain $GRID → USDC swap.    |
| `off-ramping` | Partner is settling fiat to the user's bank.          |
| `completed`   | Fiat hit the user's account.                          |
| `failed`      | Any terminal failure (KYC declined, swap rejected, …).|

Implementations MUST be idempotent: a webhook re-delivery for the same partner
reference id maps to the same `OffRampStatus`.

---

## Tests

```bash
cd coordinator/services/billing-svc
go test ./internal/offramp/...
```

Per-package: registry deduping + insertion-order, MoonPay HMAC roundtrip,
Sociable Cash documented contract URL shape.
