# iogrid legal scaffolding

This directory contains **placeholder drafts** of the legal documents required for iogrid's Phase 1 launch, prepared per the specification in [`docs/BUSINESS-STRATEGY.md` §6 Legal risk landscape](../docs/BUSINESS-STRATEGY.md#6-legal-risk-landscape--mitigation).

> **Important — these documents are NOT legal advice.** They are working drafts intended to give qualified counsel a coherent, complete starting point so review and finalization can proceed quickly and economically. Every section that requires a legal judgment call carries a `[COUNSEL: …]` marker. **Do not deploy any of these documents to providers, customers, or the public until they have been reviewed and finalized by a qualified attorney in each relevant jurisdiction.**

---

## Documents in this directory

| # | File | Purpose | Audience |
|---|------|---------|----------|
| 1 | [`provider-tos.md`](./provider-tos.md) | Provider Terms of Service | Anyone running the iogrid daemon and sharing bandwidth / compute |
| 2 | [`customer-tos.md`](./customer-tos.md) | Customer Terms of Service | API customers (bandwidth proxy, Docker compute, GPU inference, iOS builds) |
| 3 | [`aup.md`](./aup.md) | Acceptable Use Policy | Providers and Customers (binding via ToS) |
| 4 | [`dpa.md`](./dpa.md) | Data Processing Agreement (GDPR Art. 28) | EU providers and customers |
| 5 | [`privacy-policy.md`](./privacy-policy.md) | Privacy Policy | Public-facing, all data subjects |
| 6 | [`token-disclaimer.md`](./token-disclaimer.md) | $GRID token risk factors | Anyone who acquires or earns $GRID |
| 7 | [`incident-response.md`](./incident-response.md) | Law-enforcement / abuse-incident protocol | Internal ops + provider-facing summary |

---

## Revision history

| Date | Version | Author | Notes |
|------|---------|--------|-------|
| 2026-05-19 | 0.1-draft | iogrid eng (placeholder) | Initial scaffolding from `docs/LEGAL.md` + `docs/TOKENOMICS.md`. Pre-counsel-review. |

> **Last legal review:** *Never. Counsel review required before Phase 1 launch.*

---

## Counsel-review checklist

Before any of these documents may be presented to a provider, customer, or regulator, the following must occur:

- [ ] **Engagement of qualified counsel.** Recommended profile: technology-transactions partner at a firm with current experience in (a) US consumer-facing SaaS terms, (b) GDPR / EU data protection, (c) digital-asset / token regulation (for `token-disclaimer.md`). Suggested firms in the docs/LEGAL.md notes: Cooley, Fenwick, Davis Polk, Latham & Watkins, or local equivalents.
- [ ] **Jurisdiction confirmation.** Lock the operating-entity jurisdiction (iogrid as a Dynolabs product line vs. spun out, Cayman Foundation vs. Delaware C-Corp vs. other) before finalizing governing-law clauses across every document.
- [ ] **Provider ToS review.** Verify consent statement language is enforceable in each launch jurisdiction; verify common-carrier framing does not over-promise immunity; verify indemnification clause is mutual where appropriate.
- [ ] **Customer ToS review.** Verify liability cap, arbitration clause (class-action waiver enforceability varies), and right-of-refusal are compliant with consumer protection laws.
- [ ] **AUP review.** Verify the forbidden / allowed lists match operational filters; verify reporting obligations (NCMEC, etc.) are correctly stated.
- [ ] **DPA review.** Verify Annex 28 / GDPR processor obligations; verify Standard Contractual Clauses 2021/914 module-selection and that sub-processor list is current; consider UK IDTA + Swiss FADP addenda.
- [ ] **Privacy Policy review.** Verify GDPR Art. 13 / Art. 14 disclosures; verify CCPA / CPRA "notice at collection" language; verify Brazilian LGPD lawful basis statements; verify cookie disclosures.
- [ ] **Token disclaimer review.** Mandatory crypto-securities counsel. Review for Howey-test exposure, MiCA classification (utility token vs. asset-referenced token vs. e-money token), Liechtenstein TVTG fit, and US geo-block enforcement language.
- [ ] **Incident response review.** Verify subpoena-handling protocol matches actual operational capability; verify NCMEC reporting language matches the organization's registered status; verify the defense-fund disbursement language does not create unintended fiduciary duty.
- [ ] **Sign-off recorded** in the revision-history table above with reviewing-counsel name, firm, and date.

---

## Cost estimate (per `docs/LEGAL.md`)

| Item | Estimated cost (USD) | Notes |
|------|----------------------|-------|
| Provider ToS — counsel review + redraft | $1,500 – $3,000 | Per-jurisdiction multiplier may apply |
| Customer ToS — counsel review + redraft | $1,500 – $3,000 | |
| AUP — counsel review | $500 – $1,500 | Usually rolls into ToS engagement |
| DPA — counsel review (GDPR specialist) | $1,500 – $3,500 | Includes SCC module selection |
| Privacy Policy — counsel review | $1,500 – $3,000 | |
| Token disclaimer — crypto-securities counsel | $5,000 – $15,000 | Often packaged with full token legal opinion at $25K–$75K (see `docs/TOKENOMICS.md`) |
| Incident response — counsel review | $500 – $1,000 | |
| **Phase 1 minimum (non-token docs)** | **$5,000 – $10,000** | Matches `docs/LEGAL.md` estimate |
| **Including token disclaimer scope** | **$30,000 – $90,000** | Matches `docs/TOKENOMICS.md` legal budget |

---

## How to use these drafts

1. Read this README in full.
2. Read [`docs/BUSINESS-STRATEGY.md` §6 Legal risk landscape](../docs/BUSINESS-STRATEGY.md#6-legal-risk-landscape--mitigation) and [`docs/BUSINESS-STRATEGY.md` §4 Currency model](../docs/BUSINESS-STRATEGY.md#4-currency-model--grid--fiat-hybrid) for product context.
3. Walk each document. Treat every `[COUNSEL: …]` marker as a TODO for the reviewing attorney.
4. Provide the markdown source to counsel — they may convert to their preferred format (PDF, Word, web form).
5. Once counsel finalizes, update the revision history above and add a `Status: Finalized` line under each document's "Document status" header.

---

## Plain-language goal

These drafts deliberately favor plain English over legalese where the operative meaning can be expressed clearly. A typical provider or customer should be able to read these documents end-to-end without a lawyer and understand (a) what they are agreeing to, (b) what they cannot do, and (c) what happens if something goes wrong. Counsel may add formal language as needed — but the plain-language summaries should remain accessible.

---

## What these drafts are NOT

- **Not legal advice.** The authors are not your attorneys.
- **Not jurisdiction-specific.** The drafts reference US / EU / UK frameworks but do not customize for local consumer-protection regimes (e.g., Australian Consumer Law, California's CCPA-specific notice requirements, Brazilian LGPD operational specifics, etc.). Counsel must add jurisdiction-specific clauses.
- **Not a compliance certification.** None of the drafts claim "GDPR-compliant" or "SOC 2-certified" or similar — those status claims require formal audit and counsel sign-off.
- **Not final.** The documents will change during review. Track changes in revision history.
