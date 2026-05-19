# Off-ramp provider integration guide

This document is the high-level partner-integration guide for the iogrid
off-ramp adapter abstraction. The Go-level architecture lives at
[`coordinator/services/billing-svc/internal/offramp/README.md`](../coordinator/services/billing-svc/internal/offramp/README.md).

Architectural decision is recorded in
[issue #167](https://github.com/iogrid/iogrid/issues/167); the implementation
PRs close [#169](https://github.com/iogrid/iogrid/issues/169) (web flow) and
[#170](https://github.com/iogrid/iogrid/issues/170) (webhook receiver).

---

## Why loose coupling

iogrid providers earn `$GRID` for compute / bandwidth / iOS-build work. When
they want bank-deposit-able fiat, they need a partner who:

1. takes custody of the `$GRID` (briefly, atomically),
2. swaps to USDC (Jupiter / Raydium CLMM), and
3. settles fiat via local rails (ACH / wire / SEPA / GCash / M-Pesa).

Per founder direction 2026-05-19, iogrid does NOT do any of those three steps
in-house. Every off-ramp is a partner integration with a **REST contract** — no
shared code, no shared treasury, no shared legal entity. The contract surface
is exactly four functions:

```go
type Provider interface {
    Name() string
    BuildRedirectURL(req OffRampRequest) (string, error)
    VerifyWebhookSignature(payload []byte, signature string) bool
    ParseWebhook(payload []byte) (*OffRampStatus, error)
}
```

The catalogue is registry-driven (env var `OFFRAMP_PROVIDERS`), so operators
can enable/disable partners per environment without code changes.

---

## Current catalogue

| Provider          | Status               | Real impl?                                                                                          |
|-------------------|----------------------|-----------------------------------------------------------------------------------------------------|
| **MoonPay**       | Default              | Yes — HMAC-SHA256 signed redirect URLs, `Moonpay-Signature-V2` webhooks.                            |
| **Sociable Cash** | Documented contract  | Stub — real adapter lives at `sociable-cloud/cash`. iogrid maintains the contract surface.          |
| **Coinbase**      | Placeholder          | Not yet wired — activates after the Wormhole NTT bridge to Base goes live.                          |

---

## Provider redirect URL contracts

### MoonPay

```
https://sell.moonpay.com/?apiKey=<key>
  &defaultBaseCurrencyCode=grid
  &baseCurrencyAmount=<decimal GRID>
  &quoteCurrencyCode=<usd|eur|...>
  &refundWalletAddress=<solana pubkey>
  &externalCustomerId=<iogrid user id>
  &externalTransactionId=<iogrid request_id>
  &redirectURL=<our return url>
  &signature=<HMAC-SHA256 base64 of the query-string under MOONPAY_WEBHOOK_SECRET>
```

### Sociable Cash

```
https://cash.sociable.cloud/off-ramp?from=GRID
  &amount=<lamports>
  &signer=<provider wallet pubkey>
  &return_url=<our return url>
  &ref=<iogrid request_id>
  [&currency=<USD|EUR|PHP|...>]
```

(Unsigned today — Cash's KYC pipeline re-confirms swap amount with the user
client-side. When Cash adds signed redirects we'll extend `OffRampRequest`
with a `Secret` field; the interface is already designed for that.)

### Coinbase (planned)

Coinbase Pay's redirect spec will be added once the Wormhole NTT bridge is
live. The interface won't change — only the adapter body.

---

## Webhook payload contracts

Every adapter implements `VerifyWebhookSignature` + `ParseWebhook`, but the
wire shapes differ per partner. The canonical `OffRampStatus` shape the
adapters produce is identical:

```ts
type OffRampStatus = {
  request_id: string;
  provider_id: string;
  status: "pending" | "swapping" | "off-ramping" | "completed" | "failed";
  grid_amount: number;       // lamports (uint64)
  fiat_amount: string;       // "150.00"
  fiat_currency: string;     // ISO-4217
  completed_at: string | null;
  txn_signature: string;     // on-chain GRID→USDC swap signature
  provider_ref_id: string;   // partner's internal id
};
```

### MoonPay

- Header: `Moonpay-Signature-V2: t=<unix>,s=<hex hmac-sha256>`
- Signed payload: `<timestamp>.<raw body>`
- Body envelope:
  ```json
  {
    "type": "transaction_updated",
    "data": {
      "id": "<moonpay txn id>",
      "externalTransactionId": "<our request_id>",
      "externalCustomerId": "<our user id>",
      "status": "completed|waitingForSwap|...",
      "baseCurrencyAmount": 1.5,
      "quoteCurrencyAmount": 150.00,
      "quoteCurrencyCode": "usd",
      "cryptoTransactionId": "<solana sig>",
      "updatedAt": "<RFC3339>"
    }
  }
  ```

### Sociable Cash

- Header: `Cash-Signature: <hex hmac-sha256>` over the raw body, under
  `CASH_WEBHOOK_SECRET`.
- Body envelope:
  ```json
  {
    "offramp_id":    "<cash internal id>",
    "ref":           "<our request_id>",
    "provider_id":   "<our user id>",
    "status":        "pending|swapping|off-ramping|completed|failed",
    "grid_amount":   "<decimal GRID, 9 dp>",
    "fiat_amount":   "150.00",
    "fiat_currency": "USD",
    "txn_signature": "<solana sig>",
    "completed_at":  "<RFC3339>"
  }
  ```

---

## Env-var surface

| Env var                  | Required when                     | Description                                       |
|--------------------------|-----------------------------------|---------------------------------------------------|
| `OFFRAMP_PROVIDERS`      | Always                            | Comma-separated provider names in display order.  |
| `MOONPAY_API_KEY`        | `moonpay` in `OFFRAMP_PROVIDERS`  | MoonPay publishable key.                          |
| `MOONPAY_WEBHOOK_SECRET` | `moonpay` in `OFFRAMP_PROVIDERS`  | Signs redirect URLs + verifies webhooks.          |
| `MOONPAY_BASE_URL`       | Optional                          | Defaults to `https://sell.moonpay.com`.           |
| `CASH_WEBHOOK_SECRET`    | `sociable-cash` in `OFFRAMP_PROVIDERS` | Shared secret with the Sociable Cash team. |
| `CASH_BASE_URL`          | Optional                          | Defaults to `https://cash.sociable.cloud`.        |

---

## Contract gaps tracked for the Sociable Cash team

Outstanding items in [#167](https://github.com/iogrid/iogrid/issues/167):

1. **Quote endpoint** — Cash has not yet published a pre-redirect quote API.
   Until then iogrid renders `Estimated fiat: ~$X.XX` client-side from Pyth
   `$GRID/USD` × 0.97 (3% slippage buffer).
2. **Multi-rail routing** — when Cash adds GCash / M-Pesa rails, the
   `fiat_currency` string will gain values like `"PHP-GCASH"` so rail-aware
   reporting in billing-svc works.
3. **Atomic custody / refund flow** — today a failed off-ramp leaves the
   `$GRID` in the provider's wallet (Cash never custodied it). When Cash adds
   atomic custody they'll emit a `"refunded"` status that we'll map to
   `StatusFailed` with a non-nil completion timestamp.
4. **Signed redirects** — current redirect contract is unsigned. When Cash
   adds redirect signing, extend `offramp.OffRampRequest` with a `Secret`
   field and HMAC the query string. Interface is already designed for that.
5. **JWT-based webhook auth** — Cash may prefer JWTs over raw HMAC. The
   `VerifyWebhookSignature` interface accepts an opaque string so either
   scheme is compatible without changes upstream.

---

## Adding a new partner

See [`coordinator/services/billing-svc/internal/offramp/README.md`](../coordinator/services/billing-svc/internal/offramp/README.md)
for the step-by-step ("how to add a new provider").

The interface is small enough that a new partner integration is typically
~150 lines of Go + a config block + a README entry. We deliberately resist
adding methods to `Provider` — every new method makes every adapter heavier.
If a new partner needs a feature that doesn't fit (e.g. asynchronous quote
generation, multi-step KYC handoff), prefer extending `OffRampStatus` or
adding a new dedicated route, NOT widening the four-method core contract.
