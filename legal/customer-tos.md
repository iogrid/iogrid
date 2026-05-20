# Customer Terms of Service

**Status:** Draft v0.1 — pre-counsel-review. Not legal advice. See [`legal/README.md`](./README.md).
**Effective date:** *[COUNSEL: insert effective date upon finalization]*
**Governing entity:** *[COUNSEL: confirm operating entity]*

> **Plain-language summary** *(non-binding).*
>
> By using iogrid as a customer, you get on-demand access to residential-IP proxy bandwidth, Docker / GPU compute, and macOS-native iOS builds, billed per use. You agree to use it lawfully — no scraping anything illegal, no DDoS, no carding, no spam, no anything-on-the-AUP. We can refuse to serve you. You promise to defend us if a third party sues us because of something you asked us to do. Our liability is capped. You can cancel any time.

---

## 1. Definitions

- **"iogrid"**, **"we"**, **"us"**, **"our"** — the operating entity. *[COUNSEL: confirm entity name once finalized.]*
- **"Customer"**, **"you"**, **"your"** — the individual or legal entity that creates an iogrid customer account and accepts these terms. For corporate Customers, the individual accepting on behalf of the entity represents that they have authority to bind it.
- **"Service"** — collectively, the iogrid platform (Coordinator, API, web management plane, SDKs) and the four Workload types (Bandwidth, Docker, GPU, iOS-Build).
- **"Workload"** — a unit of work the Customer dispatches via the Service.
- **"Provider"** — a third party that has installed the iogrid Daemon and shares bandwidth / compute through the network.
- **"AUP"** — the [Acceptable Use Policy](./aup.md).
- **"DPA"** — the [Data Processing Agreement](./dpa.md) (applies where the Customer's use involves processing EU / UK / Swiss data subjects' personal data).
- **"Privacy Policy"** — at [`legal/privacy-policy.md`](./privacy-policy.md).
- **"Documentation"** — the developer documentation at *[COUNSEL: insert URL, e.g., https://docs.iogrid.org]* and any other reference materials we publish about the Service.
- **"Order"** — the Customer's selection of a service tier, API plan, or purchase via the management plane.
- **"Credentials"** — API keys, OAuth tokens, signed-message wallets, and other secrets we issue to the Customer to authenticate Service usage.
- **"Audit Log"** — defined in the Provider Terms of Service Section 1.

*[COUNSEL: add or refine defined terms per finalized commercial model.]*

---

## 2. Eligibility and account

2.1 **Age and capacity.** You must be at least 18 years old (or the age of legal majority in your jurisdiction) and have the legal capacity to contract.

2.2 **Sanctioned countries and persons.** The eligibility constraints in Provider ToS Section 2(5) and 2(6) apply to Customers, mutatis mutandis. We screen Customer signups against sanctions lists.

2.3 **Account information.** You must provide accurate, current, and complete information at signup and keep it up to date. False, misleading, or incomplete information is grounds for immediate termination (Section 13).

2.4 **Account security.** You are responsible for safeguarding your Credentials. You must notify us promptly at *[COUNSEL: insert security contact, e.g., security@iogrid.org]* if you suspect a Credential has been compromised. You are responsible for all activity under your Credentials prior to the time we receive your notice and have a reasonable opportunity to act.

2.5 **Corporate accounts.** If you create an account on behalf of a legal entity, you represent that you are authorized to bind that entity. "You" in these terms means both the entity and the individual accepting on its behalf, jointly and severally to the extent permitted by law.

---

## 3. The Service

3.1 **What the Service does.** iogrid provides on-demand access to a network of distributed Providers for four Workload types. See Documentation for full technical details. Service capabilities, endpoints, SDKs, pricing, and Provider availability are evolving and may change with reasonable notice.

3.2 **What the Service is NOT.** The Service is not:
- A common-carrier telecommunications service;
- An ISP or hosting service;
- A managed-security or threat-intelligence service;
- A regulated financial-services product (notwithstanding any $GRID-payment options, which are governed by [`legal/token-disclaimer.md`](./token-disclaimer.md));
- An anonymity / privacy-preserving service for end-users (we retain Audit Logs identifying Customers behind every Workload).

*[COUNSEL: confirm framing — particularly that the Service is not subject to telecommunications or financial-services regulation in each launch jurisdiction. The EU Electronic Communications Code, certain US state utility commissions, and the UK Ofcom regime have nuanced definitions.]*

3.3 **Beta features.** We may make pre-release features available marked as "beta," "preview," "experimental," or similar. Beta features are provided as-is, may change without notice, may be discontinued at any time, and have no SLA, no commitment of uptime, and no commitment to general availability.

---

## 4. Service tiers and pricing

4.1 **Tiers.** *[COUNSEL: confirm tier nomenclature and pricing as part of commercial review. Below reflects the iogrid product plan as of 2026-05-19; pricing is subject to change before finalization.]*

| Tier | Monthly cost (USD) | Workload included | Rate limit | KYC requirement |
|------|--------------------|-------------------|------------|-----------------|
| **Free** | $0 | 100 requests / day (bandwidth only) | 1 RPS | Email verification only |
| **Plus** | $49 / month | Bandwidth + Docker, 1M req or 100 GB whichever first | 100 RPS aggregate | Business email + LinkedIn / corporate confirmation |
| **Pro** | $499 / month | All Workload types, 10M req or 1 TB whichever first | 1,000 RPS aggregate | Manual review + government ID for principal |
| **Enterprise** | Custom (typ. $5K+) | Custom volume, dedicated routing, SLA available | Negotiated | Stripe Identity + business registration + AML check |

4.2 **Overage.** Usage above tier limits is billed at the published per-unit rate. See Documentation for current per-unit rates. Customers may set a hard cap to prevent overage; if no cap is set, you authorize us to bill overage at the published rate.

4.3 **Payment.** Customers pay by credit / debit card (via Stripe), bank transfer (Enterprise tier only), USDC on Solana, or $GRID. The discount and KYC framework for each payment method is in [`docs/BUSINESS-STRATEGY.md` §4 Currency model](../docs/BUSINESS-STRATEGY.md#4-currency-model--grid--fiat-hybrid) §"Customer payment options."

4.4 **Taxes.** Pricing is exclusive of VAT, GST, and other applicable transaction taxes. We collect and remit such taxes as required by law. You are responsible for any other taxes applicable to your use of the Service.

4.5 **Price changes.** We may change Service pricing with **30 days' prior notice.** Price changes apply to renewals after the notice period; prepaid Customer commitments are honored at the previously-quoted price for the prepaid term.

---

## 5. API key management and rate limits

5.1 **API key issuance.** We issue you one or more API keys upon Service onboarding. You may rotate keys at any time via the management plane.

5.2 **Rate limits.** Each tier has aggregate and per-endpoint rate limits as published in the Documentation. Rate-limit violations result in HTTP 429 responses, not termination — unless the violation is sustained, intentional, or causes Service disruption (Section 13.2).

5.3 **Provider rate limits.** Independently of your customer-side rate limit, each Provider is rate-limited per destination as specified in [`docs/BUSINESS-STRATEGY.md` §6 Legal risk landscape](../docs/BUSINESS-STRATEGY.md#6-legal-risk-landscape--mitigation). You acknowledge that some destinations (e.g., LinkedIn, Facebook, Twitter / X, Google) are configured with low per-Provider rate limits (default 10 RPS / Provider / destination) to protect Provider IP reputation. This may reduce achievable throughput for those destinations.

5.4 **Service-side throttling.** We may temporarily throttle or pause your traffic if we detect patterns indicative of AUP violations or abuse, pending investigation (Section 13).

---

## 6. Acceptable use

You agree to comply with the [Acceptable Use Policy](./aup.md) at all times. The AUP is incorporated by reference. Without limitation, you agree NOT to use the Service:
- For CSAM, human trafficking, exploitation, harassment, election interference, or critical-infrastructure attacks;
- For DDoS, stress-testing without owner consent, mass-credential testing, or carding;
- To bypass anti-spam (no SMTP relay, no SMS-pumping);
- To scrape .gov or .mil domains;
- For banking-domain scraping unless explicitly approved on your account;
- For adult-content scraping unless the Provider receiving the workload has explicitly opted in;
- In any other manner prohibited by the AUP or applicable law.

Violations may result in immediate suspension or termination (Section 13.2) and forfeit your right to refund any prepaid amounts.

---

## 7. Customer indemnification

7.1 **You defend us.** You will defend, indemnify, and hold harmless iogrid, its affiliates, and their respective directors, officers, employees, contractors, and agents from and against any third-party claim, lawsuit, demand, regulatory inquiry, subpoena, or proceeding, and any associated damages, settlements, costs, and reasonable attorneys' fees, that arises out of or relates to:
- Your use or misuse of the Service;
- Your breach of this Agreement (including the AUP);
- Your violation of any law, regulation, or third-party right (including intellectual property, privacy, and publicity rights);
- The content, application, or system you transmit, process, store, or operate through the Service;
- Any claim by a Provider arising from Workloads you initiated.

7.2 **Conditions.** Our right to your indemnification is conditional on (a) prompt notice of the claim (we will notify you within 10 business days of receipt, unless prohibited by law), (b) your sole control of the defense and settlement (with our consent for any settlement that imposes obligations on us beyond payment of money damages, which we will not unreasonably withhold), and (c) our reasonable cooperation at your expense.

7.3 **No double recovery.** Where we are entitled to indemnification from you under Section 7.1 and also have independent insurance recovery, the indemnity reduces by the net insurance proceeds we receive.

---

## 8. Right of refusal

8.1 **We may refuse to serve any Customer.** Without limiting any other right we have under this Agreement, we may decline to onboard a prospective Customer, suspend an existing Customer, or terminate an existing Customer **at any time, for any reason or no reason, with or without prior notice**, including (without limitation) for the following reasons:
- The Customer's intended use does not fit our product;
- The Customer's industry presents risk we are unwilling to accept (e.g., adult-content publishers, online-gambling operators, certain political campaigns, weapons sellers — none of which are absolutely prohibited unless covered by the AUP, but each of which may trigger discretionary refusal);
- The Customer's pattern of usage strains the network or risks Provider IP-reputation harm;
- The Customer has been the subject of negative law-enforcement, regulatory, or media attention that we believe creates reputational risk;
- The Customer is a competitor of iogrid;
- Any other reason within our reasonable business judgment.

8.2 **No discrimination on protected characteristics.** Nothing in Section 8.1 authorizes refusal on the basis of any characteristic protected by applicable anti-discrimination law (race, religion, national origin, sex, gender identity, sexual orientation, disability, age, etc., as defined by the law of the Customer's jurisdiction).

8.3 **Refund on refusal.** If we terminate a Customer under Section 8.1 in the absence of an AUP violation, we will refund any prepaid, unused amounts on a pro-rata basis.

*[COUNSEL: confirm Section 8 right-of-refusal language is consistent with anti-discrimination law in each launch jurisdiction. Some jurisdictions (e.g., certain US states under public-accommodation laws) constrain refusal-of-service language for SaaS products; the analysis turns on whether iogrid is a "place of public accommodation." Generally B2B SaaS is not, but the analysis is jurisdiction-specific.]*

---

## 9. Audit rights

9.1 **Audit of Customer usage.** If we have a reasonable, good-faith belief that you may have violated the AUP, we may:
- Inspect Audit Logs for Workloads attributed to your account;
- Request from you (and you will provide within 10 business days) reasonable evidence of the lawful basis for the Workloads in question (e.g., evidence of consent for stress-testing, regulatory approval for KYC-required activity);
- Temporarily suspend the Service pending the audit, with notice;
- Engage outside counsel or auditors at our expense to assist with the audit (their work product is confidential and they are bound by reasonable confidentiality terms toward you).

9.2 **No general audit.** Outside the trigger described in Section 9.1, we do not audit Customer Workloads on a routine basis. We do not review Workload content (Section 11.2 of the Provider ToS — we do not decrypt traffic).

9.3 **Audit-cooperation by you.** You agree to cooperate reasonably with any audit conducted under Section 9.1. Failure to cooperate may, by itself, constitute grounds for suspension or termination.

---

## 10. Data privacy and DPA

10.1 **Privacy Policy.** Your use of the Service is subject to the [Privacy Policy](./privacy-policy.md), which describes how we handle your personal data.

10.2 **DPA.** Where your use of the Service involves processing of EU / UK / Swiss data subjects' personal data, you must execute the [DPA](./dpa.md), incorporated by reference. The DPA describes iogrid's role as data processor and the parties' respective obligations.

10.3 **Your data subjects.** You are responsible for ensuring that you have a lawful basis for any data subjects whose personal data you process through the Service (GDPR Art. 6, CCPA notice at collection, LGPD Art. 7, etc.). We do not provide a lawful basis on your behalf.

---

## 11. Refund policy

11.1 **Refunds — generally not provided.** Subscription tier fees are non-refundable except as expressly provided in this Section 11 or required by applicable law.

11.2 **14-day satisfaction window (consumer Customers, EU/UK).** If you are an individual consumer ordinarily resident in the EU, UK, or Switzerland, you may have a statutory 14-day cooling-off period (e.g., EU Directive 2011/83/EU Art. 9). To exercise this right, contact *[COUNSEL: insert refund contact, e.g., billing@iogrid.org]* within 14 days of your first paid subscription. **Note: by using the Service during the cooling-off period, you may forfeit some or all of this right under Art. 16(m) "fully performed digital content" carve-out.** *[COUNSEL: confirm carve-out language and add jurisdiction-specific consumer rights as required.]*

11.3 **Service-credit refunds for unavailability.** Enterprise Customers with negotiated SLAs may earn service credits for unavailability per the Enterprise order form. Free / Plus / Pro tiers carry no SLA (Section 14).

11.4 **Termination refunds.** Per Section 8.3 (we terminate you without cause / without AUP violation) and Section 13.4 (you terminate during a prepaid term).

---

## 12. Intellectual property

12.1 **iogrid IP.** The Service, our software (Coordinator, API, SDKs, Daemon, web management plane), our trademarks, our Documentation, and any improvements, modifications, or derivative works are and remain our sole property. We grant you a non-exclusive, non-transferable, revocable license to use the Service in accordance with this Agreement.

12.2 **Customer IP.** You retain all rights in your Workload inputs, source code, container images, models, and outputs. We claim no rights in your content, except that you grant us a limited, non-exclusive, royalty-free license to process your content solely to provide the Service to you.

12.3 **Aggregated statistics.** We may aggregate anonymized usage data across Customers for analytics, research, capacity planning, and Service improvement. Aggregated statistics never identify individual Customers and are not subject to Customer-confidentiality obligations.

12.4 **Feedback.** Any feedback you provide may be used by us without restriction or compensation to you.

---

## 13. Suspension and termination

13.1 **Termination by you.** You may terminate at any time by closing your account via the management plane. Your subscription continues until the end of the then-current billing period, and prepaid amounts are not refunded except as expressly provided.

13.2 **Termination by us — for cause.** We may suspend or terminate immediately, without prior notice, if:
- You violate this Agreement or the AUP;
- You fail to pay amounts due after written notice and a 10-day cure period;
- We have a good-faith belief your account has been compromised;
- We are required by law or court order;
- You become insolvent, file for bankruptcy, or undergo similar proceedings.

13.3 **Termination by us — without cause.** We may terminate with **30 days' prior notice** to the email address on your account.

13.4 **Effect of termination.**
- Access to the Service ceases at the effective date of termination;
- Prepaid amounts: refunded pro-rata if termination is without cause (Section 8.3 / 13.3); not refunded if termination is for cause (Section 13.2);
- Audit Logs covering past Workloads are retained for the 90-day window;
- You remain liable for all amounts due, indemnification claims arising from pre-termination conduct, and any other surviving obligations (Section 21).

13.5 **Data return / deletion.** Upon termination, you may request export of your Workload outputs and configuration data within 30 days. After 30 days, we delete Customer-specific data except for the 90-day Audit Log retention and any data we are legally required to retain.

---

## 14. Service availability

14.1 **No SLA on Free / Plus / Pro tiers.** Best-effort service. Reasonable maintenance windows announced in advance via *[COUNSEL: insert status page]*.

14.2 **Enterprise SLA.** Enterprise Customers may negotiate SLA terms in their order form. SLAs typically include uptime percentage, support response time, and service-credit remedies.

14.3 **Emergency takedown.** We may take the Service or any specific Customer offline immediately to comply with law, respond to abuse, or protect the network (mirrors Provider ToS Section 10.3).

---

## 15. Disclaimer of warranties

The Service is provided **"AS IS" and "AS AVAILABLE,"** without warranties of any kind. We disclaim all warranties to the maximum extent permitted by law (mirrors Provider ToS Section 15). Carve-outs preserve non-excludable statutory rights where required by local law. *[COUNSEL: confirm jurisdiction-specific carve-outs.]*

---

## 16. Limitation of liability

16.1 **No indirect damages.** Mirrors Provider ToS Section 16.1.

16.2 **Direct damages cap.** Our aggregate liability to you for all direct damages arising out of or relating to this Agreement is limited to the greater of:
- **(a) The total amount you paid to us during the twelve (12) months immediately preceding the event giving rise to the claim;** or
- **(b) One hundred US dollars ($100 USD).**

This formula matches the spec in `docs/LEGAL.md` ("customer's most-recent monthly spend × 12") with the understanding that "monthly spend × 12" effectively equals "annual spend" — phrased as the 12-month-trailing total for clarity. *[COUNSEL: confirm whether spec intent is 12-month trailing or strict "most-recent-month × 12" (which produces a different cap for non-stable spend patterns).]*

16.3 **Indemnity carve-out.** Section 16.2's cap does not apply to your indemnification obligations under Section 7.

16.4 **Non-excludable carve-outs.** Mirrors Provider ToS Section 16.4.

---

## 17. Governing law; dispute resolution

Mirrors Provider ToS Section 17, except that:
- Customer disputes are by default subject to the same arbitration regime;
- Enterprise Customers with negotiated agreements may include alternative dispute-resolution clauses in their order forms;
- The class-action waiver applies more straightforwardly to corporate Customers but should be reviewed by counsel for consumer Customers in jurisdictions where class-action waivers are constrained.

*[COUNSEL: confirm consistency with Provider ToS Section 17. Recommend identical clause language to avoid parsing ambiguity.]*

---

## 18. Confidentiality

18.1 **Confidential information.** Each party may share information marked or reasonably understood to be confidential (including your Workload data, our pricing for Enterprise tier, our non-public roadmap).

18.2 **Confidentiality obligations.** Each party will (a) use the other's confidential information only to perform under this Agreement, (b) protect it with at least the same care it uses for its own confidential information of similar type (and no less than reasonable care), and (c) not disclose it to third parties except to its personnel and contractors with a need to know and bound by confidentiality terms substantially similar to these.

18.3 **Exceptions.** Confidentiality obligations do not apply to information that (a) is or becomes public without breach, (b) was lawfully in the receiving party's possession before disclosure, (c) is lawfully obtained from a third party not bound by confidentiality, or (d) is independently developed.

18.4 **Compelled disclosure.** A party compelled by legal process to disclose the other's confidential information may do so, but will (where lawful) notify the other party promptly and cooperate in any reasonable effort to limit the disclosure.

18.5 **Survival.** Confidentiality obligations survive termination for **three (3) years** after termination, except for trade secrets, which are protected for as long as they remain trade secrets.

---

## 19. Token-payment terms (where applicable)

If you elect to pay in $GRID, you additionally agree to the [token disclaimer](./token-disclaimer.md), incorporated by reference. Discounts for $GRID-paying Customers are described in [`docs/BUSINESS-STRATEGY.md` §4 Currency model](../docs/BUSINESS-STRATEGY.md#4-currency-model--grid--fiat-hybrid). The 20%-discount for $GRID payment is provided for the volume-discount alignment described in the tokenomics doc and is not a discount on the regulated price of any financial product. *[COUNSEL: confirm phrasing doesn't inadvertently treat $GRID as a financial instrument or create a securities-offering nexus.]*

---

## 20. General provisions

Mirrors Provider ToS Section 18 (Entire Agreement, Severability, No Waiver, Assignment, No Third-Party Beneficiaries, Force Majeure, Notices, Language).

Additional Customer-specific provisions:
- **Independent contractor; no partnership.** Nothing in this Agreement creates a partnership, joint venture, employer-employee relationship, agency, or franchise between us.
- **Compliance by Customer.** You are responsible for compliance with all laws applicable to your use of the Service, including any data-protection, consumer-protection, competition / antitrust, export-control, and industry-specific regulation (e.g., HIPAA for US healthcare, PCI-DSS for payment card data, FCRA for consumer reports).
- **No exclusive dealings.** Nothing in this Agreement obligates either party to deal exclusively with the other.

---

## 21. Survival

The following Sections survive termination: 1 (Definitions), 7 (Customer Indemnification), 10 (Data Privacy), 12 (Intellectual Property), 13.4 / 13.5 (Effect of Termination, Data Return), 15 (Disclaimer), 16 (Limitation of Liability), 17 (Governing Law / Dispute Resolution), 18 (Confidentiality), 20 (General Provisions), and this Section 21.

---

## 22. Updates to these terms

We may update this Agreement with **30 days' prior notice** (mirrors Provider ToS Section 20). Continued use after the effective date constitutes acceptance.

---

## 23. Contact

- **Customer support:** *[COUNSEL: insert, e.g., support@iogrid.org]*
- **Billing:** *[COUNSEL: insert, e.g., billing@iogrid.org]*
- **Legal / subpoena / law-enforcement:** *[COUNSEL: insert, e.g., legal@iogrid.org]*
- **Privacy / data subject rights:** *[COUNSEL: insert, e.g., privacy@iogrid.org]*
- **Postal address:** *[COUNSEL: insert finalized registered address]*

---

*End of Customer Terms of Service v0.1-draft.*

*[COUNSEL: full-document review required. Key open items: tier-pricing finalization (Section 4), liability-cap formula clarification "monthly × 12" vs. "12-month-trailing" (Section 16.2), Customer-indemnification scope (Section 7), refund-policy interplay with EU 14-day right (Section 11.2), discrimination-law analysis for refusal-of-service (Section 8.2), DPA module-selection (Section 10.2), confidentiality survival period (Section 18.5), token-payment characterization (Section 19). Total `[COUNSEL]` markers: ~25.]*
