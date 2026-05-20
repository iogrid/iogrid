# Foundation jurisdiction — detailed comparison

**Status:** Draft v0.1 — pre-counsel-review. **Not legal advice.** **This is an operational
comparison brief for the founder. Final jurisdiction selection must be made with qualified
counsel; the recommendations below are reference points, not opinions.**

**Related issues:**

- [#103](https://github.com/iogrid/iogrid/issues/103) — Foundation jurisdiction selection
- [#122](https://github.com/iogrid/iogrid/issues/122) — Foundation incorporation for `$GRID` treasury
- [#155](https://github.com/iogrid/iogrid/issues/155) — `legal/*` requires counsel review

**Default per existing docs:** **Cayman Islands Foundation Company** (see
[`docs/BUSINESS-STRATEGY.md` §4 Currency model](../../docs/BUSINESS-STRATEGY.md#4-currency-model--grid--fiat-hybrid) §"Legal risk + mitigation strategy" and
[`legal/foundation/cayman-setup.md`](./cayman-setup.md)).

---

## 0. How to read this document

This document compares five Foundation jurisdictions on the dimensions that matter for an
iogrid-style project: setup cost, ongoing cost, tax treatment, regulatory burden,
crypto-specific framework, reputation/perception, and time-to-incorporate.

Costs are public-market estimates synthesized from open-source comparable engagements
(Helium, Aptos, Sui, Pyth, Wormhole, Solana Foundation, Magic Eden, etc.). They are
NOT quotes. Counsel may quote higher or lower based on the specifics of the engagement.

The five jurisdictions:

1. **Cayman Islands** (default; default per existing docs)
2. **British Virgin Islands (BVI)**
3. **Liechtenstein** (TVTG)
4. **Wyoming DAO LLC** (US-domestic; explicitly NOT recommended in TOKENOMICS.md)
5. **Switzerland** (Zug Crypto Valley)

A sixth jurisdiction — **Panama** — is sometimes proposed for crypto foundations but has
inconsistent enforcement track records and is NOT covered here; founder may ask counsel
about it if relevant.

---

## 1. Cayman Islands Foundation Company

### 1.1 Snapshot

- **Statute:** Foundation Companies Act 2017 (as amended)
- **Form:** Foundation Company (hybrid corporation/foundation; no shareholders)
- **Regulator:** Cayman Islands Monetary Authority (CIMA) for VASP-licensed activities;
  Registrar of Companies for general corporate filings
- **Notable precedents:** Wormhole, Aptos, Sui, Pyth, Helium, Magic Eden, Jupiter, Solana
  Foundation itself

### 1.2 Setup cost

- **Counsel fees:** $30,000 – $80,000 (Walkers / Maples / Conyers / Ogier — see
  `legal/foundation/cayman-setup.md` §1 for partner names)
- **Government + registry fees:** $1,000 – $3,000 (one-time)
- **Independent directors / supervisors signing fees:** $5,000 – $15,000
- **Banking setup:** $0 – $2,000 (Mercury free; Cayman National has fees)
- **Crypto-custody setup:** $0 – $20,000 (Squads free; Fireblocks / Anchorage if used)
- **Total Year-1:** $76,000 – $240,000; midpoint ~$155,000

### 1.3 Annual maintenance cost

- **Registered-agent fee:** $3,000 – $8,000/year
- **Annual government fee:** ~$850 – $1,800/year (CI$700 – CI$1,500)
- **Independent director compensation:** $10,000 – $25,000/year per director
- **Supervisor compensation:** $5,000 – $15,000/year per supervisor
- **Accounting / audit:** $5,000 – $20,000/year
- **Legal retainer:** $10,000 – $25,000/year
- **CIMA fees (if registered):** $5,000 – $25,000/year
- **Total annual:** $40,000 – $120,000/year; midpoint ~$60,000 – $80,000/year

### 1.4 Tax treatment

- **Corporate income tax:** 0%
- **Capital gains tax:** 0%
- **Withholding tax:** 0%
- **VAT/GST:** none
- **Tax-Information-Authority (TIA) registration:** required (FATCA / CRS reporting)
- **Economic Substance:** Foundation Companies that are NOT engaged in "relevant
  activities" (banking, insurance, fund management, etc.) have minimal Economic Substance
  obligations. Token issuance is generally NOT a "relevant activity" — but counsel must
  confirm based on the specific service offering.

### 1.5 Regulatory burden

- **Beneficial-ownership register filing:** required.
- **CIMA registration:** required ONLY for VASP-licensed activities (custody, exchange,
  transfer-of-VAs-for-others-as-business). A pure utility-token issuance from a non-
  custodial network is typically NOT a VASP activity. iogrid's design is non-custodial
  (providers hold their own keys), so CIMA exemption is likely. **Counsel confirms.**
- **Audit:** financial audit may be required if the Foundation conducts "regulated
  activity" or exceeds size thresholds. For a pure utility-token issuer, audit is often
  optional but recommended.
- **Annual return:** required.

### 1.6 Crypto-specific framework

- Foundation Companies Act 2017 was specifically designed to accommodate token issuers
  and DAOs.
- Cayman has the deepest crypto-foundation legal precedent in the offshore world (see
  precedents in Section 1.1).
- Anchorage Cayman / Fireblocks Cayman serve as institutional custody options.
- Cayman counsel firms (Walkers, Maples, Conyers, Ogier) all have dedicated crypto
  practice groups.

### 1.7 Reputation / perception

- **Pro:** standard / expected for Solana-ecosystem and major crypto projects; CEX
  listings (Binance, Coinbase, Kraken) accept Cayman as the issuer jurisdiction without
  friction; auditors (Halborn, OtterSec, Trail of Bits) treat Cayman as the default.
- **Con:** "offshore" framing remains a populist talking point; some institutional
  investors prefer onshore jurisdictions. Generally NOT a blocker for crypto-native
  capital but may slow conversations with traditional VCs.

### 1.8 Time to incorporate

- **8–12 weeks** from counsel engagement to operational Foundation (per
  `legal/foundation/cayman-setup.md` Step 6 timeline).
- Filing itself takes 5–10 business days post-constitution-drafting.

### 1.9 Best for

iogrid's default profile: Solana-native utility token, non-custodial network, target
TGE Q3 2026, $80K–$200K Phase 2 budget envelope, US founder, EU launch later.

---

## 2. British Virgin Islands (BVI) Limited

### 2.1 Snapshot

- **Statute:** BVI Business Companies Act 2004 (as amended)
- **Form:** BVI Limited (limited liability company); occasionally a Purpose Trust
  arrangement is used for token-issuer structures
- **Regulator:** BVI Financial Services Commission (FSC) for VASP-licensed activities
- **Notable precedents:** smaller-cap crypto projects, NFT projects, several DeFi
  protocols — fewer marquee Solana names than Cayman

### 2.2 Setup cost

- **Counsel fees:** $10,000 – $25,000 (lower than Cayman; Conyers / Carey Olsen / Harneys
  have strong BVI books)
- **Government + registry fees:** $500 – $1,500 (one-time)
- **Independent director sign-on:** $5,000 – $10,000
- **Banking + custody:** similar to Cayman
- **Total Year-1:** $20,000 – $60,000; midpoint ~$35,000 – $45,000

### 2.3 Annual maintenance cost

- **Registered-agent fee:** $1,500 – $4,000/year (cheaper than Cayman)
- **Annual government fee:** $450 – $1,200/year
- **Director compensation:** $5,000 – $15,000/year
- **Accounting:** $3,000 – $10,000/year (audit often optional)
- **Legal retainer:** $5,000 – $15,000/year
- **Total annual:** $20,000 – $50,000/year; midpoint ~$30,000

### 2.4 Tax treatment

- **Corporate income tax:** 0%
- **Capital gains tax:** 0%
- **Withholding tax:** 0%
- **Economic Substance:** BVI imposes Economic Substance requirements for "relevant
  activity" companies. Token issuance is typically NOT a relevant activity but counsel
  must confirm. The BVI Economic Substance regime is somewhat stricter than Cayman's.

### 2.5 Regulatory burden

- **Beneficial-ownership register filing:** required.
- **VASP licensure:** BVI implemented a Virtual Assets Service Providers Act in 2022.
  Token issuers must register if they conduct "virtual-asset service" activities. A non-
  custodial utility token issuer typically does NOT require VASP registration but
  counsel must confirm. Penalties for non-registration are material.
- **Annual return:** required.

### 2.6 Crypto-specific framework

- BVI is a recognized crypto jurisdiction but less deep than Cayman.
- The 2022 VASP Act increased regulatory clarity but also increased the registration
  bar for crypto-related activities.
- Fewer marquee Solana-ecosystem precedents (most went Cayman).

### 2.7 Reputation / perception

- **Pro:** lower cost; same offshore tax treatment as Cayman; competent legal bench.
- **Con:** less recognized by major CEXes than Cayman; some auditors charge a friction
  premium for BVI-issued tokens. "Lower-prestige offshore" framing is a real perception
  issue at the institutional-investor level.

### 2.8 Time to incorporate

- **4–6 weeks** (faster than Cayman; less paperwork on the constitution side).

### 2.9 Best for

Smaller-budget projects ($20–60K all-in Year-1) where the cost saving outweighs the
reputational discount. Often used for sub-DAOs, smaller-cap projects, or as a sister
entity to a Cayman parent.

---

## 3. Liechtenstein (TVTG)

### 3.1 Snapshot

- **Statute:** Token and TT Service Provider Act 2020 (Token- und VT-Dienstleister-Gesetz,
  "TVTG")
- **Form:** Foundation (Stiftung) OR Anstalt OR AG, structured under the TVTG token
  issuance regime
- **Regulator:** Liechtenstein Financial Market Authority (FMA)
- **Notable precedents:** Bitcoin Suisse, several EU-launched tokens, Aktionariat — EU-
  native crypto projects

### 3.2 Setup cost

- **Counsel fees:** $80,000 – $150,000 (Naegele Attorneys, Bär & Karrer, Schellenberg
  Wittmer have Liechtenstein practices)
- **Government + registry fees:** $5,000 – $15,000 (TVTG registration is more involved)
- **Banking setup:** $2,000 – $10,000 (more banking friction than Cayman)
- **Foundation council compensation:** $20,000 – $40,000 (higher cost than Cayman
  directors)
- **Total Year-1:** $120,000 – $300,000; midpoint ~$180,000 – $220,000

### 3.3 Annual maintenance cost

- **Registered-agent / domiciliation fee:** $5,000 – $15,000/year
- **Annual government fee:** $2,000 – $5,000/year
- **Foundation council:** $20,000 – $40,000/year
- **Accounting / audit (mandatory under TVTG):** $15,000 – $40,000/year
- **Legal retainer:** $20,000 – $50,000/year
- **FMA fees:** $5,000 – $20,000/year
- **Total annual:** $60,000 – $120,000/year; midpoint ~$80,000 – $100,000

### 3.4 Tax treatment

- **Corporate income tax:** 12.5%
- **Capital gains tax:** generally 0% (treated as capital movement)
- **Withholding tax:** can apply to dividends; less relevant for foundations
- **VAT/MWST:** 7.7% Liechtenstein/Swiss VAT may apply to services
- **Tax holiday:** none specific to crypto

### 3.5 Regulatory burden

- **HIGH compared to Cayman/BVI.**
- **TVTG token registration:** every "token issuance" must be registered with the FMA,
  with a white-paper-equivalent disclosure document.
- **TT Service Provider license:** required for token issuers + custodians + exchanges
  (multiple license classes). Significant ongoing compliance burden.
- **AML/CFT:** Liechtenstein has full AML/CFT compliance regime, harmonized with EU.
- **Audit:** annual audit by approved auditor, mandatory.
- **Annual return:** comprehensive.

### 3.6 Crypto-specific framework

- TVTG is one of the most legally clear token regimes globally (predates MiCA but
  designed compatibly).
- Token issuers receive a formal "Token Container" registration — strong legal posture.
- MiCA-compatible: Liechtenstein is in the EEA, so MiCA applies and the TVTG was
  designed to interoperate with MiCA.
- Custodians and exchanges can obtain VASP / CASP licenses under TVTG.

### 3.7 Reputation / perception

- **Pro:** the strongest legal certainty of any major crypto jurisdiction. Excellent for
  EU institutional capital; strong reputation with regulators globally.
- **Con:** expensive; slowest to incorporate; smaller crypto-foundation talent pool.
  Less name-recognition than Cayman for US/Asia-Pacific investors.

### 3.8 Time to incorporate

- **~6 months** (TVTG registration + foundation council recruitment + FMA approval).
- This is the slowest of the five jurisdictions in this document.

### 3.9 Best for

EU-native crypto projects with deep budget ($200K+ Year-1) and a long runway, where
regulatory certainty in Europe is the primary objective. Sometimes used as an EU-facing
sister entity to a Cayman parent.

---

## 4. Wyoming DAO LLC

### 4.1 Snapshot

- **Statute:** Wyoming Decentralized Autonomous Organization Supplement (W.S. 17-31-101
  et seq., effective July 2021; updated 2024)
- **Form:** Limited Liability Company designated as a DAO
- **Regulator:** Wyoming Secretary of State; SEC / CFTC retain federal jurisdiction
- **Notable precedents:** CityDAO, several smaller DAOs; explicitly **NOT used by major
  Solana-ecosystem foundations**.

### 4.2 Setup cost

- **Counsel fees:** $10,000 – $25,000 (lowest of any option; many Wyoming firms can
  file)
- **Government + registry fees:** $200 – $500 (one-time)
- **Banking setup:** $0 – $2,000 (US-domestic; Mercury / Brex easy)
- **Total Year-1:** $15,000 – $50,000; midpoint ~$25,000 – $30,000

### 4.3 Annual maintenance cost

- **Registered-agent fee:** $200 – $500/year
- **Annual government fee:** $60/year (lowest of any option)
- **Accounting:** $3,000 – $10,000/year
- **Legal retainer:** $5,000 – $15,000/year
- **Total annual:** $5,000 – $15,000/year (lowest)

### 4.4 Tax treatment

- **Corporate income tax (federal):** 21% (US-domestic LLC is pass-through OR taxed as
  C-corp depending on election)
- **State income tax:** Wyoming has no state corporate income tax (0% Wyoming-side)
- **Effective rate:** 21% federal (pass-through to members OR C-corp election)
- **Tax holiday:** none

### 4.5 Regulatory burden

- **Wyoming DAO LLC** is a state-level designation. **Federal law still applies.**
- **SEC jurisdiction:** the SEC's reach over token issuance is unaffected by state-level
  structure. A US-domestic DAO LLC issuing a token is fully exposed to SEC enforcement
  under the Howey test.
- **CFTC jurisdiction:** similar.
- **FinCEN:** US-domestic issuers may have Money Services Business (MSB) registration
  obligations.
- **State Money Transmitter Licenses (MTLs):** potentially required in up to 50 states
  for any payment / custody activity.
- **Sanctions / OFAC:** US-domestic issuer is fully subject to US sanctions enforcement.

### 4.6 Crypto-specific framework

- The Wyoming DAO LLC structure provides corporate-form clarity for DAOs but does NOT
  shield from federal securities law.
- **TOKENOMICS.md explicitly identifies Wyoming DAO LLC as UNTESTED for token issuance
  and high-SEC-action risk.**

### 4.7 Reputation / perception

- **Pro:** US-domestic; transparent; novel structure aligns with DAO culture.
- **Con:** crypto-native counsel routinely advises against US-domestic token issuance
  for this product profile. CEXes are cautious about US-domestic issuers. Auditors
  flag this structure as high-risk for SEC exposure.

### 4.8 Time to incorporate

- **2–4 weeks** (fastest of any option).

### 4.9 Best for

DAO-native projects where US-domestic structure is a values commitment and the legal
risk is accepted. **NOT recommended for iogrid per TOKENOMICS.md.** Including in this
comparison only for completeness.

---

## 5. Switzerland (Zug Crypto Valley)

### 5.1 Snapshot

- **Statute:** Swiss Civil Code Article 80 (Foundation / Stiftung) + DLT Act 2021 (Federal
  Act on the Adaptation of Federal Law to Developments in Distributed Ledger Technology)
- **Form:** Stiftung (Foundation) OR Verein (Association) under Swiss Civil Code; token
  issuance under the DLT Act
- **Regulator:** Swiss Financial Market Supervisory Authority (FINMA)
- **Notable precedents:** Ethereum Foundation, Cardano Foundation, Tezos Foundation,
  Polkadot Foundation, Cosmos / Interchain Foundation, Solana Foundation Switzerland
  branch — many of the largest Layer-1 foundations are Swiss

### 5.2 Setup cost

- **Counsel fees:** $80,000 – $120,000 (Bär & Karrer, Walder Wyss, Schellenberg Wittmer,
  Niederer Kraft Frey have crypto practices)
- **Government + registry fees:** $5,000 – $10,000
- **Foundation council compensation:** $20,000 – $50,000
- **Banking setup:** $2,000 – $10,000 (Swiss banks: Sygnum, SEBA Bank — crypto-friendly)
- **Total Year-1:** $110,000 – $250,000; midpoint ~$160,000 – $180,000

### 5.3 Annual maintenance cost

- **Registered-agent / domiciliation:** $5,000 – $15,000/year
- **Annual government fee:** $1,000 – $3,000/year
- **Foundation council:** $20,000 – $50,000/year
- **Accounting / audit (mandatory if assets >CHF 10M):** $15,000 – $40,000/year
- **Legal retainer:** $20,000 – $50,000/year
- **FINMA fees:** variable; $5,000 – $30,000/year depending on regulated activity
- **Total annual:** $50,000 – $100,000/year; midpoint ~$70,000 – $80,000

### 5.4 Tax treatment

- **Corporate income tax (federal + cantonal Zug):** ~11.85% combined
- **Capital gains tax:** generally 0% (capital movement)
- **Withholding tax:** 35% on dividends (less relevant for non-profit foundations)
- **VAT/MWST:** 8.1% may apply
- **Tax exemption:** non-profit foundations may apply for tax exemption (charitable
  status); approval not automatic and not all token-issuer foundations qualify

### 5.5 Regulatory burden

- **HIGH but predictable.**
- **FINMA token classification:** FINMA has published clear guidance on payment
  tokens / utility tokens / asset tokens. iogrid's `$GRID` likely classifies as a
  utility token under FINMA's framework, but counsel must confirm.
- **VASP licensure:** may be required for custody / exchange / transfer-for-others.
- **AML/CFT:** full Swiss AML regime applies.
- **Audit:** mandatory above asset thresholds.
- **MiCA:** Switzerland is not in the EU, so MiCA does not directly apply, but
  passporting to EU requires either MiCA-compliant structure or a parallel EU entity.

### 5.6 Crypto-specific framework

- DLT Act 2021 is one of the most comprehensive token-regime laws globally.
- "Crypto Valley" Zug has the deepest crypto-foundation talent pool in Europe.
- Sygnum and SEBA Bank provide regulated CHF/crypto banking.
- FINMA's no-action / discretionary-guidance process is well-established.

### 5.7 Reputation / perception

- **Pro:** the strongest combination of regulatory certainty + crypto-native talent
  pool + institutional credibility. Used by Ethereum, Cardano, Tezos, Polkadot, Cosmos,
  Solana Foundation Switzerland — i.e., almost every Layer-1 with serious institutional
  support.
- **Con:** expensive; mandatory foundation council adds ongoing cost; not as fast as
  Cayman for "just launch the token and go."

### 5.8 Time to incorporate

- **8–12 weeks** (similar to Cayman; faster than Liechtenstein).

### 5.9 Best for

Projects with strong institutional / EU positioning and budget headroom. Often chosen
when the project sees itself as a "Layer-1 foundation" or "ecosystem foundation" rather
than a "token issuer." For iogrid, this would be relevant if positioning shifted toward
"crypto-native infrastructure" rather than "B2B SaaS with a token."

---

## 6. Comparison matrix

### 6.1 At-a-glance

| Dimension | Cayman | BVI | Liechtenstein | Wyoming | Switzerland |
|-----------|--------|-----|---------------|---------|-------------|
| Default? | YES | No | No | NOT recommended | No |
| Setup cost (USD) | $30–80K | $10–25K | $80–150K | $10–25K | $80–120K |
| Annual cost (USD) | $40–120K | $20–50K | $60–120K | $5–15K | $50–100K |
| Total Year-1 (USD) | $76–240K | $20–60K | $120–300K | $15–50K | $110–250K |
| Tax (income) | 0% | 0% | 12.5% | 21% federal | ~11.85% federal+cantonal |
| Time | 8–12 wk | 4–6 wk | ~6 mo | 2–4 wk | 8–12 wk |
| Crypto regulatory clarity | High | Med-High | Very High | Federal-law-overlay | Very High |
| Solana-ecosystem precedent | Highest | Some | Few | None marquee | Some (older L1s) |
| CEX-listing friction | None | Some | Some (newer) | High | None |
| Audit-firm comfort | High | Med | High | Medium | High |
| EU MiCA fit | Separate entity needed | Separate entity needed | Native | NO | Separate entity helpful |

### 6.2 Decision scoring (illustrative)

If founder weights (a) cost = 25%, (b) regulatory clarity = 25%, (c) speed = 20%,
(d) crypto-precedent = 20%, (e) EU compatibility = 10%, an illustrative score:

| Jurisdiction | Cost (25%) | Regulatory (25%) | Speed (20%) | Precedent (20%) | EU (10%) | Total |
|--------------|------------|------------------|-------------|-----------------|----------|-------|
| Cayman | 7/10 | 8/10 | 7/10 | 10/10 | 5/10 | **7.55** |
| BVI | 9/10 | 6/10 | 9/10 | 6/10 | 4/10 | **7.05** |
| Liechtenstein | 4/10 | 10/10 | 3/10 | 5/10 | 10/10 | **6.10** |
| Wyoming | 9/10 | 3/10 | 10/10 | 2/10 | 2/10 | **5.45** |
| Switzerland | 4/10 | 10/10 | 7/10 | 8/10 | 8/10 | **7.30** |

Scoring is illustrative only — the founder may weight dimensions differently. Counsel
should confirm scoring with their own judgement.

`[COUNSEL: review the scoring rubric; weights are founder-specific and counsel may
disagree with individual scores. The matrix is a structured-thinking aid, not a
mathematical answer.]`

---

## 7. Recommendation matrix

### 7.1 Default recommendation: Cayman Islands

For iogrid's profile (Solana-native utility token, US founder, B2B SaaS revenue, target
Q3 2026 TGE, $80–200K Phase 2 budget envelope, EU launch as secondary priority), the
**Cayman Foundation Company is the recommended default**, consistent with
`docs/TOKENOMICS.md` and `legal/foundation/cayman-setup.md`.

Rationale:

1. Deepest crypto-foundation precedent in the offshore world (Wormhole, Aptos, Sui,
   Pyth, Helium, Magic Eden, Jupiter — all Cayman).
2. Zero tax + acceptable regulatory burden (CIMA exemption likely for non-custodial
   utility token).
3. 8–12 week timeline matches the TGE schedule.
4. $80–200K total all-in matches the documented Phase 2 budget envelope.
5. CEX listings (Binance, Coinbase, Kraken) accept Cayman with no friction.
6. Halborn / OtterSec / Trail of Bits all comfortable with Cayman issuer.

### 7.2 Alternative scenarios

**Scenario A — tighter budget, lower prestige requirement.** Choose **BVI** if total
Year-1 budget is capped at $40–60K and the founder is comfortable with reduced CEX-
listing leverage and slightly lower auditor / institutional perception.

**Scenario B — EU-first launch, long runway.** Choose **Liechtenstein** if EU
regulatory certainty is the primary objective and budget allows for $200K+ Year-1.
Consider as a sister entity to a Cayman parent rather than the sole structure.

**Scenario C — Layer-1 / ecosystem-foundation positioning.** Choose **Switzerland** if
the project repositions as a "decentralized compute Layer-1" rather than "B2B SaaS with
a token." Switzerland's Stiftung structure carries strong institutional weight in that
positioning.

**Scenario D — US-domestic ethos.** Choose **Wyoming** if the founder strongly prefers
US-domestic structure and accepts the federal-securities-law exposure. **NOT recommended
for iogrid per TOKENOMICS.md.**

**Scenario E — dual structure.** Some projects use a Cayman parent + Swiss / Liechtenstein
sister for EU-facing activities. Adds $50–100K to setup + $30–60K to annual maintenance
but provides global flexibility. Counsel can advise on whether this complexity is warranted.

### 7.3 Decision-making heuristic

Founder may use the following decision tree:

```text
1. Is the total Year-1 budget < $60K?
   YES -> BVI
   NO  -> continue

2. Is EU regulatory certainty the primary objective?
   YES -> Liechtenstein (or Cayman + Liechtenstein sister)
   NO  -> continue

3. Is the project positioned as a Layer-1 / ecosystem foundation?
   YES -> Switzerland
   NO  -> continue

4. Is US-domestic structure a values commitment despite federal risk?
   YES -> Wyoming (NOT recommended per TOKENOMICS.md; reconfirm with counsel)
   NO  -> Cayman (default)
```

For iogrid as of 2026-05-19, every branch points to **Cayman**.

---

## 8. Open items the founder must confirm before final selection

1. **Total Year-1 budget envelope** — is $80–200K acceptable?
2. **EU launch priority** — Phase 1 / Phase 2 / Phase 3 / Phase 4?
3. **Founder personal-residency consequence** — US founder issuing through Cayman is
   standard but has reporting consequences (FBAR / FinCEN); counsel + personal tax
   advisor confirms.
4. **Director / supervisor recruitment** — does the founder have leads on independent
   directors, or will counsel source them?
5. **Banking comfort** — Mercury / Brex (US-friendly) vs. Sygnum / SEBA (Swiss crypto-
   native) vs. Cayman National (Cayman-domestic).
6. **Dual structure complexity acceptance** — does the founder want to pre-commit to a
   single jurisdiction or leave the door open for a sister entity later?
7. **Counsel-shortlist alignment** — does the founder's preferred Phase 2 counsel
   (typically Cayman: Walkers / Maples / Conyers / Ogier per
   `legal/foundation/cayman-setup.md`) align with the selected jurisdiction?

---

## 9. References + further reading

- `legal/foundation/cayman-setup.md` — operational checklist for Cayman incorporation
- `legal/foundation/foundation-rules.md` — Foundation Rules template (jurisdiction-
  agnostic; counsel adapts to selected jurisdiction)
- `docs/TOKENOMICS.md` §"Legal risk + mitigation strategy" — original founder rationale
- `docs/whitepaper.md` §11 — Foundation governance rationale
- Helium Foundation public materials (Cayman precedent)
- Aptos Foundation public materials (Cayman precedent)
- Solana Foundation public materials (Cayman + Switzerland branch)
- Ethereum Foundation public materials (Switzerland Stiftung precedent)
- Tezos Foundation public materials (Switzerland Stiftung precedent)

---

`[COUNSEL: full document review recommended. Key open items: cost ranges in each
jurisdiction section are public-market estimates compiled from open-source comparable
engagements; counsel should validate against current market and specific firm
quotations. The scoring matrix in Section 6.2 is illustrative only. The decision tree
in Section 7.3 captures the founder's expressed preferences in TOKENOMICS.md and
existing docs; counsel may recommend re-weighting based on operational realities.]`

*End of Foundation jurisdiction comparison.*
