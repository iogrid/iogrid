# Foundation operating budget — multi-year plan

**Status:** Draft v0.1 — pre-counsel-review. **Not legal advice.** **This is an operational
budget planning document for the founder, not a substitute for advice from qualified
counsel + a chartered accountant.**

**Related issues:**

- [#103](https://github.com/iogrid/iogrid/issues/103) — Foundation jurisdiction selection
- [#122](https://github.com/iogrid/iogrid/issues/122) — Foundation incorporation
- [#155](https://github.com/iogrid/iogrid/issues/155) — `legal/*` requires counsel review

**Purpose:** Provide a multi-year operating budget for the iogrid Foundation, from Year 1
(incorporation) through Year 3+ (full compliance team). Costs are public-market estimates;
counsel + accountant will refine.

**Default jurisdiction assumption:** **Cayman Islands** (per
`legal/foundation/jurisdiction-comparison.md` §7.1). For BVI / Liechtenstein / Wyoming /
Switzerland alternatives, multiply Year-1 setup by the relevant jurisdiction-comparison
multiplier (see Section 5 of this document).

---

## 0. Top-level summary

| Year | Range (USD) | Midpoint (USD) | Phase milestone |
|------|-------------|----------------|------------------|
| Year 1 | $300,000 – $500,000 | $400,000 | Foundation incorporated + TGE executed |
| Year 2 | $500,000 – $1,000,000 | $750,000 | Scale up; MTL applications if needed |
| Year 3+ | $1,000,000+ | $1,200,000+ | Full compliance team; DAO-migration prep |
| Reserves / contingency | 20–30% of annual | — | Stress reserve |

Three-year total (midpoint, before reserves): ~$2.4M.

Three-year total with 25% reserve: ~$3.0M.

These are Foundation-only costs. Operating-entity costs (Dynolabs Inc. — product
engineering, infrastructure, sales, marketing) are SEPARATE and tracked in the operating-
entity's own budget. The Foundation pays Dynolabs an operating-services fee under a
separate service agreement (see `legal/foundation/cayman-setup.md` §2.3).

---

## 1. Year 1 — incorporation + TGE + initial operations

**Range: $300,000 – $500,000. Midpoint: $400,000.**

### 1.1 Foundation incorporation (one-time)

| Line item | Range | Midpoint | Notes |
|-----------|-------|----------|-------|
| Cayman counsel fees (setup) | $30,000 – $80,000 | $55,000 | Walkers / Maples / Conyers / Ogier |
| Government + registry fees | $1,000 – $3,000 | $2,000 | One-time |
| Independent directors / supervisors (signing) | $5,000 – $15,000 | $10,000 | One-time onboarding |
| Banking setup | $0 – $2,000 | $1,000 | Mercury free; Cayman National has fees |
| Crypto-custody setup | $0 – $20,000 | $10,000 | Squads free; Fireblocks if used |
| KYC + ID-verification provider Year-1 setup | $2,000 – $5,000 | $3,500 | Sumsub or Persona |
| **Subtotal — incorporation** | **$38,000 – $125,000** | **$81,500** | One-time |

### 1.2 Token legal opinion + token launch (one-time)

| Line item | Range | Midpoint | Notes |
|-----------|-------|----------|-------|
| Token legal opinion | $25,000 – $75,000 | $50,000 | Cooley / Fenwick / Davis Polk / Latham |
| Token disclaimer finalization | $5,000 – $15,000 | $10,000 | Crypto-securities counsel |
| Provider ToS amended for token economics | $10,000 – $20,000 | $15,000 | |
| MiCA white-paper (if EU launch in Year-1) | $20,000 – $40,000 | $0 | Defer to Year 2 if EU not Phase 1 |
| Regulator outreach / no-action requests | $5,000 – $25,000 | $10,000 | Variable; Cayman / US / EU as relevant |
| **Subtotal — token launch** | **$45,000 – $175,000** | **$85,000** | One-time |

### 1.3 Smart-contract audit (one-time)

| Line item | Range | Midpoint | Notes |
|-----------|-------|----------|-------|
| Halborn / OtterSec / Trail of Bits audit | $40,000 – $120,000 | $80,000 | Anchor programs for `$GRID` token + treasury + staking |
| Bug bounty post-audit | $10,000 – $50,000 | $25,000 | Immunefi / hosted bounty |
| **Subtotal — audit** | **$50,000 – $170,000** | **$105,000** | One-time + bounty pool |

### 1.4 Phase 1 operational legal review (one-time)

| Line item | Range | Midpoint | Notes |
|-----------|-------|----------|-------|
| Provider ToS counsel review | $1,500 – $3,000 | $2,000 | |
| Customer ToS counsel review | $1,500 – $3,000 | $2,000 | |
| AUP counsel review | $500 – $1,500 | $1,000 | |
| DPA counsel review (GDPR specialist) | $1,500 – $3,500 | $2,500 | |
| Privacy Policy counsel review | $1,500 – $3,000 | $2,000 | |
| Incident Response counsel review | $500 – $1,000 | $700 | |
| **Subtotal — Phase 1 documents** | **$7,000 – $15,000** | **$10,200** | One-time |

### 1.5 Foundation recurring (Year 1, partial-year)

| Line item | Range | Midpoint | Notes |
|-----------|-------|----------|-------|
| Registered-agent fee (annual, pro-rated half-year if mid-year incorporation) | $1,500 – $4,000 | $2,500 | |
| Annual government fee (pro-rated) | $400 – $900 | $650 | |
| Independent director compensation (annual, partial) | $5,000 – $25,000 | $15,000 | 1–2 directors |
| Supervisor compensation (annual, partial) | $2,500 – $15,000 | $7,500 | 1–2 supervisors |
| Accounting / bookkeeping | $5,000 – $20,000 | $10,000 | Outsourced |
| Audit (if required) | $0 – $20,000 | $10,000 | Optional for utility token |
| Legal retainer (general counsel) | $10,000 – $25,000 | $15,000 | Cayman counsel monthly retainer |
| CIMA fees (if registered) | $0 – $25,000 | $5,000 | Likely exempt |
| Insurance (D&O for directors / supervisors) | $5,000 – $20,000 | $10,000 | Recommended |
| **Subtotal — Foundation recurring Year 1** | **$29,400 – $154,900** | **$75,650** | Annual |

### 1.6 Infrastructure paid by Foundation

| Line item | Range | Midpoint | Notes |
|-----------|-------|----------|-------|
| Solana mainnet RPC (Triton / Helius / Hellomoon) | $5,000 – $20,000 | $10,000 | Annual |
| Squads multisig (free protocol; tx fees only) | $500 – $2,000 | $1,000 | Solana tx fees |
| Treasury management software | $0 – $10,000 | $3,000 | Llama / Multis / similar |
| Domain / DNS / SSL | $500 – $2,000 | $1,000 | iogrid.org, foundation.iogrid.org |
| **Subtotal — infrastructure** | **$6,000 – $34,000** | **$15,000** | Annual |

### 1.7 Compliance + reporting

| Line item | Range | Midpoint | Notes |
|-----------|-------|----------|-------|
| Transparency-report drafting + counsel review | $5,000 – $15,000 | $8,000 | Annual (Phase 2 onward; may defer to Year 2) |
| FATCA / CRS reporting (TIA filing) | $2,000 – $8,000 | $4,000 | Annual |
| Sanctions screening service | $5,000 – $20,000 | $10,000 | Sumsub / Chainalysis Reactor |
| Chainalysis / TRM Labs subscription | $10,000 – $50,000 | $20,000 | Annual; recommended Phase 2+ |
| **Subtotal — compliance recurring** | **$22,000 – $93,000** | **$42,000** | Annual |

### 1.8 Year-1 total

| Bucket | Midpoint (USD) |
|--------|----------------|
| Incorporation (one-time) | $81,500 |
| Token launch (one-time) | $85,000 |
| Smart-contract audit (one-time) | $105,000 |
| Phase 1 documents (one-time) | $10,200 |
| Foundation recurring (annual) | $75,650 |
| Infrastructure (annual) | $15,000 |
| Compliance recurring (annual) | $42,000 |
| **Year-1 total** | **$414,350** |
| **Year-1 range** | **$257,400 – $766,900** |
| **Year-1 with 25% reserve** | **~$518,000** |

The advertised Year-1 range of "$300,000 – $500,000" is the founder-comfortable midpoint
between the optimistic ($300K — tight cost discipline; defer MiCA, defer Chainalysis) and
the conservative ($500K — full compliance posture from Day 1 + reserve).

---

## 2. Year 2 — scale up + (optionally) MTL applications

**Range: $500,000 – $1,000,000. Midpoint: $750,000.**

Year 2 is when most growth-stage costs land: scaling the legal/compliance team, adding
jurisdictions, optionally pursuing Money-Transmitter Licenses (MTLs) if iogrid operates
in regulated payment flows, deepening sub-processor compliance.

### 2.1 Foundation recurring (full year)

| Line item | Range | Midpoint | Notes |
|-----------|-------|----------|-------|
| Registered-agent fee | $3,000 – $8,000 | $5,000 | |
| Annual government fee | $850 – $1,800 | $1,300 | |
| Independent director comp (2 directors, full year) | $20,000 – $50,000 | $35,000 | |
| Supervisor comp (2 supervisors, full year) | $10,000 – $30,000 | $20,000 | |
| Accounting / audit (mandatory at scale) | $15,000 – $50,000 | $30,000 | |
| Legal retainer (general counsel) | $20,000 – $50,000 | $35,000 | |
| CIMA fees (if registered) | $5,000 – $30,000 | $15,000 | |
| Insurance (D&O + crypto-specific) | $15,000 – $50,000 | $30,000 | |
| **Subtotal — Foundation recurring** | **$88,850 – $269,800** | **$171,300** | Annual |

### 2.2 Legal team build-out

| Line item | Range | Midpoint | Notes |
|-----------|-------|----------|-------|
| In-house General Counsel (full-time) | $200,000 – $350,000 | $275,000 | Salary + benefits; loaded cost |
| Compliance Officer | $120,000 – $200,000 | $160,000 | Loaded cost |
| Outside counsel ongoing (US / EU / UK / Cayman) | $100,000 – $250,000 | $175,000 | Spread across firms |
| Specific-matter outside counsel (subpoenas, regulator inquiries, etc.) | $25,000 – $100,000 | $50,000 | Variable |
| **Subtotal — legal team** | **$445,000 – $900,000** | **$660,000** | Annual |

*Note: in Year 2, founder may choose to defer hiring in-house GC + Compliance and keep
outside counsel at the higher end (~$300K outside-only). Total cost is similar but the
profile differs (less institutional knowledge in-house, more flexibility on scope).*

### 2.3 MTL / VASP applications (optional)

If iogrid operates fiat on-ramp / off-ramp directly (rather than via MoonPay / Stripe),
the Foundation may need Money Transmitter Licenses in up to 50 US states + state-
equivalents elsewhere.

| Line item | Range | Midpoint | Notes |
|-----------|-------|----------|-------|
| MTL counsel — initial scoping | $25,000 – $50,000 | $35,000 | Multi-state strategy |
| MTL state-by-state application + bond cost (5 priority states) | $50,000 – $200,000 | $100,000 | NY BitLicense alone is $50–100K |
| Full 50-state MTL portfolio (over 3 years) | $500,000 – $2,000,000 | — | Multi-year project |
| EU VASP / CASP under MiCA (if applicable) | $40,000 – $100,000 | $0 | Defer if no direct EU custody |
| UK FCA Registration | $25,000 – $75,000 | $0 | Defer if no direct UK custody |
| **Subtotal — MTL (if pursued)** | **$0 – $300,000** | **$50,000** | Optional |

If iogrid stays on the standard "no direct fiat custody; route through MoonPay / Stripe"
model (per `docs/TOKENOMICS.md` and `legal/dpa.md` Annex 3), MTLs are NOT needed and this
line item is $0. Stripe Connect handles the regulated-payment compliance externally.

### 2.4 Year-2 total

| Bucket | Midpoint (USD) |
|--------|----------------|
| Foundation recurring | $171,300 |
| Legal team build-out | $660,000 |
| MTL applications (deferred) | $0 (or $50K if pursued) |
| Infrastructure | $20,000 |
| Compliance recurring | $50,000 |
| Other (audits, miscellaneous) | $50,000 |
| **Year-2 total** | **$951,300** |
| **Year-2 range** | **$500,000 – $1,000,000** |
| **Year-2 with 25% reserve** | **~$1,190,000** |

Note: the midpoint ($951K) is slightly above the advertised range upper bound ($1M)
because the legal-team build-out is the dominant cost. Founder may choose to defer the
in-house hire and keep outside-counsel-only ($300K instead of $660K), which brings the
total to ~$590K — within the advertised range. The decision turns on operating-entity
maturity and the volume of legal work landing on the Foundation.

---

## 3. Year 3+ — full compliance team

**Range: $1,000,000+. Midpoint: $1,200,000+.**

By Year 3, iogrid is targeting Phase 3 (DAO migration; see `docs/TOKENOMICS.md` and
`legal/foundation/foundation-rules.md`). The Foundation has full compliance staffing,
regular regulator engagement, and substantial transparency-report cadence.

### 3.1 Foundation recurring (full year, scaled)

| Line item | Range | Midpoint | Notes |
|-----------|-------|----------|-------|
| Registered-agent + government fees | $4,000 – $10,000 | $7,000 | |
| Director + supervisor comp (3+2) | $40,000 – $100,000 | $70,000 | More independent representation |
| Accounting / audit (full audit) | $30,000 – $100,000 | $60,000 | |
| Legal retainer (general counsel) | $30,000 – $75,000 | $50,000 | |
| CIMA fees | $10,000 – $40,000 | $25,000 | |
| Insurance | $25,000 – $75,000 | $50,000 | Higher coverage |
| **Subtotal — Foundation recurring** | **$139,000 – $400,000** | **$262,000** | Annual |

### 3.2 Compliance team

| Line item | Range | Midpoint | Notes |
|-----------|-------|----------|-------|
| General Counsel | $250,000 – $400,000 | $325,000 | Loaded cost; senior |
| Deputy GC / Privacy Counsel | $180,000 – $280,000 | $230,000 | EU/UK lead |
| Chief Compliance Officer | $200,000 – $300,000 | $250,000 | |
| Compliance Analyst (2 FTE) | $200,000 – $300,000 | $250,000 | Sanctions, AML, abuse review |
| Outside counsel ongoing | $150,000 – $400,000 | $275,000 | Specialized matters |
| **Subtotal — compliance team** | **$980,000 – $1,680,000** | **$1,330,000** | Annual |

### 3.3 Transparency + audit cadence

| Line item | Range | Midpoint | Notes |
|-----------|-------|----------|-------|
| Quarterly transparency reports | $20,000 – $60,000 | $40,000 | Annual cost; ~4 reports/year |
| SOC 2 Type II audit | $40,000 – $80,000 | $60,000 | Annual |
| ISO 27001 certification (if pursued) | $30,000 – $80,000 | $50,000 | Initial + maintenance |
| Penetration testing | $20,000 – $60,000 | $40,000 | Bi-annual |
| **Subtotal — transparency + audit** | **$110,000 – $280,000** | **$190,000** | Annual |

### 3.4 Year-3+ total

| Bucket | Midpoint (USD) |
|--------|----------------|
| Foundation recurring | $262,000 |
| Compliance team | $1,330,000 |
| Transparency + audit | $190,000 |
| Infrastructure | $30,000 |
| MTLs / VASP (if pursued, ongoing) | $100,000 |
| Other / contingency | $100,000 |
| **Year-3+ total** | **$2,012,000** |
| **Year-3+ range** | **$1,000,000 – $2,500,000+** |
| **Year-3+ with 25% reserve** | **~$2,515,000** |

The advertised Year-3+ midpoint of "$1.2M+" assumes a leaner-than-midpoint staffing
model. Realistic full-build cost is closer to $2M/year. Founder may choose to stay at
the leaner profile if Foundation revenue (from token burn + Foundation-held treasury
yield) does not support the full-build cost.

---

## 4. Reserves + contingency

### 4.1 Operating reserve

Standard practice: maintain **6 months** of Foundation operating expenses as a liquid
reserve, held in USDC stablecoin or short-duration treasury bills. This is held SEPARATE
from the deflationary `$GRID` treasury (which is for grants and ecosystem development).

| Year | Operating reserve target (USD) |
|------|--------------------------------|
| Year 1 | $200,000 |
| Year 2 | $375,000 |
| Year 3+ | $1,000,000+ |

### 4.2 Defense fund

Per `legal/incident-response.md` §B.5, iogrid maintains a provider defense fund. This is
covered in the operating-entity budget, not the Foundation budget, but the Foundation
may co-fund. Recommended Year-1 reserve: **$50,000–$100,000**, scaling to **$500,000+** by
Year 3.

### 4.3 Litigation contingency

Even with best-practice anti-abuse filters and the indemnification chain in the Provider
ToS, litigation is realistic. Recommended reserve: **5% of annual operating budget**
through Year 3.

| Year | Litigation reserve target (USD) |
|------|---------------------------------|
| Year 1 | $20,000 |
| Year 2 | $37,500 |
| Year 3+ | $100,000+ |

### 4.4 Regulator-action contingency

If a regulator opens an enforcement action against iogrid, defense costs can run
$500K–$5M+ depending on the jurisdiction and the action. There is no realistic way to
fully reserve for this; the Foundation maintains a posture of cooperation + rapid
counsel engagement and treats this as catastrophic-risk-tolerated.

### 4.5 Total reserves recommendation

| Reserve type | Year 1 | Year 2 | Year 3+ |
|--------------|--------|--------|---------|
| Operating | $200,000 | $375,000 | $1,000,000 |
| Defense fund (Foundation co-fund) | $50,000 | $100,000 | $500,000 |
| Litigation | $20,000 | $37,500 | $100,000 |
| **Total reserves** | **$270,000** | **$512,500** | **$1,600,000** |

These reserves are STOCK (held continuously, replenished as drawn), not FLOW (annual
expense). Foundation Treasury is the source of replenishment.

---

## 5. Jurisdiction multiplier

If founder selects a non-Cayman jurisdiction per `legal/foundation/jurisdiction-
comparison.md`, multiply each Year-1 incorporation cost by:

| Jurisdiction | Multiplier (Year 1 setup) | Multiplier (annual recurring) |
|--------------|---------------------------|-------------------------------|
| Cayman (default) | 1.0x | 1.0x |
| BVI | 0.4x | 0.5x |
| Liechtenstein | 1.8x | 1.4x |
| Wyoming | 0.3x | 0.1x (but federal-tax exposure!) |
| Switzerland | 1.7x | 1.2x |

Note: Wyoming has a 21% federal corporate income tax that the other jurisdictions do
not, which adds a flow tax cost of 21% of Foundation revenue post-Year-1. This
materially changes the multi-year economics; the Year-1 setup multiplier of 0.3x is
misleading without considering the ongoing tax burden.

---

## 6. Funding sources

The Foundation's operating budget is funded by:

1. **Initial treasury allocation** — 10% of `$GRID` supply (100M tokens), held by the
   Foundation. At a hypothetical $0.05 initial price, this is $5M nominal — sufficient
   to cover Year 1 + Year 2 expected costs with reserve. Market price will fluctuate.
2. **Foundation-held treasury yield** — staking + DeFi yield on treasury assets (USDC
   reserve + `$GRID` staking) — recommend conservative posture (2–5% APY).
3. **Protocol revenue share** — per `docs/TOKENOMICS.md`, a percentage of provider /
   customer fee flow is directed to the Foundation. This grows with network adoption.
4. **Strategic raise proceeds** — 10% allocation to strategic investors (post 12-month
   cliff + 24-month vest) provides additional treasury depth.
5. **Grants from ecosystem partners** — Solana Foundation, Multicoin, Multicoin Capital,
   etc. may fund specific initiatives (security audits, MiCA white-paper, etc.).

### 6.1 Conservative funding model

If `$GRID` trades at $0.01 (heavily discounted scenario) and protocol revenue is delayed,
the Foundation holds 100M × $0.01 = $1M in nominal treasury value. This funds ~Year 1
only at the lean profile and the founder must plan for additional funding before Year 2.

### 6.2 Realistic funding model

If `$GRID` trades at $0.05–$0.20 (TOKENOMICS.md mid-case) and protocol revenue grows,
the Foundation has $5M–$20M in nominal treasury value plus growing protocol revenue.
This funds Year 1 + Year 2 comfortably with the planned reserves.

### 6.3 Aggressive funding model

If `$GRID` trades at $0.50+ (optimistic scenario consistent with Helium / Render
benchmarks at peak), the Foundation has $50M+ in nominal treasury value. This funds the
full multi-year compliance build-out without external fundraising.

---

## 7. Decision points the founder owns

1. **Initial budget commitment** — is the founder prepared to commit to $300K–$500K
   Year-1 spend (Section 1)?
2. **In-house vs. outside-counsel staffing** — Year 2 decision (Section 2.2): hire
   in-house GC + Compliance Officer, or stay outside-counsel-only at higher hourly?
3. **MTL pursuit** — Year 2 decision (Section 2.3): pursue Money-Transmitter Licenses
   if direct fiat custody is in scope, or stay on the MoonPay / Stripe-Connect
   pass-through model?
4. **MiCA compliance timing** — Year-1 or Year-2 EU launch? Affects MiCA white-paper
   timing (Section 1.2 vs. Section 2 deferred).
5. **Smart-contract audit scope** — which Anchor programs to audit ($GRID token only vs.
   token + treasury + staking + governance + Foundation council)?
6. **Operating reserve size** — 6-month default (Section 4.1) or larger (12-month)?
7. **Defense-fund co-funding** — Foundation co-funds or operating-entity solo-funds?
8. **Transparency-report cadence** — quarterly default (Section 3.3) or semi-annual?
9. **DAO-migration trigger** — when does the Foundation begin transitioning Squads-
   multisig signers to elected community representatives (per
   `legal/foundation/foundation-rules.md`)?

---

`[COUNSEL: full budget review recommended by counsel + chartered accountant. Cost
ranges are public-market estimates synthesized from open-source comparable engagements
and may not reflect current market for the specific firms iogrid engages. Tax-treatment
assumptions (Cayman 0% income tax, etc.) must be verified by Cayman accountant for
the specific entity structure. The MTL section (Section 2.3) is jurisdiction- and
business-model-specific; defer to MTL counsel before committing.]`

*End of Foundation operating budget.*
