# Data Processing Agreement (DPA)

**Status:** Draft v0.1 — pre-counsel-review. Not legal advice. See [`legal/README.md`](./README.md).
**Effective date:** *[COUNSEL: insert effective date upon finalization]*
**Form:** This DPA satisfies the requirements of Article 28 of Regulation (EU) 2016/679 (the "GDPR"), Article 28 of the UK GDPR, the Swiss Federal Act on Data Protection (FADP), and other equivalent regimes, **subject to counsel review and finalization.**

> **Plain-language summary** *(non-binding).*
>
> When you (the controller — a customer or, in some cases, a provider) process personal data of European, UK, or Swiss data subjects through iogrid, iogrid acts as your data processor. This DPA spells out iogrid's processor obligations: what data we process, why, how we secure it, who we share it with (the sub-processor list), what happens when there's a breach, what your audit rights are, and how data moves internationally (Standard Contractual Clauses). This is the EU-required addendum to the main ToS.

---

## 1. Scope and applicability

1.1 **Parties.** This DPA is entered into between:
- **"iogrid"** — the iogrid operating entity, acting as **Processor** under the Data Protection Laws;
- **"Controller"** — the Customer or Provider as identified in the main agreement (Customer Terms of Service or Provider Terms of Service), acting as **Controller** under the Data Protection Laws.

