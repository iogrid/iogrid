# $GRID transparency report — YYYY Qn

> Status: DRAFT | PUBLISHED
> Reporting period: YYYY-MM-DD → YYYY-MM-DD
> Publish date: YYYY-MM-DD
> Slot range covered: <start_slot> → <end_slot>
> Report author: <foundation signer>
> Prior report: [YYYY-Q(n-1)](./YYYY-Qn-1.md)

---

## 1. Treasury balance

Snapshot taken at slot `<slot>` (~YYYY-MM-DD HH:MM UTC).

**Squads multisig** (`<multisig_address>`, 3-of-5):

| Asset | Balance | USD value at snapshot | Notes |
|-------|--------:|----------------------:|-------|
| USDC  | TBD     | TBD                   | Stablecoin operating reserve |
| $GRID | TBD     | TBD                   | Governance / future grants pool |
| SOL   | TBD     | TBD                   | Gas reserve for foundation ops |
| Other | TBD     | TBD                   | Itemise if material (>$10k) |

**Off-chain holdings** (if any):

| Counterparty | Asset | Balance | Reason held off-chain |
|--------------|-------|--------:|-----------------------|
| —            | —     | —       | —                     |

**Quarter-over-quarter delta:** TBD (net change in USD-equivalent treasury).

---

## 2. Emission this quarter

Programmatic emission from the SPL emission program. No discretionary mints.

| Metric | This quarter | Cumulative since TGE |
|--------|-------------:|---------------------:|
| $GRID emitted | TBD | TBD |
| % of supply (1B total) | TBD | TBD |
| Current emission rate (per day) | TBD | — |
| Next halving in | TBD days | — |

**Actual vs. curve:** TBD. The emission program is hard-coded so deviation is
zero by construction; this row exists to confirm the program ran without
interruption.

---

## 3. Buy-and-burn

| Metric | This quarter |
|--------|-------------:|
| Burn events | TBD |
| Total $GRID burned | TBD |
| Burn-address balance (cumulative since TGE) | TBD |
| USD value burned (at execution-time prices) | TBD |
| % of quarter revenue routed to burn | TBD (target ≥2%) |

**Mechanism:** Daily automated process converts ≥2% of platform revenue to
$GRID via Jupiter swap and sends to `1nc1nerator1111111111111111111111111111111`.

**Top 5 burn transactions this quarter:**

| Date | TX signature | $GRID burned | USD value |
|------|--------------|-------------:|----------:|
| TBD  | TBD          | TBD          | TBD       |

---

## 4. Staking participation

| Metric | Value at quarter end |
|--------|---------------------:|
| Total $GRID staked | TBD |
| % of circulating supply staked | TBD |
| % of total supply staked | TBD |
| Distinct stakers | TBD |
| Active validator / provider nodes | TBD |
| Median stake per provider | TBD |
| Largest single stake (% of total staked) | TBD |

**Provider lockup pool:** TBD $GRID locked under the mandatory provider
earnings lockup (Layer 3 of the deflationary mechanism).

---

## 5. Liquidity health

**Raydium CLMM pool** `$GRID / USDC`:

| Metric | Value |
|--------|------:|
| Pool address | `<pool_address>` |
| Active liquidity (USD-equivalent) | TBD |
| 30-day volume | TBD |
| Concentration range (% of liquidity within ±5% of mid) | TBD |
| Foundation-owned LP position | TBD |

**Other venues:** TBD (Jupiter aggregator flows, CEX listings if any).

**Slippage benchmark:** TBD bps for a $10k market buy at quarter close.

---

## 6. Foundation activity

### Grants disbursed

| Recipient | Amount | Purpose | Disbursement TX |
|-----------|-------:|---------|-----------------|
| —         | —      | —       | —               |

### Partnerships announced

- TBD

### Governance proposals

| Proposal | Status | Vote outcome |
|----------|--------|--------------|
| —        | —      | —            |

### Headcount / operations

- TBD

---

## 7. Compliance / legal updates

- Material regulatory developments affecting $GRID: TBD
- Audits in progress / completed this quarter: TBD
- Jurisdictional posture changes: TBD
- Material litigation, subpoenas, or enforcement contact: TBD (or "None")

---

## 8. Known issues / forward look

### Known issues

- TBD

### Forward look (next quarter, no price expectations)

- TBD

---

## Methodology

**RPC endpoint:** `<endpoint_url>`
**Block / slot range:** `<start_slot>` → `<end_slot>`

**Commands used:**

```bash
# Treasury balance
spl-token accounts --owner <multisig_address> --url <endpoint_url>

# Emission this quarter
solana program show <emission_program_id> --url <endpoint_url>
# + custom query against the emission program's state account

# Burn totals
spl-token balance 1nc1nerator11111111111111111111111111111111 --url <endpoint_url>
# delta vs. snapshot at start of quarter

# Pool state
# Raydium CLMM pool state via their on-chain account layout

# Staking
# Custom RPC against the iogrid staking program state
```

**Off-chain reconciliations:** TBD.

---

## Corrections

(Append entries here if values are restated after publication. Format:
`YYYY-MM-DD — <field> corrected from <old> to <new>. Reason: <text>.`)
