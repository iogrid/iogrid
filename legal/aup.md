# Acceptable Use Policy (AUP)

**Status:** Draft v0.1 — pre-counsel-review. Not legal advice. See [`legal/README.md`](./README.md).
**Effective date:** *[COUNSEL: insert effective date upon finalization]*
**Incorporated by reference into:** Provider Terms of Service, Customer Terms of Service.

> **Plain-language summary** *(non-binding).*
>
> Use iogrid for normal, legal business — price monitoring, SEO, ad checks, lead-gen, AI training. Do not use iogrid for anything illegal, anything that harms people (CSAM, trafficking, attacks on hospitals or power grids), anything that defrauds or DDoSes anyone, or anything that misuses .gov / .mil / banking domains. We have hard, unconditional blocks. We have soft blocks you can sometimes unlock with KYC. We have allowed uses. Read the lists. If you're unsure, ask before you ship.

---

## 1. Scope and binding effect

This AUP applies to:
- **Providers** — anyone who has accepted the [Provider Terms of Service](./provider-tos.md), regardless of how they receive compensation (cash, VPN, charity, $GRID);
- **Customers** — anyone who has accepted the [Customer Terms of Service](./customer-tos.md);
- **Any third party** using or attempting to use the iogrid network through any means.

Violations are grounds for immediate suspension or termination of the violating account, forfeiture of indemnification (for Providers), forfeiture of refunds (for Customers), and referral to law enforcement and / or applicable reporting bodies where the violation involves criminal conduct.

iogrid retains sole discretion to determine whether conduct violates this AUP, subject only to the dispute-resolution mechanism in the applicable ToS.

---

## 2. Absolutely prohibited (zero-tolerance)

The following uses of iogrid are absolutely prohibited. **Engaging in any of these results in immediate, permanent termination, retention of all Audit Logs, mandatory reporting to relevant authorities, and full cooperation with law enforcement.** No appeal, no exception, no second chance.

### 2.1 Child sexual abuse material (CSAM)

**Zero tolerance.** Any traffic that transmits, requests, hosts, links to, advertises, or facilitates access to child sexual abuse material is absolutely prohibited.