For the avoidance of doubt:
- For Customers using iogrid to process personal data of their own data subjects (e.g., a Customer scraping public profiles, or a Customer running a Docker workload that touches their users' data), the Customer is the Controller and iogrid is the Processor.
- For Providers, in respect of telemetry data we collect about them from their Device, iogrid is the Controller (governed by the Privacy Policy, not this DPA). In respect of any personal data that may transit a Provider's Device as part of a Workload, the underlying Customer is the Controller and iogrid is the Processor — the Provider has no Controller role.

1.2 **Trigger.** This DPA applies whenever and to the extent that processing of personal data of data subjects in the European Economic Area, the United Kingdom, or Switzerland occurs through the iogrid Service.

1.3 **Conflict.** If there is any conflict between this DPA and the main agreement, this DPA controls **only as to data-protection matters**; the main agreement controls otherwise.

1.4 **Definitions.** Capitalized terms not defined here have the meanings given in the GDPR, UK GDPR, FADP, or the main agreement, as the case may be.

*[COUNSEL: confirm DPA applies bilaterally (Provider Controller / iogrid Processor for Provider-side personal data) vs. unilaterally (Customer Controller / iogrid Processor for Customer-side personal data) — currently drafted as unilateral with Provider relationships governed separately by Privacy Policy.]*

---

## 2. Subject matter, duration, nature and purpose of processing

2.1 **Subject matter.** The processing of personal data by iogrid as Processor on behalf of the Controller is solely for the purpose of providing the iogrid Service to the Controller in accordance with the main agreement.

2.2 **Duration.** Processing continues for the term of the main agreement and the 90-day Audit Log retention period thereafter, except where specific data must be retained for a longer period to satisfy a legal obligation, in which case the longer period applies.

2.3 **Nature of processing.** Routing, scheduling, executing, logging, and securing Workloads. iogrid does not decrypt or read the substantive content of any traffic; processing is limited to metadata and routing information, plus any data the Controller explicitly directs iogrid to process (e.g., container inputs and outputs for Docker / GPU Workloads, source code inputs for iOS-Build Workloads).

2.4 **Purpose of processing.** To provide the Service, to bill the Controller, to respond to legal process, and to perform our security and audit obligations.

2.5 **Type of personal data processed.** See Annex 1.

2.6 **Categories of data subjects.** See Annex 1.

---

## 3. Obligations of the Processor (iogrid)

iogrid as Processor agrees to:

3.1 **Process only on documented instructions.** Process personal data only on the Controller's documented instructions, including with regard to transfers to third countries, unless required by EU, EEA member state, UK, Swiss, or other applicable law. In the latter case, iogrid will inform the Controller of the legal requirement before processing, unless the law prohibits such notification on important grounds of public interest.

3.2 **Confidentiality of personnel.** Ensure that persons authorized to process personal data have committed themselves to confidentiality or are under appropriate statutory obligation of confidentiality.

3.3 **Security.** Implement appropriate technical and organizational measures (Article 32 GDPR) to ensure a level of security appropriate to the risk. See Annex 2 (security measures).

3.4 **Sub-processors.**
- iogrid uses sub-processors as listed in Annex 3.
- Controller hereby grants general written authorization for engagement of the sub-processors listed in Annex 3 as of the effective date.
- iogrid will inform the Controller of any intended addition or replacement of sub-processors with at least **30 days' prior notice**, giving the Controller the opportunity to object on reasonable grounds.
- iogrid imposes on each sub-processor data-protection obligations substantially equivalent to those set out in this DPA, and remains responsible for sub-processor performance.

3.5 **Data subject rights assistance.** Taking into account the nature of the processing, iogrid assists the Controller, by appropriate technical and organizational measures, insofar as possible, in fulfilling the Controller's obligation to respond to data-subject rights requests (access, rectification, erasure, restriction, portability, objection, and rights related to automated decision-making and profiling) under Articles 12–22 GDPR. iogrid will not respond to data-subject requests directly (it will refer the data subject to the Controller) unless the Controller has explicitly authorized direct response.

3.6 **Article 32, 33, 34, 35, 36 assistance.** iogrid assists the Controller in ensuring compliance with the obligations under Articles 32 to 36 GDPR (security, breach notification, data-protection impact assessment, prior consultation), taking into account the nature of processing and the information available to iogrid.

3.7 **Breach notification.** iogrid notifies the Controller without undue delay, and in any case within **72 hours**, of becoming aware of a personal data breach affecting Controller's data. The notification includes (a) nature of the breach and approximate number of data subjects and records affected, (b) likely consequences, (c) measures taken or proposed, and (d) iogrid's data-protection contact. The Controller is responsible for notifying its supervisory authority and affected data subjects as required under Articles 33 and 34 GDPR.

3.8 **Return or deletion.** At the choice of the Controller, iogrid deletes or returns all personal data to the Controller after the end of the provision of services relating to processing, and deletes existing copies unless EU or member-state law (or analogous law in another applicable jurisdiction) requires storage. Specifically:
- Customer-specific data is deleted within 30 days of termination per Customer ToS Section 13.5;
- Audit Log entries are deleted on the 90-day rolling schedule per Privacy Policy and Provider ToS Section 6.1;
- Where law requires longer retention, the retained data is isolated, access-controlled, and not used for any purpose other than satisfying the legal obligation.

3.9 **Availability for audits.** iogrid makes available to the Controller all information necessary to demonstrate compliance with the obligations in Article 28 GDPR and allows for and contributes to audits, including inspections, conducted by the Controller or another auditor mandated by the Controller. See Section 6 (audit rights).

3.10 **Notify the Controller of unlawful instructions.** iogrid will inform the Controller immediately if, in its opinion, an instruction infringes the GDPR, UK GDPR, FADP, or other applicable data-protection law.

---

## 4. Obligations of the Controller

4.1 The Controller represents and warrants that:
- It has all necessary lawful bases under Article 6 GDPR (and Article 9 where special-category data is involved) for any personal data it processes through the iogrid Service;
- It has provided all required notices to data subjects under Articles 13 and 14 GDPR;
- It has obtained all required consents under applicable law;
- Its processing instructions to iogrid are lawful;
- It has performed any required Data Protection Impact Assessment (DPIA) under Article 35 where the processing meets the trigger criteria.

4.2 The Controller is solely responsible for any consequences of the Controller's failure to satisfy Section 4.1.

---

## 5. International transfers

5.1 **Transfer mechanism.** Where iogrid's provision of services involves transfer of personal data from the EEA, UK, or Switzerland to a country not benefiting from an adequacy decision by the European Commission (or the UK Secretary of State, or the Swiss FDPIC, as applicable), the parties agree the following transfer mechanisms apply:

5.1.1 **EEA transfers to a country without adequacy.** The European Commission's Standard Contractual Clauses set out in Implementing Decision (EU) 2021/914 of 4 June 2021 ("**EU SCCs**") are hereby incorporated by reference, with:
- **Module Two** (Controller-to-Processor) applying where the Controller is in the EEA and iogrid (the Processor) is in a third country;
- **Module Three** (Processor-to-Processor) applying where iogrid (the Processor) transfers data to a sub-processor in a third country.
- *[COUNSEL: select Module(s) at finalization. The choice depends on iogrid's primary operating entity location. If iogrid Inc. is in a third country (e.g., Cayman Islands), Module Two governs. Confirm and complete the SCC docking provisions (Clause 7 — docking clause; Clause 9(a) — sub-processor authorization mode; Clause 11(a) — independent dispute-resolution; Clause 17 — governing law; Clause 18 — choice of forum and jurisdiction).]*

5.1.2 **UK transfers.** The UK International Data Transfer Addendum to the EU SCCs ("**UK IDTA Addendum**"), in the form issued by the UK Information Commissioner under section 119A of the UK Data Protection Act 2018, is hereby incorporated by reference. Alternatively, the UK International Data Transfer Agreement ("**UK IDTA**") in its standalone form applies where the UK Addendum is not used.

5.1.3 **Swiss transfers.** The EU SCCs as referenced in Section 5.1.1, modified for Swiss FADP applicability per the FDPIC guidance (in particular: references to "GDPR" shall be read as "FADP" and references to EU supervisory authorities shall be read as references to the FDPIC, and Swiss governing law shall apply where the data subjects are in Switzerland).

5.2 **Conflict.** In the event of any conflict between this DPA and the SCCs / UK IDTA, the SCCs / UK IDTA control.

5.3 **Transfer Impact Assessment.** iogrid has conducted, and will keep current, a Transfer Impact Assessment (TIA) for transfers to its principal sub-processor jurisdictions (Annex 3). iogrid makes the TIA available to the Controller on request.

*[COUNSEL: complete TIA before finalization. The Schrems II decision and EDPB Recommendations 01/2020 require a documented TIA. Key risk jurisdiction is the US (sub-processors include Stripe, AWS, etc.); the EU-US Data Privacy Framework (in force since July 2023) may simplify but does not eliminate the analysis. Recommend reviewing sub-processor DPF certifications.]*

---

## 6. Audit rights

6.1 **Annual audits.** The Controller may audit iogrid's compliance with this DPA no more than **once per calendar year**, on at least **30 days' prior written notice**, at the Controller's expense, and during iogrid's normal business hours. Audits must not unreasonably disrupt iogrid's operations and must be conducted under reasonable confidentiality terms.

6.2 **Third-party auditor.** The Controller may engage a qualified independent third-party auditor (not a competitor of iogrid) to conduct the audit on its behalf.

6.3 **Audit reports in lieu.** iogrid may satisfy its audit obligation by providing recent SOC 2 Type II reports, ISO 27001 certifications, or similar industry-standard third-party audit reports, where these address the matters the Controller wishes to audit. *[COUNSEL: confirm — iogrid does not currently hold SOC 2 / ISO 27001. Per docs/LEGAL.md open items, SOC 2 Type II is a Phase 3 target. Pending that, Customer audits would be direct. Recommend explicitly stating that the in-lieu mechanism is not yet available.]*

6.4 **Investigation-triggered audits.** Where the Controller has a reasonable, good-faith basis to believe iogrid has materially breached this DPA, the Controller may conduct an audit outside the once-per-year limit, on reasonable shorter notice (not less than 10 business days). If the audit confirms a material breach, iogrid bears the audit cost.

---

## 7. Liability and indemnification

7.1 **Allocation.** Each party is liable for the damages it causes by any processing that infringes the GDPR, UK GDPR, FADP, or applicable Data Protection Laws. Section 82 GDPR (and analogous provisions) governs the allocation of liability vis-à-vis data subjects.

7.2 **Indemnification (data-protection-specific).** Each party will indemnify the other against any claim, fine, or penalty imposed by a supervisory authority or in a civil suit by a data subject, to the extent the indemnifying party's breach of this DPA caused the claim, subject to the liability caps in the main agreement.

7.3 **Caps.** Liability caps in the main agreement apply to claims under this DPA, except where overridden by mandatory law (Article 79 / 82 GDPR rights to compensation).

---

## 8. Term and termination

8.1 **Term.** This DPA enters into force on the effective date of the main agreement and terminates on the effective date of termination of the main agreement, except for surviving obligations described in this DPA and the main agreement.

8.2 **Survival.** Sections 3.7 (breach notification, as to events occurring before termination), 3.8 (return or deletion), 5 (international transfers, as to data still being transferred), 6 (audit rights, for 6 months post-termination), 7 (liability), and this Section 8 survive termination.

---

## 9. Order of precedence

In the event of conflict, the order of precedence is: (1) the SCCs / UK IDTA (where applicable), (2) this DPA, (3) the main agreement, (4) any other operative document.

---

## 10. Annex 1 — Description of processing

### 10.1 Categories of data subjects

The personal data processed concerns the following categories of data subjects (as applicable to the Controller's use):

- **For Customer-Controller use cases:** the Customer's end users, the Customer's business contacts, individuals identified in public datasets the Customer scrapes (e.g., social-media users, business-directory entries), and any data subjects whose data is contained in Workload inputs or outputs.
- **For Provider-Controller use cases:** the Provider individuals, the Provider's household members (if any data about them is incidentally collected — generally none, see Privacy Policy).

### 10.2 Categories of personal data

Depending on the Workload type:

- **Bandwidth Workloads:** destination URLs (which may contain personal data in path or query — e.g., a request to `example.com/users/jdoe`); source / destination IP addresses; timestamps; byte volumes. **NOT the substantive content of HTTPS traffic — this is not decrypted by iogrid.**
- **Docker / GPU Workloads:** whatever the Customer's container handles. iogrid does not inspect container payloads beyond resource accounting (CPU, memory, GPU utilization, runtime).
- **iOS-Build Workloads:** source code from the Customer's repository (which may contain personal data if developers have committed such, e.g., in fixture files); build outputs.
- **Customer / Provider account data:** name, email, billing address, payment method tokens, KYC documents (for Pro / Enterprise Customers — see KYC thresholds in AUP Section 4.5).

### 10.3 Special categories of data

iogrid does not solicit special-category data (Article 9 GDPR — racial / ethnic origin, political opinions, religious beliefs, health, sexual orientation, biometric data, genetic data) but may incidentally process it where Customer Workloads handle such data. Customer is solely responsible for the lawful basis under Article 9(2) for any such processing.

### 10.4 Frequency of processing

Continuous, for the term of the main agreement.

### 10.5 Nature and purpose

See Section 2 of the main DPA body.

### 10.6 Recipients

iogrid personnel with a need to know (operations, security, legal); sub-processors per Annex 3; law-enforcement and regulators where legally compelled.

### 10.7 Retention

Per Section 3.8 of this DPA, Privacy Policy, and Provider / Customer ToS.

---

## 11. Annex 2 — Technical and organizational measures (Article 32 GDPR)

iogrid implements the following measures. *[COUNSEL: this list is a starting point; specific controls should be expanded based on iogrid's actual security posture and any SOC 2 / ISO 27001 documentation.]*

### 11.1 Pseudonymization and encryption

- All data in transit between iogrid components and between iogrid and external parties is encrypted via TLS 1.2+ (preferred TLS 1.3);
- All data at rest in iogrid's primary data stores is encrypted via AES-256 or equivalent;
- All bandwidth Workloads forward TLS-encrypted bytes without decryption;
- Audit Logs include Customer IDs that are not direct personal identifiers; reverse lookup to Customer account is access-controlled.

### 11.2 Confidentiality, integrity, availability, and resilience

- Multi-tenant isolation between Customer accounts;
- Role-based access control (RBAC) within iogrid;
- Code review and pull-request gating for production deployments;
- Multi-region replication of Audit Logs and configuration data;
- Documented disaster-recovery procedures (RPO ≤ 1 hour, RTO ≤ 4 hours for the Coordinator);
- Vulnerability scanning of container images before scheduling.

### 11.3 Ability to restore availability

- Automated daily backups of configuration and Audit Log data with 90-day retention;
- Tested restore procedures quarterly.

### 11.4 Testing and evaluation

- Quarterly internal security review;
- Annual external penetration test (planned for Phase 2);
- Continuous dependency vulnerability monitoring.

### 11.5 Sub-processor management

- Annex 3 list maintained current;
- Sub-processor due-diligence on engagement (security review, DPA execution);
- Annual re-review of sub-processor compliance.

### 11.6 Incident management

- 24/7 on-call rotation for security and abuse incidents;
- Defined incident severity levels and response SLAs;
- Breach notification to Controller within 72 hours (Section 3.7).

### 11.7 Personnel measures

- Background checks for personnel with access to production systems (where lawful in the personnel's jurisdiction);
- Confidentiality undertakings for all personnel and contractors;
- Annual data-protection training.

### 11.8 Physical security

- Production infrastructure is hosted at sub-processor data centers with documented physical-security controls (see Annex 3);
- iogrid offices: access-controlled, visitor-logged.

---

## 12. Annex 3 — Sub-processors

*[COUNSEL: this list must be kept current. Update at each new sub-processor engagement. Customer must have 30 days' prior notice of additions per Section 3.4.]*

| Sub-processor | Purpose | Location of processing | Transfer mechanism for EU/UK/CH transfers |
|---------------|---------|------------------------|------------------------------------------|
| **Stripe, Inc.** | Payment processing (cash payouts to Providers; Customer billing for fiat payments) | USA + Ireland | EU SCCs + DPF; UK IDTA Addendum |
| **Sumsub** or **Persona** | KYC / AML verification for Providers and Customers above KYC thresholds | UK + USA *[COUNSEL: confirm vendor selection and primary processing location]* | EU SCCs + DPF; UK IDTA |
| **Hetzner Online GmbH** | Coordinator hosting (primary region: Germany) | Germany | Intra-EEA (no transfer mechanism required) |
| **AWS** (Amazon Web Services, Inc.) | Coordinator hosting (secondary regions); object storage | Ireland (eu-west-1) + USA (us-east-1) *[COUNSEL: confirm which regions]* | EU SCCs + DPF; UK IDTA |
| **NCMEC (National Center for Missing & Exploited Children)** | Hash-list checks for CSAM filtering | USA | Limited transfer (hash queries only, no personal data of data subjects); analyzed as non-applicable transfer |
| **INHOPE / national hotlines** | CSAM-incident reporting | Mixed | Statutory reporting obligation; not a commercial transfer |
| **PhishTank** | Phishing-list checks | USA | Limited transfer (URL queries only) |
| **OpenPhish** | Phishing-list checks | USA | Limited transfer |
| **Google Safe Browsing** (Google LLC) | Phishing / malware-list checks | USA | EU SCCs + DPF (Google's published terms) |
| **MoonPay** | Off-ramp ($GRID / USDC → fiat) for token-tier Providers | USA + Ireland *[COUNSEL: confirm]* | EU SCCs + DPF |
| **Solana RPC providers (Helius, etc.)** | Blockchain RPC for $GRID transactions | USA | EU SCCs |
| **Sentry / similar** | Application error monitoring | *[COUNSEL: confirm vendor]* | EU SCCs (if non-EEA) |
| **Cloudflare** | DDoS mitigation, CDN, DNS | Global | EU SCCs + DPF |
| **GitHub** (Microsoft Corporation) | Source code hosting; not Customer-data processing | USA | EU SCCs + DPF |
| **Datadog or similar** | Observability / metrics / logs | *[COUNSEL: confirm vendor]* | EU SCCs (if non-EEA) |
| *[COUNSEL: add any other sub-processors discovered during stack audit]* | | | |

---

## 13. Annex 4 — Contact

- **iogrid Data Protection Officer / Privacy Lead:** *[COUNSEL: insert. Confirm whether iogrid must appoint a formal DPO under Article 37 GDPR. The threshold is "core activities consist of processing operations which … require regular and systematic monitoring of data subjects on a large scale" or "processing on a large scale of special categories of data." iogrid likely meets the first criterion at Phase 2+ scale.]*
- **iogrid privacy contact:** *[COUNSEL: insert, e.g., privacy@iogrid.org]*
- **EU Representative under Article 27 GDPR** (where iogrid is established outside the EU): *[COUNSEL: appoint EU representative before processing EU data subjects' data at scale, or document why Article 27 does not apply. Recommend a vendor like VeraSafe or DataRep.]*
- **UK Representative under Article 27 UK GDPR**: *[COUNSEL: equivalent appointment for UK if iogrid is outside the UK.]*

---

*End of Data Processing Agreement v0.1-draft.*

*[COUNSEL: full-document review required. Critical open items: SCC Module selection and docking clauses (Section 5.1.1), Transfer Impact Assessment completion (Section 5.3), sub-processor list verification + DPA execution with each (Annex 3), DPO / Article 27 representative appointments (Annex 4), security-measures completeness (Annex 2), specific clauses for special categories of data if processing scope expands (Annex 1.3), audit-report-in-lieu mechanism (Section 6.3). Total `[COUNSEL]` markers: ~15.]*
