# Incident-response operational playbook

**Status:** Draft v0.1 — pre-counsel-review. **Not legal advice.** **This is an
operational runbook for the iogrid legal / operations team, not a substitute for advice
from qualified counsel on any specific incident.**

**Related issues:**

- [#155](https://github.com/iogrid/iogrid/issues/155) — `legal/*` requires counsel review
  before Phase 1 launch
- [#103](https://github.com/iogrid/iogrid/issues/103) — Foundation jurisdiction selection
- [#122](https://github.com/iogrid/iogrid/issues/122) — Foundation incorporation

**Companion documents:**

- `legal/incident-response.md` — public-facing incident-response protocol
- `legal/aup.md` — Acceptable Use Policy (abuse categorization)
- `legal/provider-tos.md` — Provider Terms (indemnification, audit-cooperation,
  defense-fund eligibility)
- `legal/customer-tos.md` — Customer Terms (customer-side cooperation duty)
- `legal/dpa.md` — Data Processing Agreement (data-subject-rights interaction)

**Purpose:** Step-by-step runbook covering the most likely incident types:

1. Provider receives a law-enforcement (LEO) inquiry
2. Customer receives a complaint or notice
3. Abuse / AUP violation reported
4. Defense-fund disbursement request
5. Data breach
6. Regulator inquiry
7. NCMEC CyberTipline reporting flow
8. Transparency-report addendum drafting

---

## 0. Triage table — incident classification

Every incoming report is classified within 15 minutes of receipt. Severity drives
escalation cadence.

| Severity | Definition | Acknowledgement SLA | Initial response SLA | Escalation |
|----------|-----------|---------------------|----------------------|-------------|
| P0 — critical | Active LEO action (search warrant, arrest), or CSAM detection, or imminent regulator action, or active data breach | 30 minutes 24/7 | 4 hours | Founder + outside counsel paged immediately |
| P1 — high | Subpoena (non-emergency), civil-litigation complaint naming iogrid, customer complaint involving alleged unlawful conduct | 4 business hours | 24 hours | Outside counsel engaged; founder notified |
| P2 — medium | Standard DMCA notice, AUP violation report, sub-processor incident, transparency-report inquiry | 1 business day | 5 business days | Internal handling; outside counsel on-call |
| P3 — low | General inquiry, transparency-report-data-request, hypothetical question, press inquiry related to legal posture | 3 business days | 14 business days | Internal handling |

---

## 1. Provider receives a law-enforcement inquiry

### 1.1 Incident scenario

A provider runs the iogrid daemon on a home device. Local law enforcement contacts the
provider — by mail, phone, in-person visit, or formal subpoena — because traffic from
the provider's IP was implicated in a law-enforcement investigation. Common triggers:

- A target platform (e.g., a major retailer, a social network) filed a complaint about
  what appears to be scraping or credential-stuffing from the provider's IP.
- A target platform's CSAM-detection system fired on traffic that transited the
  provider's IP (NOTE: iogrid's pre-flight filters should have blocked this; if it
  reached the target, this is a filter-failure incident — see Section 5).
- A criminal investigation into a downstream customer named the provider's IP in a
  warrant or subpoena.
- A civil-litigation party named the provider's IP in a third-party-subpoena seeking
  records.

### 1.2 Step-by-step (operator side)

1. **Receive** the forward from the provider per `legal/incident-response.md` §A.1
   Step 2. Subject line includes "URGENT — LEO contact" or auto-routed via the
   provider dashboard's "report law-enforcement contact" workflow.
2. **Acknowledge** the provider within the SLA in Section 0 (P0 30 min / P1 4 business
   hours). Use template at Section 9.1 (provider acknowledgement letter).
3. **Triage**: assign severity per Section 0. Generally LEO contact = P1 minimum;
   active in-person LEO contact or search-warrant execution = P0.
4. **Page** appropriate escalation:
   - P0: founder + outside counsel paged immediately (PagerDuty + phone).
   - P1: outside counsel engaged within 24 hours; founder notified by email.
5. **Pull** the relevant audit logs (90-day retention per `legal/provider-tos.md` §6).
   Specifically:
   - Provider ID + timestamps of contact.
   - All workload requests from / to the provider's IP within the time-window described
     in the LEO contact.
   - Customer identifying information (KYC level, account creation date, payment
     instrument).
   - Pre-flight filter logs (CSAM, phishing, port restrictions — see `docs/BUSINESS-STRATEGY.md` §6 (Legal risk landscape & mitigation)).
6. **Preserve** the audit logs: override the 90-day rolling deletion. Mark the audit-log
   bucket entries with a legal-hold flag.
7. **Classify** the underlying conduct:
   - If a customer is identified AND the customer was using iogrid for lawful purposes,
     proceed to Section 1.3 (defense-fund eligibility check) for the provider, and
     contact the LEO authority with the customer-identifying information (Section 1.4).
   - If a customer is identified AND the customer was violating the AUP, proceed to
     Section 1.5 (AUP violation handling) and notify the LEO authority of the customer
     identity AND the AUP violation context.
   - If no customer is identified within the time-window (rare; would indicate audit-
     log gap), escalate to founder + counsel; investigate as a system-integrity
     incident.
8. **Contact** the LEO authority directly (with provider consent unless legally
   compelled to involve us). Use template at Section 9.2 (LEO-response letter).
9. **Communicate** disposition back to the provider. Use template at Section 9.3
   (provider disposition letter).
10. **Defense-fund eligibility check** per Section 4 if provider has not violated the
    AUP and has met notification-timing duties.
11. **Document** the incident in the legal-ticket system (severity, owner, key dates,
    final disposition). Use the incident-report template at Section 9.6.
12. **Schedule** transparency-report inclusion per Section 8.

### 1.3 Provider eligibility for defense fund (quick check)

Per `legal/incident-response.md` §A.4, defense fund is available if:

- Provider has received an actual proceeding (subpoena, warrant, civil claim, LEO
  contact) arising from iogrid traffic — NOT speculative.
- Provider has NOT violated the AUP (verified via audit-log analysis — Section 1.2.5).
- Provider notified iogrid within 5 business days per Provider ToS §5.3.
- Provider has not previously received defense-fund disbursement within the last 24
  months.

If all four conditions are met, proceed to Section 4 (defense-fund disbursement flow).

### 1.4 LEO authority contact

Best practice:

- Acknowledge the LEO contact promptly (within 24 hours of provider forward).
- Confirm the issuing-authority's identity and the legal basis for the request
  (subpoena, court order, warrant) via official channels (court PACER lookup, agency
  switchboard callback, etc.).
- Engage outside counsel before providing substantive response.
- Respond with the audit-log extract identifying the customer + the customer's KYC
  level + the workload type + timestamps.
- Decline to provide any data not specifically responsive to the lawful process unless
  outside counsel advises otherwise.
- Object to overbroad requests via counsel.
- Maintain a chain-of-custody record for any data produced.

### 1.5 AUP-violation handling

If audit-log analysis reveals the customer violated the AUP:

- Immediately suspend the customer account (per `legal/customer-tos.md` and
  `legal/aup.md` §10.2).
- Preserve all customer records (KYC pack, payment history, workload submissions, audit
  logs) under indefinite legal hold.
- Notify the LEO authority of the AUP violation + customer identity.
- Notify the provider that their indemnification under Provider ToS §10 is preserved
  (because the violator is the customer, not the provider).
- Notify the provider of defense-fund eligibility status (eligible if provider did NOT
  participate in the violation).
- Initiate customer-side enforcement (termination, claim recovery, etc.) under
  `legal/customer-tos.md` §11–14.

---

## 2. Customer receives a complaint or notice

### 2.1 Incident scenario

A target platform or third-party complainant contacts iogrid about a customer's
workload. Common triggers:

- DMCA-style takedown notice about content accessed via iogrid bandwidth proxy.
- Cease-and-desist letter from a target platform's general counsel.
- Civil litigation naming iogrid + the customer as co-defendants in a scraping or
  ToS-breach claim.

### 2.2 Step-by-step

1. **Receive** the contact at the customer-legal contact (`legal/customer-tos.md` §13
   contact path).
2. **Triage**: classify per Section 0 (P1 typical; P0 if criminal allegation or court
   order).
3. **Notify** the customer immediately (within 24 hours of receipt).
4. **Pull** audit logs scoped to the customer's workloads + the targeted destination.
5. **Investigate**:
   - Was the customer's use compliant with the AUP?
   - Was iogrid's pre-flight filter implementation working correctly?
   - Did the customer comply with the customer-cooperation duty under
     `legal/customer-tos.md`?
6. **Disposition**:
   - If the customer was AUP-compliant AND iogrid's filters worked: respond to the
     complainant via counsel; customer self-defends per Customer ToS §7; iogrid provides
     evidentiary cooperation but does NOT defend the customer.
   - If the customer was NOT AUP-compliant: suspend or terminate the customer; preserve
     records; respond to the complainant with the AUP-violation finding; pursue customer
     for breach under `legal/customer-tos.md`.
   - If iogrid's filters failed (Section 5 territory): treat as system-integrity
     incident; defense-fund and indemnification may attach.
7. **Document + transparency**: log incident; schedule for transparency-report
   inclusion per Section 8.

---

## 3. Abuse / AUP violation report

### 3.1 Incident scenario

Inbound abuse report (from any source — provider, customer, member of the public, third
party) alleging that iogrid is being used in violation of the AUP. Common categories
per `legal/aup.md`:

- CSAM
- Phishing / fraud
- DDoS / network abuse
- Sanctions evasion
- Government / military / banking-target unauthorized access
- Spam (SMTP, IRC, etc.)

### 3.2 Step-by-step

1. **Receive** the report at the abuse contact (`legal/aup.md` §10.3).
2. **Triage**:
   - CSAM allegation: **P0**, mandatory NCMEC reporting flow (Section 7).
   - Active phishing / fraud: P1.
   - Other: P2 or P3 per severity.
3. **Investigate**: pull audit logs for the suspected workload(s); inspect pre-flight
   filter logs; identify the customer.
4. **Mitigate**:
   - For active high-risk content: immediately block the destination at the daemon
     level (cluster-wide filter push).
   - Suspend the customer account pending review.
   - Preserve all related records under legal hold.
5. **Report**:
   - CSAM: NCMEC CyberTipline within 24 hours of identification per 18 U.S.C. §2258A(a)
     (verify reporting-entity status per `legal/incident-response.md` §B.3 marker).
   - Other illegal content: as required by jurisdiction (NetzDG, Digital Services Act,
     Online Safety Act, etc.).
6. **Communicate**:
   - To reporter: action taken (within 5 business days per `legal/incident-response.md`
     §A.3).
   - To customer: account status, AUP violation finding, opportunity to appeal where
     applicable.
   - To affected provider(s): disposition; defense-fund-eligibility note if relevant.
7. **Document + transparency**: log incident; schedule transparency-report inclusion;
   review pre-flight filter effectiveness (was this an undetected category that should
   be added to filters?).

---

## 4. Defense-fund disbursement request

### 4.1 Incident scenario

A provider, having received a qualifying legal process (Section 1.3), requests defense-
fund disbursement to cover their legal fees.

### 4.2 Step-by-step

1. **Receive** the defense-fund-request form at the defense-fund contact
   (`legal/incident-response.md` §A.4 contact path). Provider submits the form (see
   template at Section 9.7) with:
   - Provider ID + signed claim
   - Copy of the legal process (subpoena, warrant, civil claim, LEO letter)
   - Attorney engagement letter (or "intent to engage" with named attorney)
   - Estimated fee budget
   - Provider's compliance certification (no AUP violation; timely notification)
2. **Eligibility review** (within 5 business days):
   - Confirm legal process is genuine and active.
   - Confirm provider compliance per Section 1.3.
   - Confirm provider is not on the defense-fund 24-month cooldown.
   - Confirm cap availability ($25K standard, may be exceeded with founder + legal-lead
     approval).
3. **Approval** by joint sign-off:
   - Legal lead reviews and approves up to $10K.
   - Founder + legal lead jointly approve up to $25K.
   - Founder + legal lead + Foundation supervisor approve above $25K.
4. **Disbursement**: payment direct to the provider's attorney's IOLTA / trust account.
   No direct payment to the provider (avoids self-dealing optics).
5. **Monitoring**: provider attorney provides progress updates monthly; budget reviews
   at 25%/50%/75%/100% of approved cap.
6. **Closure**: on case resolution, attorney submits final-billing reconciliation;
   surplus returned to fund; over-budget requests handled per Section 4.2 step 3.
7. **Document + transparency**: log disbursement (provider ID anonymized in public
   transparency report; aggregate $ disbursed shown).

### 4.3 Defense-fund replenishment

Per `legal/incident-response.md` §A.4, the fund is replenished by 5–10% of monthly B2B
revenue. Operations team verifies replenishment monthly; founder + legal lead notified
if fund balance drops below $5,000 USD.

---

## 5. Data breach / system-integrity incident

### 5.1 Incident scenario

iogrid systems compromised (coordinator breach, daemon-update supply-chain attack,
KYC-data theft, audit-log tampering, etc.). Or pre-flight filters failed and prohibited
content was relayed through iogrid traffic.

### 5.2 Step-by-step

1. **Receive** the alert (Sentry / Datadog detection, security researcher disclosure,
   internal employee report).
2. **Triage**: P0 by default for any breach affecting personal data, audit logs, or
   pre-flight filter integrity.
3. **Engage**:
   - Internal security team for containment.
   - Outside counsel for legal-disclosure obligation analysis (72-hour GDPR notification
     per Art. 33; state-specific data-breach-notification laws in the US; etc.).
   - Outside forensic firm if scope warrants.
4. **Contain**: stop the bleeding (rotate credentials, isolate affected systems, push
   emergency daemon update if filter-failure traces to a known CVE in the daemon, etc.).
5. **Investigate**: scope of compromise, affected data subjects, affected providers /
   customers, attacker identity if knowable.
6. **Notify**:
   - Affected data subjects per jurisdictional requirements (typically within 72 hours
     GDPR; "without unreasonable delay" CCPA; state-specific timing US).
   - Affected providers / customers.
   - Sub-processors potentially implicated.
   - Regulators (Information Commissioner's Office UK, supervisory authorities EU
     member states, state Attorneys General US, FTC US if applicable).
   - Foundation supervisors (per `legal/foundation/foundation-rules.md` governance).
7. **Remediate**: fix root cause; deploy patches; update audit-log integrity checks.
8. **Document**: comprehensive post-incident report; redacted version published in
   next transparency report; root-cause analysis circulated internally.
9. **Schedule** transparency-report inclusion (Section 8).

---

## 6. Regulator inquiry

### 6.1 Incident scenario

A regulator (SEC, CFTC, FTC, state AG, ICO, EU supervisory authority, FINMA, FCA, etc.)
opens an inquiry into iogrid. May begin as a Civil Investigative Demand, a no-action
letter request rejection, a published guidance update naming iogrid, or a more formal
enforcement action.

### 6.2 Step-by-step

1. **Receive** the regulator contact (formal letter, email from agency address, court
   filing).
2. **Triage**: P0 by default for any regulator inquiry. Acknowledge within 24 hours;
   substantive response per the inquiry's stated deadline.
3. **Engage**:
   - Outside counsel with regulator-specific expertise (US securities = Cooley / Fenwick
     / Davis Polk / Latham; EU data = Bird & Bird; UK FCA = Bird & Bird or specialist;
     etc.).
   - Founder + Foundation supervisor briefing.
4. **Coordinate** response strategy with counsel:
   - Information-production timeline.
   - Privilege claims (attorney-client, work-product).
   - Document-hold scope.
5. **Produce** responsive documents under counsel supervision.
6. **Communicate** with the regulator only through counsel; do NOT engage in informal
   communications.
7. **Document + transparency**: log the inquiry; transparency-report inclusion subject
   to gag-order / non-disclosure constraints in the inquiry. Where transparency is
   permissible, include aggregate counts (e.g., "iogrid received 1 SEC inquiry in Q3
   2026; status: pending").

---

## 7. NCMEC CyberTipline reporting flow

### 7.1 Trigger

Pre-flight filter (CSAM hash check against NCMEC PhotoDNA + INTERPOL hash list) fires
OR a credible report from any source identifies CSAM transiting iogrid traffic OR
iogrid otherwise obtains actual knowledge of CSAM.

### 7.2 Reporting obligation

Per `legal/incident-response.md` §B.3 and `legal/aup.md` §2.1:

- iogrid's status as a reporting entity under 18 U.S.C. §2258A must be confirmed by
  counsel before Phase 1 launch (open `[COUNSEL: ...]` marker — see
  `legal/counsel/issue-list.md`).
- If status is confirmed as "electronic communication service provider" or "remote
  computing service provider" with actual knowledge, **mandatory** reporting within 24
  hours of obtaining knowledge.
- Reports filed via the NCMEC CyberTipline (cybertipline.org or API per NCMEC
  documentation).
- Retain records of every report for the longer of: (a) 90 days from report (NCMEC
  baseline) or (b) the duration of any related law-enforcement investigation iogrid is
  aware of.

### 7.3 Reporting steps

1. **Identify** the CSAM incident (filter fire OR third-party report verified).
2. **Block** the destination at the daemon level immediately.
3. **Suspend** the customer account; preserve all related records under indefinite
   legal hold.
4. **Notify** founder + legal lead (P0 escalation per Section 0).
5. **File** NCMEC CyberTipline report within 24 hours. Required information:
   - Suspect identity (customer KYC data)
   - Destination URL / content identifiers
   - Timestamps + IP information (provider IP + customer payment / KYC)
   - Hash matches (from pre-flight filter)
   - Other relevant context
6. **Notify** the affected provider that LEO follow-up may occur and that defense-fund
   coverage attaches if the provider did not participate in the violation.
7. **Notify** the registered agent / Cayman counsel if any duty attaches under Cayman
   law.
8. **Document** + retain records per Section 7.2.

### 7.4 Do NOT

- Do NOT engage in any back-and-forth communication with the suspected uploader; report
  to NCMEC and let LEO handle.
- Do NOT delete the underlying content; preserve under legal hold.
- Do NOT publish the incident in a transparency report without LEO coordination (active
  investigation = silence until cleared).

---

## 8. Transparency-report addendum drafting

### 8.1 Cadence

Per `legal/incident-response.md` §B.6, target cadence is quarterly starting Phase 2 (end
of 2026). Each report covers the prior quarter.

### 8.2 Standard content

Each transparency report contains aggregate counts of:

- Law-enforcement contacts received (by type: subpoena, warrant, court order, NSL,
  international MLAT)
- Subpoenas responded to / objected to / quashed
- Customer accounts suspended for AUP violations (by AUP section)
- Provider accounts suspended for ToS violations
- Defense-fund disbursements (aggregate $; counts; no provider identification)
- NCMEC reports filed (counts; aggregate)
- Data-subject requests processed (access, deletion, portability)
- Data-breach incidents disclosed
- Regulator inquiries (where disclosure permitted)
- Sub-processor changes
- Pre-flight-filter aggregate statistics (blocks per category)

### 8.3 Drafting steps

1. **Compile** the prior quarter's incident logs from the legal-ticket system.
2. **Aggregate** into counts per Section 8.2 categories.
3. **Anonymize**: no provider / customer identification; no destination identification
   beyond category (e.g., "phishing destinations: 1,234 blocked" not "phishing-site-X
   blocked Y times").
4. **Draft** the report using the template at Section 9.5.
5. **Counsel review**: outside counsel reviews for litigation-exposure + disclosure-
   compliance.
6. **Founder + supervisor sign-off**: per `legal/foundation/foundation-rules.md`.
7. **Publish** at the iogrid transparency URL (see `legal/incident-response.md` §B.6
   marker for canonical URL once finalized).
8. **Archive** prior reports; never delete.

### 8.4 Addendum drafting (out-of-cycle)

If a material incident occurs between scheduled reports AND counsel advises disclosure
is appropriate AND the incident is not under gag order, draft an out-of-cycle addendum:

1. **Scope** the addendum: what specific event, what specific data.
2. **Draft** per template at Section 9.5 (abbreviated for single-incident scope).
3. **Counsel review** + founder + supervisor sign-off (faster timeline; 48–72 hour
   target).
4. **Publish** as a transparency-report addendum at the same URL with date stamp.

---

## 9. Templates

### 9.1 Provider acknowledgement letter (LEO contact)

```text
Subject: iogrid — receipt of your law-enforcement contact report (ticket #[ID])

Hello [Provider name],

We have received your forwarded law-enforcement contact and are working on it.

Your incident ticket: #[ID]
Reported at: [timestamp UTC]
Severity assigned: [P0 / P1]
Owner: [legal-lead name + email]
Outside counsel engaged: [yes / pending]

What happens next:

  1. We will pull the audit-log records for the time-window described in the LEO
     contact within the next [24 hours / 4 business hours].
  2. We will contact the issuing authority directly (with your consent if you are
     not legally compelled to involve us) and provide the customer-identifying
     information for the traffic in question.
  3. We will keep you updated by email at this address.
  4. If your contact requires legal representation, the iogrid Legal Defense Fund
     may cover your reasonable legal fees up to $25,000 USD per
     legal/incident-response.md §A.4. We will assess your eligibility within
     5 business days and respond.

In the meantime:

  * Do NOT uninstall the daemon, destroy data, or tamper with your device.
  * Do NOT answer substantive LEO questions about traffic content or customer
    identity — you may legitimately defer to iogrid on these (you do not know the
    answers; we do).
  * You ARE entitled to consult counsel before substantive engagement; the defense
    fund covers reasonable counsel fees per the criteria above.
  * If LEO is at your door RIGHT NOW, call us at [URGENT phone — see
    legal/incident-response.md §B.1 for the line to fill in].

Best regards,
iogrid legal team
[ticket-system signature]
```

### 9.2 LEO-response letter

```text
[On iogrid letterhead]

[Date]

[Issuing Authority]
[Address]

Re: [Subpoena / Warrant / Letter reference number]
    iogrid response

Dear [Officer / Agent / Counsel],

We received the above-referenced process on [date]. iogrid is an operating entity
described at https://github.com/iogrid/iogrid. This letter constitutes our response
to the lawful process.

1. Records produced

  We enclose audit-log records identifying the iogrid Customer responsible for the
  traffic described in your process, scoped to the time-window and IP/Provider
  identifiers specified. These records include:

    * Customer name + KYC verification level
    * Customer billing entity + payment instrument
    * Workload type + destination identifier(s)
    * Timestamps + traffic volume
    * Pre-flight filter logs (CSAM, phishing, port restrictions)

2. Records preserved

  We have placed all related records under legal hold and will preserve them
  pending resolution of your investigation.

3. Provider status

  The Provider (the individual or entity hosting the iogrid daemon) is identified
  in our records but did NOT initiate or direct the traffic described. Per our
  Provider Terms of Service, the Provider operates as a passive bandwidth
  intermediary and does not have actual knowledge of customer traffic content.
  We respectfully request that any follow-up regarding the underlying conduct be
  directed to the Customer identified above.

4. Objections

  [Any objections counsel chooses to make, e.g., overbreadth, jurisdiction,
  privilege, etc.]

5. Contact

  For any follow-up, please contact:

    [Legal lead name]
    [Outside counsel name + firm + email + phone]

Sincerely,

[Legal lead signature block]
iogrid legal team
```

`[COUNSEL: review this template carefully before transmission. Counsel may
substantially restructure; some firms prefer no record-by-record production in the
response letter and instead a transmittal cover-letter referencing a separate document
production. Counsel decides format.]`

### 9.3 Provider disposition letter

```text
Subject: iogrid — disposition of your LEO contact (ticket #[ID])

Hello [Provider name],

Update on your incident ticket #[ID].

What we did:

  1. We pulled audit-log records for the time-window in the LEO contact.
  2. We identified the iogrid Customer responsible for the traffic.
  3. We contacted the issuing authority on [date] and provided the customer-
     identifying information.
  4. [Customer status: AUP-compliant / AUP-violator / under further review.]

Your status:

  [Choose:]

  (a) Defense-fund eligible. You meet all four eligibility criteria
      (legal/incident-response.md §A.4). Disbursement is approved for up to
      $25,000 USD against attorney fees + reasonable costs. Please complete the
      defense-fund-request form attached and submit your attorney's engagement
      letter.

  (b) Defense-fund not applicable. The LEO contact appears to have been resolved
      by our direct response to the issuing authority; you should not need to
      retain counsel. We will let you know if anything changes. If you nevertheless
      wish to retain counsel for your own peace of mind, we can recommend local
      counsel (see template Section 9.4).

  (c) Defense-fund ineligible. Audit-log analysis indicates [reason — e.g., the
      provider participated in the AUP violation; the provider failed to notify
      within 5 business days; etc.]. We're sorry we can't help on the fund side.
      You may still retain counsel at your own expense.

Next steps:

  [Follow-up timeline per case.]

Best regards,
iogrid legal team
```

### 9.4 Local-counsel referral list

```text
Subject: iogrid — local-counsel recommendations for your jurisdiction

Hello [Provider name],

For your [country / state] jurisdiction, here are counsel we have worked with or
that have been recommended to us for matters of this type:

  1. [Firm name 1] — [partner contact + email]
  2. [Firm name 2] — [partner contact + email]
  3. [Firm name 3] — [partner contact + email]

Each is independent and represents the client (you), not iogrid. We make no
representation about the quality of their work or their willingness to take your
case. Please vet thoroughly.

If you prefer to engage your own counsel, that is fine; the defense fund (where
applicable) will reimburse reasonable attorney fees regardless of which counsel
you engage.

Best regards,
iogrid legal team
```

### 9.5 Transparency report template

```markdown
# iogrid transparency report — [quarter, year]

**Reporting period:** [QN YYYY], [start date] – [end date]
**Published:** [date]
**Signing supervisor (Foundation):** [name]

---

## Summary

[2–3 sentences of headline content for the quarter.]

## Law-enforcement contacts received

| Type | Count | Responded | Objected | Quashed |
|------|-------|-----------|----------|---------|
| Subpoena (US federal) | N | N | N | N |
| Subpoena (US state) | N | N | N | N |
| Subpoena (non-US) | N | N | N | N |
| Court order | N | N | N | N |
| Search warrant | N | N | N | N |
| National Security Letter | * | * | * | * |
| International MLAT | N | N | N | N |
| Informal LEO inquiry | N | N | N | N |

*"NSL" entries are subject to gag-order constraints. See warrant-canary disclosure
language below.

## AUP enforcement actions

| AUP section | Count of customer suspensions | Count of customer terminations |
|-------------|-------------------------------|--------------------------------|
| 2.1 CSAM | N | N |
| 2.2 Phishing / fraud | N | N |
| 2.3 DDoS / network abuse | N | N |
| 2.4 Sanctions evasion | N | N |
| 2.5 Government / banking targets | N | N |
| 2.6 Spam | N | N |
| 5.x (other categories) | N | N |

## Provider enforcement actions

| Reason | Count |
|--------|-------|
| ToS §X violation | N |
| Voluntary termination | N |
| Failure to maintain anti-abuse filters | N |

## Defense-fund disbursements

| Disbursement category | Count | Aggregate USD |
|------------------------|-------|---------------|
| Defense-fund disbursements | N | $X |
| Average per disbursement | — | $Y |
| Fund balance at quarter-end | — | $Z |

## NCMEC reports filed

| Category | Count |
|----------|-------|
| Reports filed | N |
| Confirmed CSAM (post-NCMEC verification) | N |

## Data-subject requests processed

| Type | Count | Median response time |
|------|-------|----------------------|
| Access (GDPR Art. 15) | N | X days |
| Deletion (GDPR Art. 17) | N | X days |
| Portability (GDPR Art. 20) | N | X days |
| CCPA "do not sell" | N | X days |

## Data-breach incidents

[Count of incidents; aggregate scope; remediation status. Detail only as far as
ongoing investigation permits.]

## Regulator inquiries

[Aggregate counts where disclosure is permissible. Specific inquiries listed only
where the inquiry has resolved.]

## Sub-processor changes

[List additions / removals per DPA Annex 3 update obligations.]

## Pre-flight filter statistics (aggregate)

| Filter category | Blocks |
|-----------------|--------|
| CSAM hash match (NCMEC / INTERPOL) | N |
| Phishing destinations | N |
| Government / military targets | N |
| Port-restriction enforcement | N |

## Warrant-canary status

[Per `legal/incident-response.md` §B.7. The standard statement is "iogrid has not
received any national-security letter or other gag-ordered process during this
reporting period." Absence of this statement = removal of the canary = the
operative legal signal.]

Signed: [Foundation supervisor name + signature]
Date: [date]
Public-key signature: [signature block — Ed25519 typical]
```

### 9.6 Internal incident-report template

```text
INCIDENT REPORT

Ticket ID: #[ID]
Severity: [P0 / P1 / P2 / P3]
Type: [LEO contact / Customer complaint / Abuse report / Defense-fund request /
       Data breach / Regulator inquiry / NCMEC report / Other]
Opened: [timestamp UTC]
Owner: [legal-lead name]
Outside counsel: [firm + partner if engaged]

Summary:
[2–3 sentences]

Affected parties:
  Provider(s): [IDs]
  Customer(s): [IDs]
  Sub-processor(s): [names]
  Other: [LEO authority, regulator, target platform, etc.]

Timeline:
  [Timestamp] — Event
  [Timestamp] — Event
  ...

Records preserved:
  [Audit-log buckets + retention markers]
  [Email threads]
  [LEO communications]

Disposition:
[What was decided / done]

Defense-fund implication:
  Eligible: [yes / no / partial]
  Disbursed: [$ amount]
  Approver(s): [names]

Public transparency-report inclusion:
  Yes / No (with reason)
  Aggregate-only / Specific (with reason)

Closed: [timestamp UTC]

Lessons learned + process improvements:
[1–3 bullets]
```

### 9.7 Defense-fund request form

```text
iogrid Legal Defense Fund — disbursement request

1. Provider information
   Provider ID:
   Legal name:
   Email:
   Phone:

2. Legal process information
   Type: [subpoena / warrant / civil claim / LEO letter / other]
   Issuing authority:
   Date received:
   Process attached: [yes — attach copy]

3. Notification timeliness
   Date iogrid was notified by you: [date]
   Was notification within 5 business days of receiving process? [yes / no]
   If no, please explain:

4. Attorney information
   Attorney name:
   Firm:
   Bar admission state(s) / jurisdiction:
   Email + phone:
   Engagement letter attached: [yes — attach copy]
   IOLTA / trust account information for direct fee payment:

5. Estimated fee budget
   Initial estimate (anticipated through next 90 days):
   Estimated peak total if matter goes to trial:

6. Compliance certification
   By signing this form, I certify:
     (a) I did not knowingly violate the iogrid Acceptable Use Policy.
     (b) I am not aware of any iogrid customer who violated the AUP in connection
         with the underlying traffic, and I have not facilitated any such violation.
     (c) The legal process described in Section 2 is genuine and is currently
         active against me.
     (d) I have not previously received a defense-fund disbursement within the past
         24 months.
     (e) I will keep iogrid informed of the matter's progress and provide reasonable
         updates on fee burn rate.

Signed: [Provider signature]
Date:

For iogrid use only

Eligibility verified: [legal-lead name + date]
Approval level: [legal-lead / founder + legal-lead / founder + supervisor]
Approved amount: [$]
Approved date:
Disbursement-to: [IOLTA account info]
```

### 9.8 Provider-facing abuse-report acknowledgement

```text
Subject: iogrid — your abuse report (ticket #[ID])

Hello [Reporter name],

Thank you for reporting an abuse incident to iogrid. We take these reports
seriously.

Ticket: #[ID]
Severity assigned: [P0 / P1 / P2 / P3]
Owner: [legal-lead name]
Target acknowledgement: [date — within 5 business days]

What we will do:

  1. We will pull audit logs for the workload(s) you identified.
  2. We will identify the iogrid Customer responsible and verify against the AUP.
  3. If the AUP is violated, we will suspend or terminate the Customer account
     and pursue any required reporting (NCMEC for CSAM, etc.).
  4. We will respond to you with the action taken.

If you reported anonymously, we will publish action in our quarterly transparency
report (aggregate; no reporter identification).

Thank you,
iogrid legal team
```

---

## 10. Maintenance + improvement

### 10.1 Quarterly review

The legal lead reviews this playbook quarterly. Trigger reviews on:

- Any P0 incident handled in the prior quarter (lessons-learned integration).
- New regulatory guidance affecting iogrid operations.
- New AUP categories or pre-flight filter additions.
- Outside-counsel feedback on incident handling.

### 10.2 Drill exercises

At least annually, the legal lead runs a tabletop drill exercise covering each of
the 8 incident categories in Sections 1–8. Drill participants:

- Legal lead
- Founder (for P0 escalation paths)
- Engineering rep (for audit-log + system-integrity scenarios)
- Customer-success rep (for customer-side communication)
- Outside counsel (annually attendance recommended; quarterly check-in regardless)

Drill outputs:

- Timing measurements (acknowledge / engage / disposition).
- Process-gap identification.
- Template updates.

### 10.3 Version control

This playbook lives in the public `legal/incident-response/` directory. Substantive
amendments are committed with detailed commit messages and reviewed by counsel.
Operational templates in Section 9 may be amended without counsel review if the change
is clerical (e.g., updating a contact email); substantive changes require counsel
review.

---

`[COUNSEL: full document review required. Key open items: NCMEC reporting-entity
status confirmation drives Section 7 (open marker in legal/incident-response.md
§B.3); LEO-response letter template (Section 9.2) should be carefully reviewed
before any actual transmission; defense-fund approval thresholds (Section 4.2 step 3)
are operational defaults and may need adjustment per Foundation governance.
Transparency-report content scope (Section 8.2) should be confirmed against
gag-order obligations once iogrid has received any actual gag-ordered process.]`

*End of incident-response operational playbook.*
