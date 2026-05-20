# $GRID audit — budget

Cost-and-timing envelope for the pre-mainnet audit engagement. Numbers are 2026-current
based on direct quotes received from each firm in earlier scoping conversations, public
case studies, and aggregated industry benchmarks.

---

## Headline number

**Target spend: $30–80K** for the primary audit (one firm, all 5 programs, one fix-up
round), plus an **optional $10–20K** for a Neodyme spot review on `vesting` + `emission`.

This sits inside the [`docs/BUSINESS-STRATEGY.md` §4 Currency model](../../docs/BUSINESS-STRATEGY.md#4-currency-model--grid--fiat-hybrid) §"Token launch
sequence" budget of $30–80K and the [`legal/token-disclaimer.md`](../../legal/token-disclaimer.md)
implied audit-line item.

**Calendar:** 4–8 weeks from engagement letter signing to final report, depending on
firm queue + scope complexity. Plan to engage 12+ weeks before TGE to absorb fix-up
rounds and any second-pair-of-eyes review.

---

## Firm-by-firm quote ranges

Quotes below are based on conversations with each firm's sales team in Q1/Q2 2026 and
on published case studies. Final quote depends on the bundle hash, scope variance, and
firm's queue at submission time.

### OtterSec (primary recommendation)

- **Quote range:** $40–80K
- **Calendar:** 4–6 weeks
- **What's included:** full audit of all 5 programs, written report with severity-rated
  findings, one fix-up round, public report at iogrid's discretion
- **What's not included:** re-audit on subsequent upgrades ($5–15K per upgrade), public
  threat model / blog post (extra $5K typically)
- **Why this firm:** deepest Anchor expertise in the ecosystem. Audited Phoenix, Squads,
  Jupiter, MarginFi, Drift, Tensor. The reports are detailed and the team responds quickly.
- **Risks:** longest queue. Typical lead time 4–8 weeks from outreach to kickoff.

### Halborn (strong fallback)

- **Quote range:** $30–60K
- **Calendar:** 4–6 weeks
- **What's included:** same as OtterSec, plus an optional "retainer" model where Halborn
  monitors the repo and re-audits diffs at a discount ($3–5K per upgrade).
- **What's not included:** less narrative writeup than OtterSec; some Anchor 0.31 gaps
  reported (but they upskill fast).
- **Why this firm:** multi-chain, so the team has seen ERC-20 / Solana cross-pattern
  bugs that pure-Solana firms might miss. Faster intake than OtterSec.
- **Risks:** Solana practice is smaller than OtterSec's; first audit on a complex Anchor
  workspace may take slightly longer than quoted.

### Neodyme (spot review)

- **Quote range:** $10–30K (for spot review on 2 programs)
- **Calendar:** 4–6 weeks
- **What's included:** deep dive on `vesting` + `emission`. Neodyme is known for
  primitive-level analysis — they discovered the original Wormhole exploit pattern.
- **What's not included:** holistic audit of all 5 programs (use as spot only, not
  primary).
- **Why this firm:** boutique with very deep Solana-runtime expertise. Excellent
  second-opinion on the math-heavy programs.
- **Risks:** longest queue of the three (boutique = limited capacity). Plan 6–10 weeks.

### Sec3 (cost alternative)

- **Quote range:** $20–40K
- **Calendar:** 3–5 weeks
- **What's included:** automated review via X-Ray tool + manual followup.
- **What's not included:** less narrative depth than OtterSec / Halborn. Auditor
  ecosystem regards Sec3 as a "preliminary audit" tool, useful before going to a top-tier
  firm — not a complete substitute.
