# Privacy Policy

**Status:** Draft v0.1 — pre-counsel-review. Not legal advice. See [`legal/README.md`](./README.md).
**Effective date:** *[COUNSEL: insert effective date upon finalization]*
**Last updated:** 2026-05-19 (draft)
**Operating entity:** *[COUNSEL: confirm finalized entity]*

> **Plain-language summary** *(non-binding).*
>
> When you use iogrid as a provider or customer, we collect the data we need to operate the network and pay you: bandwidth volume per device, uptime, approximate location (country / city), and contact / billing info. We do NOT read the content of your traffic — we never decrypt HTTPS. We share data with sub-processors listed below (payment, hosting, anti-abuse). We respond to subpoenas. We keep audit logs for 90 days. You have access, deletion, portability, and other rights under GDPR / CCPA / LGPD. Email privacy@iogrid.org to exercise them.

---

## 1. Who we are

The entity controlling your personal data is *[COUNSEL: insert finalized entity name and registered address]* ("**iogrid**," "**we**," "**us**," "**our**"). Contact details are in Section 12.

For data subjects in the EEA, UK, and Switzerland, our representative under Article 27 GDPR / UK GDPR is *[COUNSEL: insert — likely VeraSafe, DataRep, or equivalent vendor]*.

---

## 2. Scope

This Privacy Policy explains how we collect, use, share, and protect personal data of:
- **Providers** — individuals or legal entities who install the iogrid Daemon (separately, see Provider Terms of Service);
- **Customers** — individuals or legal entities who use iogrid as a service (separately, see Customer Terms of Service);
- **Website visitors** — anyone visiting iogrid.org, docs.iogrid.org, and other iogrid-operated web properties;
- **End users incidentally observed** — individuals whose data is encountered as a side-effect of a Workload (e.g., a Customer's scraping target's user profile). **For these individuals, the Customer is generally the Controller and we are the Processor — see the [DPA](./dpa.md).**

