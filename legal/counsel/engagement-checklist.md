# Counsel engagement — operational checklist

**Status:** Draft v0.1 — pre-counsel-review. Not legal advice. **This is an operational checklist for the founder, not a substitute for engagement with qualified counsel.**

**Related issues:**

- [#155](https://github.com/iogrid/iogrid/issues/155) — `legal/*` requires counsel review before Phase 1 launch
- [#103](https://github.com/iogrid/iogrid/issues/103) — Foundation jurisdiction selection
- [#122](https://github.com/iogrid/iogrid/issues/122) — Foundation incorporation

**Purpose:** Walk the founder through every operational step from "I have selected a firm" to "the engagement is live and producing deliverables on cadence." This document complements `legal/counsel/rfp.md` (the procurement RFP).

---

## 0. Pre-engagement (founder action items)

Before the first engagement-letter conversation:

1. **Read** the full RFP at `legal/counsel/rfp.md`.
2. **Read** `docs/BUSINESS-STRATEGY.md` §6 (Legal risk landscape & mitigation), `docs/BUSINESS-STRATEGY.md` §4 (Currency model — $GRID + fiat hybrid), and every file in `legal/`.
3. **Decide** which Phase(s) the founder will engage on this round (Phase 1 only, Phase 2 only, or both — see `legal/counsel/rfp.md` §2).
4. **Decide** Foundation-jurisdiction preference (Cayman is the documented default; alternatives at `legal/foundation/jurisdiction-comparison.md`).
5. **Decide** budget envelope (Section 3 of the RFP defaults; founder may tighten or expand).
6. **Decide** which firms receive the RFP and in what order (Section 5 of the RFP).
7. **Confirm** the founder is the sole signatory on engagement letters (or, if not, identify the alternate signatory and their authority).
8. **Allocate** founder time: roughly 4–8 hours/week for the duration of Phase 1, 8–12 hours/week for Phase 2 weeks 1–4.
9. **Set up** a private (NOT public-repo) location for engagement letters, executed agreements, and counsel privileged communications. Recommendations: 1Password Business shared vault, or Notion private workspace with explicit access control.

---

## 1. Engagement-letter terms to negotiate

Standard counsel engagement letters from large firms favor the firm. The founder should negotiate the following terms; most are standard asks that most reputable firms will accept.

### 1.1 Rate caps + fixed-fee preference

- **Fixed-fee preference:** ask for a fixed-fee bid for each Phase 1 deliverable (per `legal/counsel/rfp.md` §2.1) and for a fixed-fee bid for the Foundation-incorporation portion of Phase 2.
- **Hourly cap:** for portions that genuinely must be hourly (e.g., regulator response, complex novel legal questions), ask for a not-to-exceed cap with explicit founder-approval required before exceeding.
- **Rate freeze:** ask for partner + associate hourly rates to be frozen for the duration of the engagement (typically 12 months).
- **Disbursements**: ask for itemized disbursements + a $500 disbursement cap per month without explicit approval.

### 1.2 Scope-creep mechanism

- **Defined scope:** the engagement letter must list each deliverable from `legal/counsel/rfp.md` §2.1 (Phase 1) or §2.2 (Phase 2) explicitly.
- **Out-of-scope policy:** counsel must obtain written founder approval (email is fine) before incurring fees for any work outside the listed deliverables.
- **Scope-change estimates:** counsel must provide a written estimate (within $X +/- 25%) before founder approves an out-of-scope item.

### 1.3 IP ownership of work product

- **Work-product ownership:** all deliverables (markup, memos, redrafted documents) are iogrid property, deliverable in their source format (markdown source preferred; Word acceptable; PDF rejected as primary format).
- **Public-domain commitment:** iogrid intends to publish all finalized legal documents to the public repository at `github.com/iogrid/iogrid`. The engagement letter must permit this; some firms include "firm-confidential" boilerplate that conflicts with this intent. Negotiate the carve-out explicitly.
- **No firm name on public documents** without explicit founder consent (most firms prefer not to be named on a public legal document anyway; this is a mutual ask).

### 1.4 Termination + transition

- **No-cause termination:** founder may terminate the engagement at any time on 14 days' notice without cause.
- **Outstanding-work payment:** founder pays for work actually performed up to the termination date plus a reasonable wind-down; no termination-fee or "minimum engagement" lock-in.
- **File transfer on termination:** counsel transfers all work product (in source format) and active matter files within 30 days of termination.
- **Counsel-of-record withdrawal:** counsel files appropriate withdrawal in any regulatory proceeding within 30 days.

### 1.5 Conflict-of-interest waiver

- **Affirmative conflict check:** counsel certifies that no current or recent (within 2 years) representation exists for:
  - Direct iogrid competitors (Bright Data, IPRoyal, Honeygain, Salad, Pawns.app, Proxycurl, Soax, Smartproxy, Apify, Octoparse, ScrapingBee, Decodo, NetNut)
  - Major target-platform plaintiffs in scraping litigation (Meta v. Bright Data, X v. Bright Data, LinkedIn v. hiQ Labs, NYT v. OpenAI, etc., as relevant to AUP §5)
  - Any iogrid sub-processor whose engagement may be impacted by counsel's advice (Stripe, Sumsub, Persona, AWS, MoonPay, Sentry, Datadog — see `legal/dpa.md` Annex 3)
  - Any current or prior iogrid investor (if applicable)
- **Future-conflict waiver:** founder will NOT pre-consent to future conflicts that may arise; each future conflict gets its own consent decision.
- **Information-barrier acceptance:** if counsel maintains an information barrier with a related representation, founder will accept that barrier only on the firm's affirmative representation that the barrier is operationally enforced.

### 1.6 Liability cap

- **Standard cap:** typical engagement-letter cap is the lesser of (a) 3x fees paid in the matter or (b) $X (often $1M for mid-sized firms).
- **Carve-outs:** the cap should not apply to liability for (a) intentional misconduct, (b) gross negligence, (c) breach of confidentiality, (d) malpractice (where prohibited by professional-conduct rules).
- **Insurance representation:** counsel represents that it carries professional malpractice insurance of at least $X (typical: $10M for mid-sized firms; $50M+ for AmLaw100 firms).

### 1.7 Communication protocol

- **Named contacts:** engagement letter names the lead partner + lead associate; founder may direct all communications to either.
- **Response-time SLAs:** counsel will respond to founder email within 2 business days; urgent matters (subpoena, regulator letter) within 4 business hours.
- **Weekly check-in:** counsel attends a 30-minute weekly check-in for the duration of active engagement (Section 4 of this document).
- **Billing cadence:** monthly billing with itemized time records; founder reviews within 14 days of receipt; disputes resolved via partner conversation within 30 days.

`[COUNSEL: review the negotiation positions above; founder may adjust before transmission. Some firms will reject the markdown-source-deliverable requirement (Section 1.3) — confirm this is a hard or soft requirement for the founder.]`

---

## 2. Conflict-of-interest disclosure required

Before engagement-letter signing, the founder must receive a written conflict-of-interest disclosure that affirmatively addresses:

1. **Direct competitors** (list in Section 1.5).
2. **Plaintiffs in adverse scraping litigation** (list in Section 1.5).
3. **Major sub-processors** (Stripe, Sumsub, Persona, AWS, MoonPay, Sentry, Datadog).
4. **Other token issuers** that compete with `$GRID` for the same provider attention (Helium, Render, Akash, io.net, Salad token, Theta, etc.).
5. **Foundation-jurisdiction registry counterparties** if Phase 2 (e.g., Cayman Islands Government, CIMA — typically not a conflict, but flag for completeness).
6. **iogrid investors** (when iogrid has investors).
7. **Founder personal representations** (counsel disclosing whether they represent the founder personally in unrelated matters).

If a conflict exists, the founder evaluates whether to (a) accept a written conflict waiver from the existing client (rare), (b) accept an information barrier, or (c) decline and proceed with a different firm.

---

## 3. Information packet to send counsel (on NDA, post-engagement)

Once the engagement letter is signed AND a mutual NDA is in place, send counsel the following information packet:

### 3.1 Public material (already linked in `legal/counsel/rfp.md` §9)

- This engagement-checklist document (the one you are reading).
- All public material from RFP §9.

### 3.2 Private operational material

- Operating-entity cap table (Dynolabs Inc. as of date).
- Founder + key-contributor KYC pack (passport scans, proof of address, biographies).
- Existing vendor contracts (Stripe, Sumsub / Persona, AWS, MoonPay, Sentry, Datadog).
- Sub-processor list with current-state DPA execution status.
- Current bank and crypto-custody arrangements.
- Any regulator correspondence to date (typically empty for a pre-launch project).
- Any prior counsel work product (typically empty for a pre-launch project).

### 3.3 Technical material (on request)

- Architecture and threat-model documentation (`docs/ARCHITECTURE.md` is public; private detail is on request).
- KYC pipeline implementation (private code).
- Anti-abuse filter implementation (private code).
- Audit-log retention implementation (private code).
- Sub-processor data-flow diagrams.

---

## 4. Kick-off meeting agenda

The first 60-minute call after engagement-letter signing. Attendees: lead partner, lead associate, founder, lead engineer (for technical Q&A). Agenda:

1. **Introductions** (5 minutes).
2. **Project re-brief** (10 minutes): founder summarizes iogrid in 5 minutes; engineer covers daemon + coordinator architecture in 5 minutes.
3. **Scope confirmation** (10 minutes): counsel confirms each deliverable from the engagement letter; resolve any ambiguities. Add or remove deliverables in writing if needed.
4. **Timeline confirmation** (5 minutes): confirm milestone dates; identify any founder-blocking dependencies (KYC docs, sub-processor contracts, etc.).
5. **Communication protocol** (5 minutes): confirm weekly check-in time; confirm urgent-matter contact path; confirm preferred document-exchange channel.
6. **Open `[COUNSEL: ...]` markers walkthrough** (15 minutes): walk through `legal/counsel/issue-list.md`; counsel flags the markers they want to address first.
7. **Information-packet status** (5 minutes): confirm NDA executed, confirm packet received, identify any missing items.
8. **Next steps + action items** (5 minutes): founder + counsel each leave with a written action-item list.

Founder takes notes and circulates a memo within 24 hours of the call; counsel confirms or corrects within 48 hours.

---

## 5. Weekly check-in protocol

For the duration of active engagement, a 30-minute weekly check-in. Attendees: lead associate (mandatory), lead partner (optional, for substantive decisions), founder (mandatory).

Agenda (15 minutes structured + 15 minutes open):

1. **Progress update** (5 minutes): what was delivered this week, against the engagement-letter timeline.
2. **Open `[COUNSEL: ...]` markers status** (5 minutes): walk through the issue-list.md tracker; mark each resolved marker.
3. **Founder-blocking items** (3 minutes): does counsel need anything from the founder to unblock the next week's work?
4. **Counsel-blocking items** (2 minutes): does the founder need anything from counsel to unblock product / engineering / fundraising?
5. **Open Q&A** (15 minutes): substantive legal questions, sub-processor reviews, regulator-correspondence drafting, etc.

After each check-in:

- Founder commits a meeting memo to the private (NOT public) records location.
- Counsel updates the deliverable tracker (counsel-side; founder-side mirror in `legal/counsel/issue-list.md`).
- Any new action items entered into the founder's tracker.

---

## 6. Escalation protocol

If the engagement diverges from plan:

### 6.1 Schedule slip

- **<1 week slip:** founder acknowledges; counsel commits to a revised date.
- **1–2 week slip:** partner-level conversation; founder reviews whether to add staff or reduce scope.
- **>2 week slip on a critical-path deliverable:** founder evaluates termination (per Section 1.4) and engagement of alternative counsel.

### 6.2 Budget overrun

- **<10% overrun:** counsel notifies founder in writing; founder approves or rejects.
- **10–25% overrun:** partner-level conversation; founder reviews whether scope or staffing model needs adjustment.
- **>25% overrun:** founder evaluates termination.

### 6.3 Quality concerns

- **Deliverable quality:** founder requests partner-level review; engagement may continue with revised quality bar.
- **Strategic disagreement:** founder seeks second opinion (paid, ~$2–5K for a 1-hour partner consult at a different firm); founder makes the final call.

### 6.4 Conflict-of-interest discovery mid-engagement

- **New conflict surfaces:** counsel must disclose within 5 business days of discovery.
- **Founder evaluates:** (a) waiver, (b) information barrier, (c) termination.
- **No retroactive billing** for work done while in undisclosed conflict.

---

## 7. Engagement close-out protocol

When the engagement is complete (Phase 1 deliverables published OR Foundation operational OR token-launch sign-off achieved):

1. **Final deliverable acceptance:** founder confirms in writing that each engagement-letter deliverable has been received and is acceptable.
2. **Final invoice:** counsel issues final invoice within 14 days of close-out.
3. **Work-product transfer:** counsel transfers all source-format work product to the founder; founder confirms receipt.
4. **Reference offer:** founder asks counsel for permission to use as a future reference; counsel decides.
5. **Repository commit:** founder commits all finalized legal documents to `github.com/iogrid/iogrid` per the publication plan in `legal/counsel/rfp.md` §9.
6. **Revision-history sign-off:** the `legal/README.md` revision-history table is updated with counsel name, firm, and date for each finalized document.
7. **Engagement-letter close:** the engagement letter is filed in the private records location; the public repository commits do NOT reference the firm name (per Section 1.3) unless mutually agreed.
8. **Renewal decision:** founder decides whether to engage the same firm for ongoing matters (annual maintenance, regulator inquiries, transparency-report review, etc.) or to pursue a separate retainer.

---

## 8. Annual maintenance engagements

Post-Phase-1 and post-Phase-2, iogrid will need ongoing legal support. Typical annual-maintenance engagements:

| Item | Cadence | Typical cost |
|------|---------|--------------|
| Document refresh (annual review of ToS, AUP, DPA, Privacy Policy) | Annual | $5–15K |
| Sub-processor list update + DPA refresh | Per change | $1–3K per new sub-processor |
| Transparency-report counsel review | Quarterly | $2–5K per report |
| Subpoena / law-enforcement-letter response | Per incident | $1–5K per response |
| Regulator inquiry response | Per incident | $5–25K per inquiry |
| Foundation annual filings | Annual | $5–10K |
| Token-disclosure refresh (post-TGE) | Annual or per material change | $5–15K |
| MiCA white-paper refresh (if EU launch) | Annual | $5–10K |

Counsel may bid for any of these as separate retainers post-Phase-1 / post-Phase-2.

---

## 9. Multi-jurisdiction coordination

If iogrid engages counsel in multiple jurisdictions (e.g., US lead counsel + Cayman corporate counsel + EU MiCA counsel + UK FCA counsel), one firm should be designated as **coordinating counsel**.

Responsibilities of coordinating counsel:

- Single point of contact for the founder.
- Routes questions to the appropriate jurisdictional counsel.
- Ensures consistency of legal positions across jurisdictions.
- Owns the deliverable tracker.
- Hosts the weekly check-in (Section 5).

Recommended choice: the firm running Phase 2 Foundation incorporation (typically Cayman counsel; see `legal/foundation/cayman-setup.md` for the Walkers / Maples recommendation).

---

## 10. Decision points the founder owns

Counsel will not decide these for you; founder retains decision rights even with counsel engaged.

1. **Foundation jurisdiction** (Cayman / BVI / Liechtenstein / Wyoming / Switzerland). Counsel recommends; founder decides.
2. **Restricted-jurisdiction list** for token launch. Counsel recommends; founder decides which jurisdictions to geo-block at TGE.
3. **Sub-processor selection** (Sumsub vs. Persona for KYC; Datadog vs. Sentry for observability; etc.). Founder decides; counsel reviews DPA fit.
4. **Tier-name and pricing** for Provider / Customer ToS. Founder decides; counsel reviews for regulatory fit.
5. **Token-2022 freeze authority retention vs. burn.** Counsel recommends per regulatory posture; founder decides per centralization-perception trade-off.
6. **Transparency-report cadence** (annual / semi-annual / quarterly). Founder decides; counsel reviews for legal-exposure trade-off.
7. **Defense-fund structure** (operational reserve vs. true segregated fund). Counsel recommends; founder decides.
8. **Whether to publish counsel firm name** on finalized documents. Mutual decision.
9. **Phase 3 DAO migration trigger** (24-month track-record + Supervisor vote, per `legal/foundation/foundation-rules.md`). Founder + Supervisors decide; counsel codifies.

---

`[COUNSEL: full document review recommended. Key open items: liability-cap defaults in Section 1.6 are general-market norms and may need adjustment for the specific firm tier; conflict-of-interest competitor list in Section 1.5 / 2.1 should be reviewed for completeness; budget envelopes in Section 8 are public-market estimates not quotes.]`

*End of counsel-engagement operational checklist.*
