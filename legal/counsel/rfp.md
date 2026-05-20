# Counsel engagement RFP — iogrid Phase 1 + Foundation

**Status:** Draft v0.1 — pre-counsel-review (this RFP is itself drafted by founder/engineering and should be reviewed by the founder before transmission to counsel; it is not legal advice). **Use as the basis for outreach to candidate firms.**

**Related issues:**

- [#155](https://github.com/iogrid/iogrid/issues/155) — `legal/*` requires counsel review before Phase 1 launch
- [#103](https://github.com/iogrid/iogrid/issues/103) — Foundation incorporation (Cayman/BVI/Liechtenstein)
- [#122](https://github.com/iogrid/iogrid/issues/122) — Foundation incorporation: Cayman Foundation for `$GRID` treasury

**Purpose:** Provide a single coherent Request-for-Proposal that founder can attach to outreach emails to each candidate firm. The RFP is structured so that firms can return a fixed-scope fee quote without ambiguity.

---

## 1. Project overview

### 1.1 What iogrid is

iogrid is a peer-to-peer network where home PC and Mac owners share their idle compute and bandwidth with paying enterprise customers. Providers earn cash, free unlimited VPN, or charity contributions in return. Customers get residential-IP proxy, Docker compute, GPU inference, and macOS-native iOS builds at a fraction of cloud prices.

Mobile users (iOS / Android) are VPN consumers only — they never share resources, only consume them.

**Four workloads, one API:**

| Workload | Use cases | Price |
|----------|-----------|-------|
| Bandwidth proxy | E-commerce monitoring, SEO tracking, ad verification, social intelligence | $0.30–0.60 / GB |
| Docker compute | Batch processing, ML inference, internal jobs | $0.02–0.10 / CPU-hour |
| GPU / AI inference | LLM serving, vision models | $0.20–2.00 / GPU-hour |
| iOS builds | Xcode CI on real Mac hardware | $0.04 / minute |

**Three differentiators:**

1. Radical transparency — providers see every byte flowing through their IP, categorized, in real-time, in a dashboard. Block any category, destination, or customer with one click.
2. Multi-currency incentives — providers choose cash, free VPN, or charity.
3. First-class iOS-build workload — solving a real CI/CD pain point at half the GitHub Actions Mac-runner rate.

### 1.2 Token economics summary

Per `docs/TOKENOMICS.md` (full document is ~400 lines, included in the information packet):

- **Symbol:** `$GRID`
- **Network:** Solana (SPL Token-2022)
- **Initial supply:** 1,000,000,000 (1 billion); decimals: 9
- **Emission curve:** halving every 2 years; Year-1 emission 5% of supply
- **Burn rate target:** ≥2% of monthly revenue → market-buy → burn
- **Treasury custody:** 3-of-5 Squads Protocol multisig
- **Allocation:** 50% provider rewards (10y vest) / 15% team (4y/1y cliff) / 10% treasury / 10% strategic / 10% community / 5% DEX liquidity
- **Target TGE:** Q3 2026
- **Issuer structure:** Foundation (Cayman recommended per `docs/TOKENOMICS.md`); operating entity is Dynolabs Inc. (Delaware C-corp)
- **Utility:** provider payouts, customer payments (20% discount), governance, staking, fee-rebate

### 1.3 Legal posture, plain-language

iogrid sits in the same regulatory neighborhood as Bright Data, Honeygain, Pawns.app, IPRoyal, Salad. Public-data scraping is not per-se illegal in the US per *hiQ Labs v. LinkedIn* (CA9, 2017–2022, settled) and the recent *Meta v. Bright Data* ruling — but every node in the supply chain has potential exposure. iogrid's design choice is to take that exposure onto the operating entity / Foundation (deeper pockets, stronger filters, audit logs) and shield individual providers via mutual indemnification and a defense-fund mechanism.

The full risk landscape (William Weber Austria case, Moritz Bartl Tor-exit raids, Nolan King FBI case, etc.) is documented in `docs/LEGAL.md`.

### 1.4 Current state of legal artifacts

The repository contains **lawyer-ready drafts** of all Phase 1 legal documents:

| Document | Path | Length | Counsel markers |
|----------|------|--------|-----------------|
| Provider ToS | `legal/provider-tos.md` | ~43 KB | ~25 |
| Customer ToS | `legal/customer-tos.md` | ~25 KB | ~20 |
| Acceptable Use Policy | `legal/aup.md` | ~22 KB | ~15 |
| Data Processing Agreement | `legal/dpa.md` | ~24 KB | ~15 |
| Privacy Policy | `legal/privacy-policy.md` | ~19 KB | ~15 |
| Token Disclaimer | `legal/token-disclaimer.md` | ~19 KB | ~10 |
| Incident Response | `legal/incident-response.md` | ~15 KB | ~12 |
| Cayman Foundation setup | `legal/foundation/cayman-setup.md` | ~17 KB | ~3 |
| Foundation Rules template | `legal/foundation/foundation-rules.md` | ~15 KB | ~2 |

Every document carries `[COUNSEL: ...]` markers (~144 total across the set) flagging specific points where qualified counsel must (a) make a legal judgement, (b) customize for the finalized operating-entity jurisdiction, or (c) review for enforceability in each launch jurisdiction.

A deduplicated, file:line-referenced list of every counsel marker is at `legal/counsel/issue-list.md`.

---

## 2. Engagement scope

We are seeking proposals for a **two-phase engagement**. Firms may bid on Phase 1, Phase 2, or both. Phase 1 is gating Phase 1 product launch (target Q2 2026); Phase 2 is gating Token Generation Event (target Q3 2026).

### 2.1 Phase 1 — operational document review

Scope: review, redraft, and finalize the non-token Phase 1 legal documents listed in Section 1.4 (Provider ToS, Customer ToS, AUP, DPA, Privacy Policy, Incident Response).

Deliverables (numbered):

1. Engagement letter signed within 5 business days of selection.
2. Initial scoping call (60 minutes) within 10 business days of engagement, attended by partner + senior associate, founder + 1 engineer.
3. Markup of each Phase 1 document with redlines, comments, and resolved `[COUNSEL: ...]` markers (delivered in 2 waves: ToS+AUP first, DPA+Privacy+Incident second).
4. Jurisdiction confirmation memo: operating-entity jurisdiction, governing-law clause across documents, restricted-jurisdiction list.
5. NCMEC reporting-entity status memo (per AUP §2.1 and Incident Response §B.3).
6. Sub-processor list + DPA execution checklist (per DPA Annex 3).
7. Article 27 EU/UK representative appointment recommendation.
8. SCC Module-selection memo + docking-provision drafting (per DPA §5.1.1).
9. Transfer Impact Assessment (TIA) per Schrems II / EDPB Recommendations 01/2020 (per DPA §5.3).
10. Final clean copies of each document, ready to publish.
11. Revision-history sign-off entry (counsel name, firm, date) for `legal/README.md`.

Timeline: **4–6 weeks** from engagement letter to final clean copies.

### 2.2 Phase 2 — Foundation + token legal opinion

Scope: incorporate the iogrid Foundation (Cayman recommended; see Section 4 for alternatives) AND obtain a token legal opinion covering US securities-law analysis (Howey), EU MiCA classification, UK FCA financial-promotions, and a defensible utility-token characterization for the `$GRID` token.

Deliverables (numbered):

1. Engagement letter signed within 10 business days of selection.
2. Foundation incorporation per `legal/foundation/cayman-setup.md` operational checklist (or jurisdiction-equivalent if Founder selects alternative):
   - KYC of initial directors / supervisors
   - Constitution drafting (Memorandum, Articles, Foundation Rules)
   - Filing with the Cayman Islands Registrar of Companies (or selected-jurisdiction registry)
   - Post-incorporation registrations (CIMA assessment, Tax Information Authority, Beneficial-ownership)
   - Banking introduction (Mercury / Brex / Cayman National)
3. Tech-license agreement between Dynolabs Inc. and the Foundation.
4. Service agreement between Dynolabs Inc. and the Foundation for ongoing operations.
5. Token legal opinion covering:
   - US securities classification (Howey-test analysis; Reg S geo-block enforceability)
   - EU MiCA classification (utility token vs. asset-referenced token vs. e-money token)
   - UK FCA financial-promotions regime applicability
   - Sanctions / OFAC compliance program review
   - Cayman / selected-jurisdiction local-law compliance
6. Token disclaimer (`legal/token-disclaimer.md`) finalization with counsel-resolved markers.
7. Restricted-jurisdiction list with geo-block enforcement recommendations.
8. Token-2022 freeze-authority retention policy + disclosure language.
9. MiCA white-paper (if EU launch in scope).
10. Strategic-raise documentation (Reg D / Reg S equivalents) if applicable.
11. Final clean copies of all token-related documents; Foundation operational; multisig live.

Timeline: **8–12 weeks** from engagement letter to operational Foundation with token-launch sign-off.

### 2.3 Exclusions and assumptions

- iogrid's product engineering, anti-abuse filters, audit-log retention, and KYC pipeline are operational and counsel may rely on existing technical implementation.
- Founder retains decision rights over: Foundation jurisdiction (counsel may recommend), restricted-jurisdiction list (counsel may recommend), tier-name and pricing (commercial decisions).
- Counsel may not commit to securities-law positions for jurisdictions outside their licensure; sub-counsel engagement in additional jurisdictions is acceptable and may be billed separately.
- The repository is public. Counsel deliverables may be committed to the repository as `legal/*.md` documents (markdown source).

---

## 3. Budget ranges

These are public-market reference points based on the project's own internal cost analysis in `docs/LEGAL.md`, `docs/TOKENOMICS.md`, and `legal/foundation/cayman-setup.md`. Firms are encouraged to bid above or below as appropriate; cost is one factor among several in the selection criteria (Section 5).

### 3.1 Phase 1 budget envelope

- **Total: $5,000 – $15,000 USD (fixed-fee preferred)**
- Reference points:
  - Provider ToS review + redraft: $1,500 – $3,000
  - Customer ToS review + redraft: $1,500 – $3,000
  - AUP review: $500 – $1,500 (typically rolls into ToS engagement)
  - DPA review (GDPR specialist): $1,500 – $3,500
  - Privacy Policy review: $1,500 – $3,000
  - Incident Response review: $500 – $1,000

### 3.2 Phase 2 budget envelope

- **Total: $80,000 – $200,000 USD**
- Reference points:
  - Foundation structuring (Cayman / equivalent): $30,000 – $80,000
  - Annual Foundation maintenance Year 1: $40,000 – $120,000 (recurring; see Section 4)
  - Token legal opinion: $25,000 – $75,000
  - Token disclaimer finalization: $5,000 – $15,000
  - Provider ToS amended for token economics: $10,000 – $20,000
  - MiCA white-paper (if EU launch): $20,000 – $40,000

Cost ranges are public-market estimates from open-source comparable engagements; they are not quotes. Counsel may bid higher or lower with justification.

`[COUNSEL: review the cost ranges above before transmission; they were assembled from public market data and Solana-ecosystem-foundation precedents and may not reflect current market.]`

---

## 4. Foundation jurisdiction — open decision

Founder has not yet selected a Foundation jurisdiction. Detailed comparison is at `legal/foundation/jurisdiction-comparison.md`. Summary:

| Jurisdiction | Default? | Setup | Annual | Time | Notable |
|--------------|----------|-------|--------|------|---------|
| Cayman Islands Foundation Company | YES (per docs) | $30–80K | $40–120K | 8–12 weeks | Wormhole, Aptos, Sui, Pyth precedent |
| BVI Limited (non-profit) | No | $10–20K | $20–40K | 4–6 weeks | Cheapest; lower prestige at CEX listing |
| Liechtenstein TVTG | No | $80–150K | $60–120K | ~6 months | EU-friendly, MiCA-compatible, formal token-issuance license |
| Wyoming DAO LLC | No (explicitly NOT recommended in TOKENOMICS) | $10–25K | $5–15K | 2–4 weeks | UNTESTED for token issuance, high SEC-action risk |
| Switzerland (Zug Crypto Valley) | No | $80–120K | $50–100K | 8–12 weeks | Strong reputation; established crypto-valley legal ecosystem |

Bidders may propose any jurisdiction, including ones not listed; if so, please explain the rationale.

---

## 5. Firm shortlist

The following firms are the candidate set for outreach. Bidders not on this list may still respond; we welcome introductions to additional crypto-tech counsel.

### 5.1 US firms — crypto-securities focus

| Firm | Office locations | Crypto track record | Notes |
|------|------------------|---------------------|-------|
| Cooley | SF, Palo Alto, NY, DC, London | Coinbase, Anchorage, Polygon, others | US large, expensive; partner-heavy crypto bench |
| Fenwick | SF, Mountain View, Seattle | Robinhood Crypto, Brave, others | Strong tech-startup brand; crypto experience growing |
| Davis Polk | NY, London, Hong Kong | Bitwise, others; top-tier crypto-securities | Highest end of price range; SEC-defense capable |
| Latham & Watkins | NY, SF, London, Hong Kong | Coinbase, Circle, others; top-tier crypto | Premium rates; deep crypto / international bench |

### 5.2 Cayman / BVI specialists

| Firm | Office locations | Crypto track record | Notes |
|------|------------------|---------------------|-------|
| Walkers | Cayman, BVI, London, Singapore, Hong Kong | Aptos, Wormhole, Solana Foundation | Recommended primary in `legal/foundation/cayman-setup.md` |
| Maples Group | Cayman, BVI, Dublin, Singapore, Hong Kong | Sui, multiple Solana foundations | Recommended fallback in `legal/foundation/cayman-setup.md` |
| Conyers | Cayman, BVI, Bermuda, Singapore | Pyth Foundation | Strong regulatory + banking-introduction practice |
| Carey Olsen | Cayman, BVI, Jersey, Guernsey | Multiple crypto funds | Competitive on price; offshore-funds-heavy practice |

### 5.3 EU / Switzerland

| Firm | Office locations | Crypto track record | Notes |
|------|------------------|---------------------|-------|
| Bird & Bird | London, Brussels, Frankfurt, others | MiCA implementation work | UK / EU MiCA bench; tech-sector focus |
| Schellenberg Wittmer | Zurich, Geneva | Swiss FADP + Liechtenstein TVTG | If Foundation jurisdiction = Switzerland / Liechtenstein |

### 5.4 Notes on the shortlist

- The shortlist is informed by `docs/LEGAL.md` (which references Cooley / Fenwick / Davis Polk / Latham for US crypto-securities counsel) and `legal/foundation/cayman-setup.md` (which recommends Walkers / Maples for Cayman).
- The list is not exhaustive. Boutique firms (Sullivan & Worcester crypto group, Anderson Kill, Reed Smith crypto group, Bryan Cave Leighton Paisner, etc.) are welcome to bid.
- For Phase 1 alone (operational documents), a tech-transactions partner at any of the above firms — or at a regional firm with strong SaaS / DPA experience — may be appropriate. The full crypto-securities bench is only required for Phase 2.

`[COUNSEL: review shortlist for completeness and for any firm with active conflicts (e.g., representing a direct competitor — Bright Data, IPRoyal, Honeygain, Salad, Pawns.app, etc.). Add or remove as appropriate.]`

---

## 6. Selection criteria matrix

Bidders will be scored against the following criteria. Total: 100 points.

| Criterion | Weight | Description |
|-----------|--------|-------------|
| Jurisdiction expertise | 20 | Demonstrated practice in each relevant jurisdiction (US, EU, UK, Cayman + applicable others). For Phase 2: crypto-securities-specific bench. |
| Crypto experience | 25 | Solana-ecosystem foundation precedent preferred; SPL Token-2022 specifics a plus; Howey-test analysis precedent; MiCA classification precedent. |
| Response time | 15 | Time from outreach email to first call ≤ 5 business days; engagement-letter turnaround ≤ 10 business days; Phase 1 deliverable turnaround per timeline (Section 2). |
| Hourly rate / fixed-fee | 20 | Bid relative to budget envelope (Section 3); fixed-fee strongly preferred over hourly for Phase 1. |
| References | 10 | Three (3) live client references with comparable engagement (provider/customer ToS + Foundation + token launch); founder will conduct reference calls. |
| Conflict-of-interest disclosure | 5 | Clean disclosure (Section 7) of any current or recent representation of competitors, sub-processors, target-platform plaintiffs (Meta v. Bright Data, X v. Bright Data, hiQ Labs etc.). |
| Partner-staff ratio + named partner | 5 | Named lead partner committed for the duration of the engagement; clear staffing model. |

`[COUNSEL: review weighting; founder may adjust before transmission.]`

---

## 7. Evaluation rubric

After receiving proposals, the founder will:

1. **Score** each bid against the Section 6 matrix.
2. **Interview** the top 3 bidders (30-minute partner + 30-minute team intro).
3. **Reference-check** the top 2 bidders (3 references each).
4. **Negotiate** engagement-letter terms with the selected firm (see `legal/counsel/engagement-checklist.md` §"Engagement letter terms to negotiate").
5. **Sign** the engagement letter; commit the executed copy (redacted as needed) to the project's private records (NOT the public repo).

Decision targets:

- Phase 1: counsel selected within 14 calendar days of RFP transmission.
- Phase 2: counsel selected within 28 calendar days (may overlap with Phase 1).

---

## 8. Submission instructions

Proposals should include:

1. Firm overview + named lead partner CV (1–2 pages).
2. Crypto-engagement case studies (3 minimum; Solana-ecosystem-Foundation experience preferred for Phase 2 bidders).
3. Phase 1 fixed-fee bid OR Phase 2 fixed-fee bid (or both).
4. Estimated timeline for each phase bid on.
5. Conflict-of-interest disclosure (per Section 6 row 6 and `legal/counsel/engagement-checklist.md`).
6. Three (3) client references with name, title, firm, email; iogrid founder will contact directly.
7. Engagement-letter draft terms (rate caps, scope-creep mechanism, IP ownership of work product — see `legal/counsel/engagement-checklist.md` for the founder-preferred terms).

**Submit to:** *[COUNSEL: insert founder contact email, e.g., legal-rfp@iogrid.org]*.

**Submission deadline:** 21 calendar days from RFP transmission.

---

## 9. Information packet (sent on engagement / NDA)

Once a firm is selected, iogrid will provide:

- This RFP (public).
- Project README — [`README.md`](../../README.md) (public).
- Architecture overview — [`docs/ARCHITECTURE.md`](../../docs/ARCHITECTURE.md) (public).
- Legal scaffolding spec — [`docs/BUSINESS-STRATEGY.md` §6 Legal risk landscape](../../docs/BUSINESS-STRATEGY.md#6-legal-risk-landscape--mitigation) (public).
- Tokenomics — [`docs/BUSINESS-STRATEGY.md` §4 Currency model](../../docs/BUSINESS-STRATEGY.md#4-currency-model--grid--fiat-hybrid) (public).
- All `legal/*.md` drafts (public, in repo).
- Cayman Foundation operational checklist — [`legal/foundation/cayman-setup.md`](../foundation/cayman-setup.md) (public).
- Counsel-marker dedup list — [`legal/counsel/issue-list.md`](./issue-list.md) (public).
- Operating-entity cap table + KYC pack (private, on NDA).
- Existing vendor contracts (Stripe, Sumsub, Persona, AWS, etc.) (private, on NDA).
- Founder + key contributor KYC pack (private, on NDA).

Repository URL: <https://github.com/iogrid/iogrid>

---

## 10. Outreach template

A standard outreach email is below. Founder personalizes the salutation and partner-specific paragraph; the rest is uniform across firms.

```text
Subject: iogrid — Phase 1 + Foundation counsel engagement RFP

Dear [Partner name],

iogrid is a distributed-compute and bandwidth-mesh project (think Bright Data
crossed with Honeygain crossed with Salad, with a Solana-native $GRID utility
token aligning provider and customer incentives) preparing for Phase 1 launch
in Q2 2026 and Token Generation Event in Q3 2026. Detailed background:

  * README:        https://github.com/iogrid/iogrid/blob/main/README.md
  * Whitepaper:    https://github.com/iogrid/iogrid/blob/main/docs/whitepaper.md
  * Tokenomics:    https://github.com/iogrid/iogrid/blob/main/docs/TOKENOMICS.md
  * Legal RFP:     https://github.com/iogrid/iogrid/blob/main/legal/counsel/rfp.md
  * Draft legal:   https://github.com/iogrid/iogrid/tree/main/legal

We are seeking proposals for a two-phase counsel engagement:

  Phase 1 (~$5–15K, 4–6 weeks): review and finalize operational documents
    (Provider ToS, Customer ToS, AUP, DPA, Privacy Policy, Incident Response).
    All drafts are lawyer-ready in markdown with ~144 [COUNSEL:...] markers
    flagging where counsel judgement is needed.

  Phase 2 (~$80–200K, 8–12 weeks): Cayman Foundation incorporation +
    token legal opinion (US Howey + EU MiCA + UK FCA + Cayman) +
    Foundation operational setup (banking, custody, multisig).

Bidders may bid on Phase 1, Phase 2, or both. Full RFP, deliverables list, and
evaluation rubric are at the legal/counsel/rfp.md link above. Submission
deadline: 21 calendar days from receipt.

[Partner-specific paragraph — e.g., "We were particularly impressed by your
work on the Aptos Foundation; the iogrid Foundation structure is closely
analogous." OR "Your firm's MiCA implementation guidance has been a key
reference for our EU-launch planning."]

Would you have 30 minutes for an introductory call in the next 2 weeks?

Best regards,
[Founder name]
[Title]
iogrid / Dynolabs Inc.
[email]
```

---

## 11. Open items for founder review before transmission

Before this RFP is sent to any firm, the founder must:

1. **Confirm budget envelopes** (Section 3) match what the founder is prepared to commit.
2. **Confirm shortlist** (Section 5) is complete and that no firm on it has a known conflict.
3. **Confirm contact email** in Section 8 and complete any *[COUNSEL: insert]* placeholders.
4. **Confirm submission deadline** (Section 8 — 21 days is the default).
5. **Confirm public repository URL** is the canonical one (Section 9).
6. **Decide on partner-personalization paragraph** for each outreach (Section 10).

---

`[COUNSEL: full RFP review recommended before transmission. The RFP itself is a procurement document, not a legal opinion; nevertheless counsel may wish to remove or refine specific representations (e.g., the legal-posture description in Section 1.3; the budget envelopes in Section 3) before it goes out to competing firms. The RFP is intentionally drafted to be sent as-is; founder may strip the [COUNSEL: ...] markers from the version that ships to firms.]`

*End of counsel-engagement RFP.*