- Outbound destinations are filtered against the National Center for Missing & Exploited Children (NCMEC) PhotoDNA hash database and the INTERPOL hash list before any request is forwarded. *[COUNSEL: confirm iogrid's status as a registered NCMEC partner / ESP under 18 U.S.C. §2258A. The reporting obligation in §2258A applies to "electronic communication service providers" and "remote computing service providers" with actual knowledge — confirm whether iogrid qualifies and the consequences of qualifying (mandatory reporting is then a legal duty, not a courtesy).]*
- All confirmed or reasonably suspected CSAM transit is reported to NCMEC's CyberTipline at *cybertipline.org* (US-based reporting) and to INHOPE's national hotline for non-US incidents.
- Audit Logs for any incident involving CSAM are retained indefinitely (not the 90-day default), pending law-enforcement disposition.
- Customers whose accounts initiate, even inadvertently, traffic to CSAM domains are permanently terminated and reported to the relevant law-enforcement authority in their jurisdiction.

### 2.2 Human trafficking and exploitation

Any use of iogrid to recruit, transport, transfer, harbor, or receive persons by means of force, threat, deception, or coercion for purposes of exploitation; or to advertise commercial sexual exploitation; or to facilitate forced labor, organ trafficking, or any analogous offense, is absolutely prohibited.

### 2.3 Critical-infrastructure attacks

iogrid may not be used in any way that targets the availability, integrity, or confidentiality of critical infrastructure, including:
- Electric power generation, transmission, or distribution systems;
- Water and wastewater treatment systems;
- Healthcare delivery systems and electronic health records (this includes scraping for non-public PHI, port-scanning hospital networks, etc.);
- Financial market infrastructure (exchanges, clearing houses, payment systems);
- Air, rail, maritime, and surface transportation control systems;
- Government emergency-services dispatch (911, 112, 999, etc.);
- Voting infrastructure (election systems, voter registration databases, ballot-counting systems).

### 2.4 Election interference

iogrid may not be used to:
- Target election infrastructure (Section 2.3);
- Disseminate forged government communications relating to elections;
- Coordinate inauthentic political activity on social platforms at scale;
- Disrupt voter-registration drives;
- Engage in voter suppression.

### 2.5 Weapons of mass destruction

iogrid may not be used to acquire, develop, manufacture, transfer, or deploy nuclear, chemical, biological, or radiological weapons, or precursor materials for the same.

### 2.6 Terrorist content

iogrid may not be used to disseminate, glorify, recruit for, or finance terrorism, as defined under the laws of the United States, the United Kingdom, the European Union, or any other jurisdiction whose laws apply to the conduct in question. We cooperate with the Global Internet Forum to Counter Terrorism (GIFCT) and equivalent national bodies for terrorist-content hash-list filtering. *[COUNSEL: confirm GIFCT membership eligibility and integration plan.]*

---

## 3. Hard-blocked (technical filters; cannot be overridden)

The following are blocked by the Coordinator's pre-flight filters. Attempts to bypass these filters (e.g., through obfuscation, encryption-of-destination-in-payload, or modification of the Daemon) are themselves a separate violation.

### 3.1 Government and military domains

`.gov`, `.mil`, and analogous national-government TLDs (`.gov.uk`, `.gov.au`, `.gc.ca`, `.gouv.fr`, `.bund.de`, `.gov.sg`, etc.) are unconditionally blocked for Bandwidth Workloads. We do not allow scraping or any other traffic to government-domain destinations.

*[COUNSEL: enumerate the complete list of government TLDs at finalization. There are >100 such TLDs; the daemon's blocklist must be kept current.]*

### 3.2 Outbound port restrictions

The following outbound ports are blocked from Provider IPs for Bandwidth Workloads:
- **TCP 25, 465, 587, 2525** — SMTP and submission (no email-spam relay);
- **TCP 6667, 6697** — IRC (no DDoS coordination, no spam-bot control);
- **TCP 9001, 9030** — Tor onion-routing protocol (we are not, and will not become, a Tor exit relay ourselves);
- **TCP 19, 53 (queries — outbound DNS), 123, 161, 389, 1900, 5353, 11211** — amplification-attack-prone protocols (DDoS-vector mitigation).

*[COUNSEL: confirm port list is appropriate and complete. The DNS amplification-vector port 53 outbound block may need exception handling for legitimate DNS-over-HTTPS / DoT, which should be carved out. Recommend defaulting outbound DNS to a sanctioned resolver.]*

### 3.3 Tor exit ports

iogrid Providers do not function as Tor exit relays. Attempting to chain a Tor exit through an iogrid Provider IP is a violation. (We have no objection to Tor itself — many of us use it personally — we just won't be the exit.)

### 3.4 SSH brute-force patterns

Rate-limited per destination per Provider. Sustained failed-authentication patterns are blocked.

### 3.5 CSAM and phishing hash matches

Per Section 2.1 and Section 4.2.

---

## 4. Soft-blocked (conditional, KYC or opt-in required)

The following are not unconditionally prohibited, but require additional verification.

### 4.1 Banking domains

Scraping or accessing banking, brokerage, or other financial-services domains (including but not limited to bankofamerica.com, chase.com, hsbc.com, mufg.jp, paypal.com, etc.) requires:
- Pro or Enterprise tier;
- Documented lawful purpose (e.g., regulatory compliance research, fraud detection, the Customer's own bank);
- KYC verification at the principal level (Section 4.5);
- Acknowledgment that even with this approval, the Customer remains solely responsible for compliance with the target site's terms of service and any financial-services regulation (Bank Secrecy Act, MiFID II, etc.).

### 4.2 Phishing and fraud-list domains

Destinations on PhishTank, OpenPhish, Google Safe Browsing, or other phishing-list services are blocked by default. A Customer with legitimate research purposes (e.g., a security-research firm, a phishing-defense vendor) may request the block be lifted for specific destinations, subject to manual review by *[COUNSEL: insert review contact, e.g., trust@iogrid.org]*.

### 4.3 Adult content

Adult-content domains may be accessed by Customers, but only via Providers who have explicitly opted in to receive adult-content Workloads. Default Provider posture is opt-out. Customers cannot direct adult-content Workloads to Providers who have not opted in; the Coordinator routes around them.

Adult content directed at minors, or any content involving non-consenting adults, falls under Sections 2.1 / 2.2 and is absolutely prohibited.

### 4.4 Stress-testing

Penetration testing, load testing, and other stress-testing is permitted **only against destinations the Customer owns or has documented authorization from the destination owner to test**. The Customer must retain a copy of the authorization and produce it on request (Customer ToS Section 9.1). Unauthorized stress-testing is a DDoS (Section 5.4) and prohibited.

### 4.5 KYC thresholds

Mirroring [`docs/LEGAL.md`](../docs/LEGAL.md):

| Customer monthly spend | KYC requirement |
|------------------------|-----------------|
| <$100 | Email verification only |
| $100–500 | Business email + LinkedIn / corporate confirmation |
| $500–5K | Manual review, government ID for principal |
| >$5K | Stripe Identity + business registration + AML check |

---

## 5. Conditionally prohibited (no KYC unlock available)

### 5.1 No mass-credential testing (credential stuffing)

Submitting credentials at high volume to a third party's authentication endpoint, where the credentials were obtained from a data breach or other unauthorized source, is prohibited. (This is distinct from a Customer testing their own users' credentials against their own service for breach-detection purposes, which is permitted with appropriate KYC and AUP attestation.)

### 5.2 No anti-spam bypass

iogrid may not be used to defeat spam filters, evade email blacklisting, or circumvent SMS-pumping defenses. Outbound SMTP is blocked at the port level (Section 3.2). SMS-pumping attacks (sending high-volume one-time-passcode requests to a target service to inflate the target's SMS bill) are prohibited regardless of the protocol used.

### 5.3 No fraud or financial-crime facilitation

iogrid may not be used for:
- Carding (testing stolen credit card numbers against merchant payment gateways);
- Money laundering, including via mixers, tumblers, or chain-hopping schemes;
- Sanctions evasion;
- Tax evasion;
- Securities fraud (pump-and-dump, manipulation, etc.);
- Identity theft;
- Romance scams, advance-fee scams, or other consumer fraud.

### 5.4 No DDoS

iogrid may not be used in or to coordinate denial-of-service attacks against any target the Customer does not own or have explicit authorization to test. This includes amplification attacks (Section 3.2 port list), application-layer floods, and slowloris-style exhaustion attacks.

### 5.5 No mass scraping of private content

Scraping content that requires authentication (login walls, paywalls) using credentials obtained through breach, social engineering, or other unauthorized means is prohibited. This is distinct from a Customer's own authenticated access to their own accounts, which is permitted.

### 5.6 No competitive intelligence in breach of contract

If the Customer's contract with a third party (e.g., a SaaS-platform ToS) prohibits scraping of that party's content, the Customer is responsible for that contractual violation. iogrid does not police every Customer-ToS pairing, but a known pattern of contract-breach scraping (e.g., a Customer scraping a platform after receiving a cease-and-desist) is grounds for termination.

*[COUNSEL: the "scraping of public data is legal but might breach the target's ToS" question is the central litigation theme of the Bright Data v. Meta / hiQ Labs case-law. Recommend clear separation between (a) what iogrid prohibits unconditionally (above), (b) what iogrid permits, and (c) what is between the Customer and the target site. The current draft leans on (c) by saying "responsible for that contractual violation" — counsel should confirm this is the right framing.]*

### 5.7 No bypass of geographic restrictions on regulated content

iogrid Bandwidth Workloads route through residential IPs, which by their nature alter the apparent geographic origin of a request. iogrid is not a VPN-for-end-users service (although individual Providers can opt in to free-VPN as their compensation). Customers may NOT use iogrid to:
- Circumvent geo-blocks imposed by regulators on regulated content (e.g., online-gambling licensing restrictions, regulated-securities offerings, prescription-drug import controls);
- Circumvent geo-blocks on copyright-licensed content (e.g., region-locked streaming) at industrial scale.

Incidental geographic-routing for legitimate purposes (e.g., ad-verification testing in a target market) is permitted.

### 5.8 No bypass of CAPTCHA / anti-bot at protected destinations

Using iogrid in coordinated CAPTCHA-solving or anti-bot-evasion workflows against destinations whose ToS prohibits automated access is prohibited.

*[COUNSEL: this is a difficult line. Industry practice (Bright Data, Apify, Octoparse, ScrapingBee) generally does NOT prohibit CAPTCHA-bypass. iogrid's stricter stance here is intentional but should be confirmed as the policy choice the founder wants. Alternative is to permit CAPTCHA-bypass for "publicly accessible content" only.]*

---

## 6. Permitted uses

The following are permitted and encouraged uses of iogrid:

### 6.1 E-commerce and pricing intelligence

- Price monitoring across competitor websites;
- Stock and availability tracking;
- Coupon and promotion discovery;
- Cross-marketplace catalog enrichment.

### 6.2 SEO and SERP analysis

- Rank-tracking from residential IPs in target geographies;
- Featured-snippet / knowledge-panel verification;
- SERP screenshot capture for client deliverables;
- Backlink discovery via public search engines.

### 6.3 Ad verification and brand safety

- Verifying that a paid ad displays in the targeted geography;
- Detecting unauthorized ad placements;
- Brand-safety auditing of programmatic-ad ecosystems.

### 6.4 Lead-generation scraping

- Scraping publicly-listed business directories, professional profiles, and public-record databases, subject to the destination site's ToS (Section 5.6) and applicable privacy laws (Section 7.2).
- LinkedIn, Twitter / X, and other social-media public profile data may be scraped where the destination platform's ToS does not prohibit and where applicable privacy law permits. **As of the effective date, these target platforms' ToS positions are evolving and subject to active litigation; Customers are advised to seek their own legal counsel.** *[COUNSEL: review carefully — the hiQ Labs v. LinkedIn line of cases and Meta v. Bright Data line of cases together establish that PUBLIC data scraping is generally lawful in the US, but ToS-breach and CFAA exposure remains. EU position is different (GDPR Art. 6 lawful-basis analysis required). Recommend a separate "scraping legal context" page in the docs rather than relying on this AUP.]*

### 6.5 Social-media intelligence

- Aggregating public posts for trend analysis, sentiment analysis, public-relations monitoring;
- Identifying public influencers and engagement patterns;
- Subject to the target platform's ToS and applicable privacy law.

### 6.6 AI / ML training data collection

- Web crawling for AI / ML training data sets, **respecting `robots.txt` and any explicit "no AI training" signals** at the destination (e.g., `User-agent: GPTBot Disallow: /`, the IETF-AI Preferences draft, or `ai.txt` proposals once standardized);
- Per-domain rate limits;
- Attribution of training data sources where required by applicable copyright law and any model-providence regulation (e.g., EU AI Act Art. 53).

*[COUNSEL: as of finalization in 2026, the AI-scraping legal landscape is in motion. The EU AI Act's data-governance requirements (Art. 10), copyright considerations (Directive 2019/790 Art. 4 TDM exception with opt-out), and pending US fair-use litigation (NYT v. OpenAI, Authors Guild v. OpenAI, Andersen v. Stability AI, etc.) all bear on this section. Recommend version-controlled annual review.]*

### 6.7 Distributed compute, GPU inference, iOS builds

- Use of Docker / GPU / iOS-Build Workloads for any lawful business purpose, subject to the workload-specific filters in [Provider ToS Section 7](./provider-tos.md).
- AI training and inference is permitted, subject to AUP compliance for any data being processed.
- iOS builds for the Customer's own applications or for applications the Customer has documented authority to build (e.g., a CI service building on behalf of paying users who own the application).

---

## 7. Provider-specific obligations

In addition to the general prohibitions above, Providers specifically agree:

7.1 **Do not tamper with the Daemon.** Do not modify, repackage, or run alternative versions of the Daemon. Do not disable filters, telemetry, or audit-log forwarding.

7.2 **Do not falsify geography.** Do not run the Daemon through a VPN, Tor exit, proxy, or other middlebox that obscures the Device's true geographic location. (You may run the Daemon and also use a VPN for your personal traffic on the same Device — the issue is whether the Daemon's outbound IP reflects the Device's true ISP-assigned address.)

7.3 **Do not co-host with other proxy networks.** Do not simultaneously enrol the same Device IP in another proxy / residential-IP service (e.g., Bright Data SDK, Honeygain, IPRoyal Pawns, etc.). Concurrent enrolment creates audit-log ambiguity and is prohibited.

7.4 **Do not use the Daemon on networks you do not have authority over.** See Provider ToS Section 2(4).

---

## 8. Customer-specific obligations

8.1 **Lawful basis.** You represent and warrant that you have a lawful basis under applicable law for every Workload you initiate, including a lawful basis for any personal-data processing of data subjects whose data you process through iogrid (GDPR Art. 6, LGPD Art. 7, CCPA notice at collection).

8.2 **Respect destination ToS.** While iogrid does not enforce every destination's ToS, you are responsible for your own ToS compliance with the destinations you target. iogrid's permission to use the Service is not a substitute for the destination's permission.

8.3 **Maintain authorization records.** For any restricted activity (Section 4: banking, stress-testing, etc.), retain documented authorization for at least 7 years and produce on iogrid request.

8.4 **No sub-licensing or resale.** You may not resell, sub-license, or otherwise redistribute access to iogrid to third parties without iogrid's prior written consent. (You may build Customer-facing applications that use iogrid as a backend, subject to your continued responsibility for any AUP violations originating from your application.)

---

## 9. Reporting violations

If you become aware of any AUP violation, please report it to *[COUNSEL: insert abuse contact, e.g., abuse@iogrid.org]*. Include as much detail as possible: timestamps, destination URLs, suspected mechanism. We do not require you to identify yourself.

CSAM-specific reports may also be made directly to NCMEC at *cybertipline.org* (US) or to your national INHOPE hotline.

---

## 10. Enforcement and remedies

10.1 **Investigation.** On receipt of a credible AUP-violation report or on our own detection of suspicious patterns, we may:
- Suspend the affected account pending investigation;
- Inspect Audit Logs;
- Engage outside counsel or forensic investigators;
- Notify the affected Provider or Customer of the investigation, except where doing so would compromise the investigation or violate a non-disclosure order.

10.2 **Termination.** Confirmed violations result in suspension or permanent termination per the applicable ToS.

10.3 **Notification to authorities.** For Section 2 violations (CSAM, trafficking, critical-infrastructure attack, election interference, WMD, terrorism), iogrid will report to the relevant national or international authority. For other criminal violations, iogrid retains discretion to report.

10.4 **Civil action.** iogrid reserves the right to pursue civil remedies against violators, including injunctive relief and damages, and to recover legal fees where the applicable jurisdiction permits.

*[COUNSEL: confirm mandatory-reporting framing for Section 2 violations matches iogrid's actual legal obligations under §2258A (US), NetzDG / Digital Services Act (EU), Online Safety Act (UK), and analogous regimes.]*

---

## 11. Updates

We may update this AUP from time to time. Updates take effect on posting and are versioned in the [`legal/README.md`](./README.md) revision history. Material updates are also announced with 30 days' notice per the applicable ToS Section 20.

---

## 12. Contact

- **Report abuse:** *[COUNSEL: insert, e.g., abuse@iogrid.org]*
- **CSAM-specific (US):** *cybertipline.org*
- **Legal / law-enforcement liaison:** *[COUNSEL: insert, e.g., legal@iogrid.org]*

---

*End of Acceptable Use Policy v0.1-draft.*

*[COUNSEL: full-document review required. Key open items: NCMEC partner-status confirmation (Section 2.1), GIFCT membership (Section 2.6), port-block list completeness (Section 3.2), CAPTCHA-bypass policy choice (Section 5.8), AI-training opt-out framing (Section 6.6), destination-ToS responsibility allocation (Section 5.6 / 8.2), mandatory-reporting obligations per jurisdiction (Section 10.3). Total `[COUNSEL]` markers: ~15.]*