- **Why this firm:** cheaper, faster. Good fit if budget tightens.
- **Risks:** tool-driven reports can miss subtle economic-logic bugs (the kind that hurt
  iogrid's vesting / emission math).

### Trail of Bits (premium tier)

- **Quote range:** $80–150K
- **Calendar:** 6–10 weeks
- **What's included:** premium audit with formal methods. Industry leader.
- **What's not included:** Solana is not their primary chain; they're best-in-class on
  EVM. Quote assumes they're available, which they often aren't for new projects.
- **Why this firm:** if budget permits and timeline allows, ToB is the "absolute
  best" choice. Most projects find it overkill for a 5-program Anchor workspace.
- **Risks:** highest cost; longest queue; least Solana-native.

---

## Total spend scenarios

### Scenario A — conservative ($40–60K total)

- OtterSec primary at $40K (low end if scope is clean)
- No second-pair-of-eyes
- One fix-up round included
- Public report

### Scenario B — recommended ($50–80K total)

- OtterSec primary at $50–60K
- Neodyme spot review on `vesting` + `emission` at $10–20K
- One fix-up round each
- Both reports published

### Scenario C — premium ($100–150K total)

- OtterSec primary at $60K
- Trail of Bits second-pair-of-eyes at $40K (on the same 5 programs)
- Bug bounty kickoff funding at Immunefi $10K base
- Total: $110K

We recommend Scenario B for TGE. Scenario C is appropriate if the strategic raise lands
above target and the Foundation wants belt-and-suspenders coverage before mainnet.

---

## Hidden costs (line items to budget separately)

| Item | Estimated cost | Notes |
|------|----------------|-------|
| Audit-bundle preparation (engineering time) | ~$5K opportunity cost | Make `audit-export` works; freeze the tree on `v0.1.0-audit` tag; clean clippy/CI |
| Fix-up engineering time | ~$10–20K (1 engineer × 2 weeks) | Critical/High findings require code changes + tests |
| Public report hosting (PDF + iogrid.org/security) | $0–$500 | Static page on docs-site |
| Immunefi bug-bounty escrow | $25K base bond + first month's payouts reserve | Immunefi requires upfront bond to publish a program |
| Re-audit on upgrade (post-launch) | $5–15K per upgrade | Retainer clause reduces this |

**Total all-in (Scenario B + hidden costs): ~$100K**. This is in the same envelope as the
Tokenomics doc's $30–80K headline figure plus the hidden-cost line items the headline
intentionally elides.

---

## Cost-of-NOT-auditing

If iogrid launches without an audit:

- **Token holders are uninsured.** Smart-contract risk has no mitigation, full exposure
  to Critical-severity bugs.
- **CEX listings reject.** Tier-1 CEXs (Binance, Coinbase, Kraken) require an audit
  report for any token listing review. Without one, listing is impossible.
- **Liability exposure.** Operating-entity (iogrid Inc.) and Foundation directors face
  personal-liability exposure for breach-of-duty if the project ships without standard-
  industry audit practice. Counsel will not sign off.
- **Reputational risk.** The Solana ecosystem (and crypto generally) treats "unaudited"
  as a yellow-flag warning. iogrid loses provider and customer trust.

A failed audit (or a publicly-disclosed Critical bug pre-mainnet) is recoverable. A
launched-unaudited-and-then-exploited project is usually not. The audit is a cost of
doing business in this market.

---

## Funding source

The Foundation's treasury (10% allocation = 100M $GRID at TGE) and the strategic raise
proceeds (per [`docs/BUSINESS-STRATEGY.md` §4.11 Strategic raise](../../docs/BUSINESS-STRATEGY.md#411-strategic-raise--terms-sketch)) fund the audit.
Specific line items:

- **From strategic raise (if pursued, ~$2M):** $50–100K earmarked for audit per
  Tokenomics §"Use of proceeds — Smart contract audit — $50K".
- **From Foundation treasury (at TGE):** reserve $50K for re-audit / Immunefi escrow.
- **From operating entity (Dynolabs Inc.) bridge funding (pre-Foundation):** $30–80K may
  flow as a pre-payment if the Foundation is not yet incorporated. The pre-payment is
  reimbursed by the Foundation post-incorporation. Counsel must structure this carefully
  to avoid any appearance of operating-entity control over post-audit code.

---

## Decision timing

The founder should make the firm-selection decision **at least 12 weeks before target
TGE**. Decision path:

1. Week 0 (decision deadline): firm shortlist → outreach to top 2 firms.
2. Week 1–2: receive quotes; check queues.
3. Week 3: sign engagement letter with primary firm. Optionally engage secondary firm.
4. Week 4–9: audit window.
5. Week 10: receive findings. iogrid fixes Critical/High.
6. Week 11: fix-up re-audit.
7. Week 12: final report.
8. TGE proceeds.

If iogrid is in week 0 of this clock NOW (2026-05), TGE targets late Q3 2026. This
aligns with the [`docs/BUSINESS-STRATEGY.md` §4 Currency model](../../docs/BUSINESS-STRATEGY.md#4-currency-model--grid--fiat-hybrid) §"Token launch sequence"
Month 6–9 mainnet TGE target.

---

## Pricing references (public)

- OtterSec public engagement page (no fixed price, custom quote): https://osec.io/audit
- Halborn public engagement page (no fixed price, custom quote): https://halborn.com/services/smart-contract-audit
- Neodyme: https://neodyme.io
- Sec3: https://sec3.dev/audit
- Solana Foundation curated auditor list: https://solana.com/developers/courses/program-security/audit-handover

**Final word:** these are quote *ranges* not fixed prices. The exact number depends on
scope variance, queue conditions, and negotiation. The numbers in this document are
operational planning estimates, not commitments. Founder should expect the actual quote
to land within ±20% of the midpoint.
