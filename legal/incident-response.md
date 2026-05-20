# Incident Response Protocol

**Status:** Draft v0.1 — pre-counsel-review. Not legal advice. See [`legal/README.md`](./README.md).
**Effective date:** *[COUNSEL: insert effective date upon finalization]*
**Scope:** Internal operational document + provider-facing summary. Defines how iogrid responds to law-enforcement inquiries, abuse incidents, security incidents, and defense-fund disbursement requests.

> **Plain-language summary** *(non-binding).*
>
> If a provider gets contacted by law enforcement, served with a subpoena, raided, or sued because of traffic that went through their iogrid IP, here is what happens: provider forwards the contact to legal@iogrid.org → iogrid responds with audit logs identifying the customer behind the request → customer's KYC is reviewed → if the provider meets the criteria, the defense fund covers their legal fees up to $25K. If the provider violated the AUP, the fund pays nothing.

---

## 1. Document audience

This protocol has two audiences:

1. **iogrid operations / legal team** — full internal protocol, including non-public detail (escalation paths, internal counsel contacts, fund-disbursement approval flows).
2. **Providers and Customers** — public-facing summary of what to expect, what to do, and what iogrid will do for them.

For this draft, the public-facing summary is presented in `## A.` sections; the internal-only detail is presented in `## B.` sections. Counsel may split into two documents at finalization.

---

## A. Public-facing protocol

### A.1 If you are a provider and receive a law-enforcement contact

**Step 1: Stay calm. Do not panic-uninstall.**
If LEO is at your door, do not destroy data, do not uninstall the Daemon, do not tamper with the Device. Doing so may itself be a crime in your jurisdiction (evidence destruction, obstruction).

**Step 2: Forward immediately to *[COUNSEL: insert legal contact, e.g., legal@iogrid.org]*.**
- Send a scan or photo of any written process (subpoena, court order, search warrant);
- Send the contact information for the LEO officer or agent;
- Include your Provider ID (from the Daemon UI);
- Include the timestamp and method of contact;
- Mark the message subject "URGENT — LEO contact" so we see it within minutes, not hours.

**Step 3: Cooperate factually with LEO but do not waive your right to counsel.**
You are entitled to consult a lawyer before answering substantive questions. The Daemon's role in your network is technically complex; you may legitimately decline to answer questions about traffic content (which you do not know) and customer identity (which iogrid retains in Audit Logs and will provide to LEO directly when properly compelled).

You may say (and we encourage you to say): "I run a piece of open-source software called iogrid that routes other companies' bandwidth through my home internet. The company that operates iogrid maintains audit logs identifying the customers behind every request. I do not personally know what traffic goes through my IP. Here is iogrid's legal contact: *[COUNSEL: insert]*."

**Step 4: We respond.**
On receipt of your forwarded contact, our legal team:
- Acknowledges within **4 business hours** during business days, or within **24 hours** including weekends / holidays;
- Engages outside counsel where appropriate;
- Pulls the relevant Audit Log entries;
- Contacts the issuing LEO authority directly (with your consent if you are not legally compelled to involve us) and provides the Customer identifying information for the traffic in question;
- Communicates the disposition back to you.

**Step 5: Defense fund disbursement.**
If your contact requires legal representation (e.g., you have been served a subpoena to appear, your Device has been seized, you are a witness in a criminal proceeding), and you have not violated the AUP, the iogrid Legal Defense Fund may cover your reasonable legal fees up to **$25,000 USD** (Section A.4).

### A.2 If you are a customer and receive a complaint or legal notice

**Step 1: Forward to *[COUNSEL: insert customer-legal contact]*.**
We need to know about (a) any DMCA-style notice naming a Workload, (b) any complaint from a target site, (c) any subpoena or government request naming your account.

**Step 2: We respond.**
We may suspend the affected Workload pending review, request authorization documentation from you, or — in serious cases — terminate your account.

**Step 3: Customer self-defense.**
Per Customer ToS Section 7, you are responsible for defending yourself against third-party claims arising from your Workloads. iogrid will provide reasonable evidentiary cooperation (Audit Logs, technical documentation) but does not defend you legally as we defend providers.

### A.3 If you discover abuse / AUP violation

Report to *[COUNSEL: insert abuse contact, e.g., abuse@iogrid.org]*. You may report anonymously. Include:
- Timestamp(s) of the suspected abuse;
- Destination URL or other target identifier;
- What you observed (e.g., your IP showing up on a botnet blacklist; your domain experiencing what looks like an iogrid-sourced scrape);
- Your contact information (optional but helps us follow up).