This policy does NOT cover:
- The websites or services of third parties (linked from our pages or accessed via Workloads — those have their own policies);
- The handling of data after it leaves iogrid (e.g., once we hand it to a sub-processor on documented instructions, the sub-processor's own policy may apply).

---

## 3. What we collect

### 3.1 From Providers

When you sign up and run the Daemon, we collect:

- **Account information** you submit: name, email address, country of residence, payout method, and (where required by KYC) government-ID image, business registration document, tax ID, and other identity-verification data;
- **Device telemetry** (collected by the Daemon and forwarded to the Coordinator):
  - Bandwidth volume per Workload (bytes in / bytes out);
  - Uptime statistics;
  - Approximate geographic location (country and city, derived from your public IP — never a street address);
  - Device hardware fingerprint: OS family and version, CPU model, GPU model, RAM size — used for Workload-fit scheduling;
  - Daemon version and any runtime error or crash diagnostic data;
- **Audit Log entries** in respect of Workloads routed through your Device (the Customer ID, destination domain, timestamps, byte volumes — see Provider ToS Section 6.2);
- **Wallet address** (if you elect $GRID payouts) — your Solana wallet public key;
- **Payout history** — amounts and dates of cash, $GRID, VPN-credit, or charity-donation payouts.

### 3.2 From Customers

When you sign up and use the Service, we collect:

- **Account information** you submit: name, email, billing address, payment method (Stripe customer ID, USDC wallet address, or $GRID wallet);
- **KYC documents** (Pro / Enterprise tiers): government-ID image, business registration, principal-of-business identification, AML check results;
- **API usage data**: API request logs, rate-limit counters, billing-relevant volumes;
- **Workload inputs** — the content the Customer submits to the Service (container images, source code, command-line arguments, request bodies). We process this content to provide the Service; we do not analyze or aggregate it for any other purpose.

### 3.3 From website visitors

When you visit our websites, we collect:

- **Server-log data**: IP address, user-agent, referrer URL, requested page, timestamp;
- **Cookies and similar identifiers**: see Section 8 (Cookies).

We do not use third-party analytics on iogrid-operated web properties. *[COUNSEL: confirm. If Plausible Analytics, Fathom, or any other "cookieless" analytics is in use, list and disclose.]*

### 3.4 What we do NOT collect

- **Content of HTTPS traffic.** We forward encrypted bytes through Provider Devices without decryption. We have no technical means to read the substantive content of HTTPS traffic.
- **Content of customer container payloads, logs, or files** beyond what is necessary to provide the Service. We do not aggregate or analyze container contents across Customers.
- **Audio, video, microphone, or camera input** from Provider Devices.
- **Files on Provider Devices outside the Daemon's installation directory.**
- **Personal browsing history of Provider users** (traffic generated by the Device outside the Daemon's routing scope).

---

## 4. Why we collect it (purposes of processing)

| Purpose | Categories of data | Lawful basis (GDPR Art. 6) |
|---------|--------------------|----------------------------|
| Operate the iogrid network (route Workloads, schedule, log) | Telemetry, Audit Logs, account data | Contract performance (Art. 6(1)(b)) |
| Calculate and pay out compensation | Telemetry, payout data, payment method | Contract performance |
| Bill Customers | Account data, API usage, payment method | Contract performance |
| Comply with KYC / AML obligations | KYC documents | Legal obligation (Art. 6(1)(c)) — applicable to Stripe-Connect-mediated payouts and certain Customer tiers |
| Tax reporting (1099 issuance, etc.) | Payout history, account data | Legal obligation |
| Anti-abuse filtering | Workload destinations, telemetry | Legitimate interest (Art. 6(1)(f)) — operating a lawful, abuse-resistant network |
| Respond to legal process | Audit Logs, account data | Legal obligation |
| Defend Providers against legal claims (per Provider ToS Section 5) | Audit Logs | Contract performance + legitimate interest |
| Operate the websites | Server-log data, cookies | Legitimate interest |
| Communicate updates, support responses | Email, account data | Contract performance |
| Marketing (newsletters, product announcements) | Email | Consent (Art. 6(1)(a)) — opt-in only, opt-out available any time |
| Aggregate analytics (non-personal) | Anonymized aggregates | Legitimate interest |

*[COUNSEL: lawful-basis mapping should be confirmed jurisdiction-by-jurisdiction. CCPA / CPRA does not use the GDPR lawful-basis framework but requires notice-at-collection and disclosure of "categories of personal information," "purposes of use," and "categories of third parties." Brazilian LGPD has its own 10 lawful bases under Article 7. Australian Privacy Act, Canadian PIPEDA, etc. — all need confirmation.]*

---

## 5. Who we share data with

We share personal data only as described in this section.

### 5.1 Sub-processors

We use the sub-processors listed in [`legal/dpa.md`](./dpa.md) Annex 3 to perform specific functions. Each sub-processor is bound to terms substantially equivalent to those in our DPA and may not use the data for any purpose other than to provide their service to us.

Key sub-processor categories:
- **Payments:** Stripe (cash payouts and Customer billing); MoonPay (token off-ramp);
- **KYC / AML:** Sumsub or Persona;
- **Hosting:** Hetzner, AWS;
- **Anti-abuse:** NCMEC, PhishTank, OpenPhish, Google Safe Browsing;
- **Token infrastructure:** Solana RPC providers, the Solana network itself (public blockchain);
- **DDoS / CDN:** Cloudflare;
- **Observability:** *[COUNSEL: confirm vendor — likely Datadog or Sentry]*.

### 5.2 Law enforcement and government requests

We comply with valid legal process (subpoenas, court orders, warrants, MLAT requests). Our standard handling:
- Verify the process is valid (issuing authority, proper service, scope);
- Where the process names a Provider IP / Device, identify the Customer whose Workload generated the traffic and respond with the Customer's identifying information;
- Notify the Provider whose IP is named, **unless legally prohibited from doing so** (e.g., a non-disclosure order accompanies the process);
- Object to overbroad or facially defective requests;
- Where we are not US-based, route US legal process through our outside counsel; route MLAT requests likewise.

We will publish quarterly transparency reports starting Phase 2 listing aggregate counts of requests received, jurisdictions, and compliance rate.

### 5.3 Business transfers

If we are acquired, merged, or otherwise undergo a business transition, personal data may be transferred to the acquirer or successor entity, subject to (a) the same purposes and limitations as in this Privacy Policy, or (b) a successor policy that you accept.

### 5.4 With your direction or consent

We may share data with third parties at your direction (e.g., when you connect a third-party app via OAuth) or with your specific consent.

### 5.5 What we do NOT share

- **We do not sell personal data** (within the meaning of "sale" under CCPA § 1798.140(t) or analogous laws).
- **We do not share personal data with advertisers** for targeting.
- **We do not share Audit Logs with Customers about which Customers' traffic flowed through a given Provider IP** — Audit Logs are accessible only to (a) iogrid personnel with a need to know, (b) the Provider whose IP is named (limited to entries naming their IP), (c) the Customer who initiated the Workload (limited to their own Workloads), (d) law-enforcement under valid process.

---

## 6. How long we keep data

| Data | Retention period |
|------|------------------|
| Audit Log entries (per-Workload records) | **90 days** rolling, then deleted (anonymized aggregates retained indefinitely) |
| Daemon telemetry (per-Workload volumes, uptime) | 90 days for per-Workload granularity; anonymized aggregates indefinite |
| Account data | Duration of the contractual relationship plus 7 years (or longer where required by tax / KYC / AML law) |
| Payment records | 7 years (US tax requirement, similar in many jurisdictions) |
| KYC documents | 5 years post-relationship end (typical AML retention) — *[COUNSEL: confirm per applicable AML law]* |
| Marketing-consent log | Duration of consent + 3 years post-withdrawal |
| Website server logs | 30 days |
| Backups | Audit Log backups retained for 90 days max; other backups 30 days |

Where you exercise a right of erasure (Section 9.4), we honor it within the time required by applicable law, but we may retain certain data where retention is required by law (e.g., tax records) or necessary to defend legal claims.

---

## 7. International transfers

Some of our sub-processors are based outside the EEA, UK, and Switzerland (notably in the United States — see DPA Annex 3). For such transfers, we rely on:
- **Standard Contractual Clauses (EU SCCs)** under Implementing Decision (EU) 2021/914;
- **UK International Data Transfer Addendum** (or UK IDTA standalone form);
- **Swiss SCCs** as modified per FDPIC guidance;
- **EU-US Data Privacy Framework** certifications where the sub-processor is certified.

You may request a copy of the safeguards in place by emailing *[COUNSEL: insert privacy contact]*.

---

## 8. Cookies and tracking

### 8.1 Essential cookies

We use cookies and similar technologies that are strictly necessary for the websites and the management plane to function:
- Authentication / session cookies;
- CSRF tokens;
- Load-balancer affinity cookies.

These cookies do not require consent under the EU ePrivacy Directive (Article 5(3) "strictly necessary" exemption).

### 8.2 Non-essential cookies and trackers

**We do not use non-essential cookies, third-party advertising trackers, or session-replay technology on iogrid-operated web properties.** If this changes, we will update this section and obtain consent where required.

*[COUNSEL: confirm. If any optional analytics is added later (Plausible, Fathom, etc.), update language and consent flow.]*

### 8.3 Do Not Track

We honor the W3C Do Not Track signal where technically applicable. Given Section 8.2, the practical effect is unchanged.

---

## 9. Your rights

Subject to applicable law (GDPR, UK GDPR, Swiss FADP, US state laws including CCPA / CPRA, Brazilian LGPD, Quebec Law 25, Australian Privacy Act, Canadian PIPEDA, and others), you have the following rights regarding your personal data:

### 9.1 Right to be informed

You have the right to be informed about how we process your data — this Privacy Policy is the primary instrument for this right.

### 9.2 Right of access (GDPR Art. 15, CCPA § 1798.110, LGPD Art. 18)

You have the right to know what personal data we hold about you and to receive a copy.

### 9.3 Right of rectification (GDPR Art. 16, LGPD Art. 18)

You have the right to have inaccurate data corrected. You can update most account data directly in the management plane.

### 9.4 Right of erasure / "right to be forgotten" (GDPR Art. 17, CCPA § 1798.105, LGPD Art. 18)

You have the right to request deletion of your data, subject to legal-retention requirements (Section 6).

### 9.5 Right to restrict processing (GDPR Art. 18)

You have the right to ask us to restrict processing of your data pending resolution of a dispute about its accuracy or our right to process.

### 9.6 Right to data portability (GDPR Art. 20, CCPA § 1798.130, LGPD Art. 18)

You have the right to receive your personal data in a structured, commonly used, machine-readable format (we provide JSON exports).

### 9.7 Right to object (GDPR Art. 21, LGPD Art. 18)

You have the right to object to processing based on legitimate interests or for direct marketing.

### 9.8 Rights related to automated decision-making (GDPR Art. 22)

We do not make solely-automated decisions with significant effects on Providers or Customers. Our scheduling algorithm is fully automated but its outputs (Workload dispatch decisions) do not have legal or similarly significant effects on data subjects.

### 9.9 Right to withdraw consent (GDPR Art. 7(3))

Where processing is based on consent (e.g., marketing emails), you may withdraw consent at any time. Withdrawal does not affect the lawfulness of processing before withdrawal.

### 9.10 Right to lodge a complaint with a supervisory authority (GDPR Art. 77)

You have the right to complain to your national data-protection authority. A list is available at *https://edpb.europa.eu/about-edpb/about-edpb/members_en*. For UK residents, the Information Commissioner's Office (ICO) at *ico.org.uk*. For Swiss residents, the FDPIC at *edoeb.admin.ch*.

### 9.11 California-specific rights (CCPA / CPRA)

California residents have additional rights, including:
- The right to know the categories of personal information we collect, the categories of sources, the purposes, and the categories of third parties to whom we disclose (covered in Sections 3, 4, 5);
- The right to opt out of any "sale" or "sharing" of personal information for cross-context behavioral advertising — **we do not sell or share personal information for these purposes** (Section 5.5);
- The right to limit use of sensitive personal information;
- The right to non-discrimination for exercising CCPA rights.

### 9.12 Brazilian LGPD rights

LGPD-specific rights include (in addition to the above) the right to information about whether we process the data subject's personal data, the right to know with whom we share, and the right to anonymization, blocking, or deletion of unnecessary or excessive data (Article 18).

### 9.13 How to exercise rights

Email *[COUNSEL: insert, e.g., privacy@iogrid.org]* with the request, your account email, and any details that help us locate the relevant records. We will respond within:
- **1 month** for GDPR / UK GDPR requests (extendable by 2 months for complex requests with notification);
- **45 calendar days** for CCPA requests (extendable by 45 days with notification);
- **15 days** for LGPD requests;
- Or shorter periods where applicable law requires.

We may need to verify your identity before responding. We will not charge a fee unless the request is manifestly unfounded or excessive.

---

## 10. Security

We implement technical and organizational measures appropriate to the risk, as described in [`legal/dpa.md`](./dpa.md) Annex 2. These include encryption in transit and at rest, role-based access control, multi-region replication, vulnerability monitoring, and an incident-response process.

No security measure is perfect. If you discover a vulnerability, please report it to *[COUNSEL: insert security contact, e.g., security@iogrid.org]*. We operate a coordinated-disclosure / responsible-disclosure program (Phase 2: bug bounty).

---

## 11. Children

iogrid is not intended for and is not marketed to children under the age of 18 (or higher age of majority, as applicable). We do not knowingly collect personal data from children. If you believe we have inadvertently collected such data, contact us and we will delete it.

*[COUNSEL: confirm — Section 2 of Provider / Customer ToS requires age 18+. Some jurisdictions distinguish children-under-13 (US COPPA), children-under-16 (default GDPR Art. 8 unless member-state-modified), children-under-14 (Brazil LGPD Art. 14). The "18+" service-wide policy avoids these specific carve-outs.]*

---

## 12. Contact

- **Privacy / data subject rights:** *[COUNSEL: insert, e.g., privacy@iogrid.org]*
- **Data Protection Officer:** *[COUNSEL: insert, where appointed]*
- **EU representative (Art. 27 GDPR):** *[COUNSEL: insert vendor + address]*
- **UK representative (Art. 27 UK GDPR):** *[COUNSEL: insert]*
- **Postal address:** *[COUNSEL: insert finalized registered address]*

---

## 13. Updates to this policy

We may update this Privacy Policy from time to time. Material updates will be notified via email to the address on your account (where you have one) and / or by prominent notice on iogrid.org at least **30 days** before the effective date. The current version is always published at https://iogrid.org/legal/privacy.

The revision history is maintained in [`legal/README.md`](./README.md).

---

*End of Privacy Policy v0.1-draft.*

*[COUNSEL: full-document review required. Key open items: lawful-basis mapping completeness (Section 4), GDPR vs. CCPA vs. LGPD harmonization (Section 9), Art. 27 representative appointment (Section 12), CCPA "sensitive personal information" notice carve-out review (Section 9.11), cookie-and-tracker disclosure completeness (Section 8), Article 22 "automated decision-making" analysis (Section 9.8). Total `[COUNSEL]` markers: ~15.]*