We respond within **5 business days** with the action taken. For CSAM (Section 2.1 of AUP), we additionally report to NCMEC's CyberTipline (US) or the national INHOPE hotline.

### A.4 Defense fund disbursement criteria

The iogrid Legal Defense Fund (described in [`docs/BUSINESS-STRATEGY.md` §6 Legal risk landscape](../docs/BUSINESS-STRATEGY.md#6-legal-risk-landscape--mitigation) §"Legal defense fund") is initially capitalized at **$10,000 USD** and is replenished by **5–10% of monthly B2B revenue**.

**Disbursement is available to:**
- Providers who have received a subpoena, search warrant, civil claim, or LEO contact arising from iogrid traffic;
- Provided the provider has NOT violated the AUP (verified via Audit Log analysis);
- Provided the provider notified iogrid of the contact in compliance with Provider ToS Section 5.3.

**Disbursement is NOT available to:**
- Providers in violation of the AUP;
- Customers (per Customer ToS Section 7 — customers defend themselves);
- Claims arising from a provider's pre-iogrid conduct or other unrelated activity on the same Device IP;
- Speculative or generalized defense (e.g., "I'm worried, can I have a retainer?" — no; fund covers actual proceedings).

**Cap per provider:**
- **$25,000 USD per incident**;
- **One incident per provider per 24-month period** (the fund is intended to address active legal exposure, not subsidize a recurring litigation defense);
- Cap may be exceeded with explicit approval by *[COUNSEL: identify approver — likely founder + legal lead jointly]*.

**Coverage scope:**
- Reasonable attorneys' fees and disbursements;
- Court filing fees;
- Expert-witness fees where necessary;
- Costs of complying with subpoenas (e.g., your time documenting records, with reasonable hourly compensation);
- Settlement amounts where we determine settlement is in the provider's interest, with the provider's consent;
- **NOT covered**: punitive damages, fines or penalties imposed for the provider's own conduct, opportunity costs of the provider's time beyond subpoena-compliance compensation.

**Disbursement process:**
1. Provider submits a defense-fund-request form (template at *[COUNSEL: insert URL]*) to *[COUNSEL: insert defense-fund contact, e.g., defense@iogrid.org]*.
2. iogrid legal team reviews within **5 business days**, includes Audit Log analysis to confirm no AUP violation.
3. On approval, iogrid pays counsel directly (preferred) or reimburses the provider on documented invoices.
4. Disbursements are tracked in iogrid's internal ledger; aggregate (anonymized) disbursement statistics are published in the quarterly transparency report starting Phase 2.

### A.5 What we will NOT do

- **Backdoor access.** We will not insert backdoors or create special-access channels for any government. Audit Logs are accessible via valid legal process — there is no privileged backchannel.
- **Voluntary surveillance handoff.** We will not voluntarily hand over Audit Logs absent a valid legal process, except in cases of imminent threat to life or to comply with mandatory-reporting laws (CSAM, terrorism).
- **Decryption of HTTPS.** We do not decrypt HTTPS traffic. No legal process can compel us to produce HTTPS payload content because we do not have it.
- **Provider data sale.** We do not sell, lease, or otherwise transfer for value the telemetry we collect from provider Devices.

---

## B. Internal protocol (operations + legal team)

*[COUNSEL: this section is operational detail. Counsel may move to a separate internal-only runbook at finalization. The public version (Section A) is the part that gets published.]*

### B.1 Severity levels

| Level | Trigger | Initial response time | Escalation |
|-------|---------|------------------------|------------|
| **SEV1** | Active LEO action against a Provider Device (raid, seizure); active CSAM transit detected; security breach with active exploitation | Within 30 minutes, 24×7 | Founder + legal lead + outside counsel immediately |
| **SEV2** | Subpoena requiring response within 14 days; high-value complaint; data-protection-authority inquiry | Within 4 business hours | Legal lead + outside counsel |
| **SEV3** | Routine subpoena (>30 days); standard DMCA notice; routine abuse report | Within 1 business day | Legal team |
| **SEV4** | Audit-rights inquiry from controller; informational request | Within 5 business days | Legal team |

### B.2 LEO request handling — internal steps

1. **Intake** — log the request in the legal-ticket system *[COUNSEL: select system]*, assign severity, assign owner.
2. **Validity check** — confirm issuing authority, scope, jurisdictional fit. Reject facially defective requests with a polite, documented response (template at *[COUNSEL: insert path]*).
3. **Scope minimization** — limit response to the exact records compelled. Do not over-share.
4. **Audit Log pull** — extract the relevant entries by Provider ID, timestamp range, and / or destination.
5. **Customer identification** — map Audit Log to Customer account; pull Customer KYC + AUP-history.
6. **Provider notification** — notify the named Provider unless legally prohibited (gag order, NDA, etc.). If prohibited, document the basis for non-notification.
7. **Response drafting** — outside counsel reviews any response containing personal data of EU / UK data subjects (Schrems II analysis for cross-border data flow).
8. **Response delivery** — to issuing authority via the channel specified in the request.
9. **Defense-fund triggering** — if the request creates legal exposure for the Provider, open a defense-fund file proactively (do not wait for Provider to ask).
10. **Closure** — record disposition; update transparency-report tally.

### B.3 Abuse-detection handling

- **CSAM hit** in pre-flight filter: block, report to NCMEC within 24 hours per 18 U.S.C. §2258A(a)(1) requirement *[COUNSEL: confirm iogrid's status as a reporting entity]*. Preserve Audit Log indefinitely (override the 90-day rolling deletion). Terminate the Customer account immediately and preserve all related records for potential law-enforcement subpoena.
- **Phishing-list hit**: block; notify Customer and inquire (some Customers — security-research firms — have legitimate reasons to query phishing domains, see AUP Section 4.2).
- **DDoS pattern detected** (per-target, per-Customer burst above thresholds): rate-limit and notify; if sustained, terminate.
- **Credential-stuffing pattern detected**: rate-limit and notify; if sustained, terminate.
- **Banking-domain target without authorization**: block; investigate; terminate if no authorization is produced.

### B.4 Security-incident handling

- See iogrid security runbook *[COUNSEL: confirm referenced internal doc exists]*.
- Breach-notification timing: within 72 hours to data-controller-Customers per DPA Section 3.7.
- Affected-data-subject notification per applicable law (GDPR Art. 34 if high risk; CCPA / state laws; LGPD; etc.).

### B.5 Defense-fund administration

- Fund balance is a line item in iogrid's bookkeeping (operating reserve, **not** a separate trust).
- Replenishment is automated monthly: 5–10% of B2B revenue (the exact percentage is set quarterly by the founder + legal lead).
- Approval flow: requests <$5,000 → legal lead; $5,000–$25,000 → legal lead + founder; >$25,000 → board / governance vote (Phase 3+) or founder + outside-counsel recommendation (current).
- Reporting: aggregate disbursements published in quarterly transparency report.

### B.6 Transparency report

Starting Phase 2 (target: end of 2026), iogrid publishes a quarterly transparency report at *[COUNSEL: insert URL, e.g., https://iogrid.org/transparency]* containing:
- Aggregate count of LEO requests received by jurisdiction;
- Compliance rate (responded vs. rejected as overbroad);
- Defense-fund disbursements (anonymized aggregate);
- Account terminations for AUP violations (anonymized aggregate);
- CSAM reports to NCMEC / INHOPE (numeric counts only — never any identifying information).

### B.7 Warrant canary (Phase 3 consideration)

Per `docs/LEGAL.md` Open items §5, the legal value of a warrant canary is debated. If implemented, it will be a separate page maintained by the legal lead, signed monthly with a published key, and removed if iogrid has received a national-security letter (US) or other gag-ordered process where the canary's absence is the only lawful signal. *[COUNSEL: review enforceability and risk-management value.]*

---

## C. Cross-references

- [Provider Terms of Service](./provider-tos.md) Section 5 (Indemnification), Section 6 (Audit-cooperation).
- [Customer Terms of Service](./customer-tos.md) Section 7 (Customer Indemnification), Section 9 (Audit rights).
- [Acceptable Use Policy](./aup.md) Section 10 (Enforcement).
- [Data Processing Agreement](./dpa.md) Section 3.7 (Breach notification).
- [Privacy Policy](./privacy-policy.md) Section 5.2 (Law enforcement requests).
- [`docs/BUSINESS-STRATEGY.md` §6 Legal risk landscape](../docs/BUSINESS-STRATEGY.md#6-legal-risk-landscape--mitigation) §"Legal defense fund," §"Cooperation with law enforcement."

---

*End of Incident Response Protocol v0.1-draft.*

*[COUNSEL: full-document review required. Key open items: NCMEC reporting-entity status confirmation (Section B.3, B.6), defense-fund structural question — operational reserve vs. true segregated fund (Section B.5), warrant-canary policy decision (Section B.7), transparency-report content scope (Section B.6), severity-level escalation contacts (Section B.1), public-vs-internal document split (overall document structure). Total `[COUNSEL]` markers: ~12.]*
