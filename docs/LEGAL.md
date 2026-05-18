# Legal scaffolding

## Risk landscape

iogrid sits in the same regulatory neighborhood as Bright Data, Honeygain, Pawns.app, IPRoyal, Proxycurl, and Salad. Public-data scraping itself is **not** illegal in the US per the *hiQ Labs v. LinkedIn* (CA9, 2017–2022, eventually settled) decision — but every node in the supply chain has potential exposure:

- **Providers** — their IP gets blamed for traffic they didn't initiate
- **Coordinator** (us) — secondary-liability theories under copyright (DMCA), trespass-to-chattels, ToS violations of the destination services
- **Customers** — primary liability for whatever they're doing

Active litigation in 2024–2025 includes Meta v. Bright Data (Bright Data won, scraping public data ruled lawful), X v. Bright Data (still pending), and various CFAA cases involving smaller scrapers.

**Historical precedent for providers (where it's gone wrong):**

- **William Weber (Austria, 2014):** Ran Tor exit relay; CSAM transited through his IP; **convicted, 4 years probation.** No anti-abuse filtering, no commercial intermediary, no defense fund.
- **Moritz Bartl, Zwiebelfreunde e.V. (Germany):** Operates Tor exits via nonprofit, multiple home raids since 2012; charges dropped each time, but each raid = months of legal stress, lawyer fees.
- **Nolan King (US, 2007):** FBI raid for CSAM allegedly distributed via Tor exit; 2 years of legal hell; charges eventually dropped.
- **Honeygain & Bright Data providers:** No personal lawsuits known. Bright Data's TOS makes them the legal target; providers are shielded.

The reason commercial intermediaries take the legal hit is: deeper pockets, stronger anti-abuse defenses, central audit logs that pinpoint customers. We have to maintain those defenses or we lose the liability shield.

---

## Mandatory anti-abuse before any external provider joins

These are blockers for Phase 1. They must be functional and verified before we onboard the first external provider.

### Bandwidth workload pre-flight filters

For every outbound destination, before iogrid relays traffic:

1. **CSAM filter** — destination URL host + hash check against NCMEC's PhotoDNA database (free for registered orgs) and INTERPOL's hash list
2. **Phishing / fraud filter** — check destination against PhishTank, OpenPhish, Google Safe Browsing (free APIs)
3. **Outbound port restrictions:**
   - No SMTP outbound (port 25, 465, 587, 2525) — no spam
   - No IRC (port 6667, 6697) — no DDoS coordination
   - No Tor exit ports (9001, 9030) — don't be a Tor exit ourselves
   - No SSH brute-force patterns (rate-limit per-target SSH)
4. **High-risk target list:**
   - Banking domains: customer must explicitly request, KYC verified
   - Government domains (.gov, .mil): block unconditionally
   - Adult content domains: provider must explicitly opt-in
5. **Per-customer rate limits:**
   - Default: 100 RPS aggregate
   - Premium tier: 1000 RPS aggregate, KYC required
6. **Per-provider rate limits per destination:**
   - No single provider IP serves more than 100 RPS to any one destination
   - Hot destinations (LinkedIn, Facebook, Twitter, Google): max 10 RPS per provider per destination

### Docker workload filters

1. Container image must come from approved registry (default: ghcr.io, docker.io official-images, Dockerhub-verified-publisher namespace)
2. Coordinator scans image's published vulnerability list before scheduling
3. Network namespace inside container: only outbound through iogrid bandwidth router (same filters above apply)
4. No privileged containers, no host filesystem mount, no host network namespace
5. Resource caps enforced via cgroups
6. Per-customer container submission rate limit

### iOS-build workload

1. Source code must come from a Git URL the customer authenticates with their token (we don't store the repo)
2. Tart VMs are ephemeral — destroyed after build, no state carries across customers
3. Build output uploaded to coordinator's S3 with per-customer encryption keys
4. Build time-boxed (default 30 min, max 4 hours)

### Customer KYC thresholds

| Customer monthly spend | KYC requirement |
|------------------------|-----------------|
| <$100 | Email verification only |
| $100–500 | Business email + LinkedIn / corporate confirmation |
| $500–5K | Manual review, government ID for principal |
| >$5K | Stripe Identity + business registration verification + AML check |

---

## Required documents (Phase 1 prerequisites)

These must be drafted by qualified counsel before external onboarding. Total cost expected: $5–10K.

### 1. Provider Terms of Service

Must include:

- **Consent statement:** "I authorize iogrid to make my device act as a network exit for third-party traffic. I understand my IP will be visible to those third parties. I understand my IP may be flagged or temporarily blocked by some services as a result."
- **Common-carrier defense language:** "I act as a passive bandwidth intermediary. I have no knowledge of the content of the traffic I relay. iogrid operates pre-flight filters to block illegal content."
- **Indemnification clause:** "iogrid will defend, indemnify, and hold you harmless from claims arising from third-party use of your bandwidth, EXCEPT where you have violated this Agreement (e.g., disabled anti-abuse filters, knowingly routed illegal traffic)."
- **Audit-cooperation clause:** "iogrid retains 90 days of audit logs identifying the customer behind each request through your IP. We will use these to respond to law-enforcement inquiries and direct investigations away from you."
- **Revocation rights:** "You may pause or uninstall at any time. Pausing kills traffic instantly. Uninstalling deletes all telemetry from your device."
- **Tax compliance:** US providers earning >$600/year receive a 1099-NEC. EU providers handle local tax. We collect W-9 / W-8BEN on signup.

### 2. Privacy Policy

Must include:

- What we log (bandwidth volume per device, uptime, approximate location for geo-targeting — never traffic content)
- Retention period: 90 days for audit logs, anonymized aggregates indefinitely
- GDPR / CCPA / Brazilian LGPD lawful basis declarations
- Data subject rights (access, deletion, portability)
- Sub-processor list (Stripe, NCMEC, PhishTank, Google Safe Browsing)

### 3. Data Processing Agreement (DPA)

EU-required addendum to ToS. Specifies iogrid as a data processor when the provider is acting as a data subject, and specifies iogrid's processor obligations.

### 4. Acceptable Use Policy (AUP)

What providers and customers cannot do via iogrid. Covers:

- No CSAM (zero tolerance)
- No human trafficking, exploitation, or harassment
- No critical-infrastructure attacks (utilities, healthcare, finance)
- No election interference
- No DDoS, including stress-testing without owner consent
- No carding, fraud, or financial-crime facilitation
- No mass-credential testing (credential stuffing)
- No bypass of anti-spam (no email-spam relay, no SMS-pumping)

Violations: immediate termination, audit-log forensics shared with law enforcement.

### 5. Customer Terms of Service + AUP

Customer-side analog, with:

- Liability cap (we cap our liability at the customer's most-recent monthly spend × 12)
- Indemnity from customer (they hold us harmless for legal claims arising from their requests)
- Right of refusal (we can refuse to serve any customer at any time, for any reason)
- Audit rights (we may audit a customer's usage on suspicion of policy violation)

---

## Legal defense fund

Phase 1 starts with **$10K initial pool**, replenished by **5–10% of B2B revenue** monthly.

Purpose:
- Cover provider legal fees if their IP is subpoenaed or LEO contacts them
- Cover our own ToS-defense litigation costs (Section 230, common-carrier arguments)
- Retain outside counsel on a recurring basis (a partner-level tech attorney, ~$2K/month retainer once Phase 2)

Disbursement criteria:
- Provider received subpoena, LEO contact, or civil claim arising from iogrid traffic → fund pays their reasonable lawyer fees up to $25K
- Provider violated AUP → fund pays nothing
- Customer subpoenaed → fund pays nothing (customer is on their own per their ToS)

---

## Insurance (Phase 2 prerequisite)

- **Cyber liability:** $1M coverage, ~$3K/year (covers data breach, ransomware, business interruption from cyber-attack)
- **E&O / Tech E&O:** $1M coverage, ~$5K/year (covers professional negligence claims by customers)
- **D&O:** $1M coverage, ~$2K/year (covers Dynolabs directors)

---

## Jurisdiction & corporate structure

**Operating entity:** Dynolabs (existing). iogrid is a product line within Dynolabs, not a separate subsidiary at Phase 0/1. Phase 2 decision whether to spin out as separate company for liability isolation.

**Customer contracts governed by:** Delaware law (if Dynolabs is DE-incorporated; otherwise wherever Dynolabs is registered).

**Disputes:** binding arbitration in our home jurisdiction, customer waives class-action rights. Standard SaaS pattern.

**Provider contracts:** governed by provider's home jurisdiction (GDPR / regional consumer-protection laws are mandatory regardless).

---

## Cooperation with law enforcement

- **Subpoena response:** standard, redirect to customer who owns the relevant audit log entry
- **MLAT / international requests:** process via outside counsel
- **Transparency report:** publish quarterly Phase 2 onward (number of requests received, jurisdictions, compliance percentage)
- **Warrant canary:** Phase 3 consideration (some networks use these; legal value debated)

---

## What we won't do

- **Provider data sale** — bandwidth usage data is the provider's. We never sell it.
- **Backdoor access** — we won't insert backdoors at any government's request. Audit logs are accessible via subpoena, but no special-access channel.
- **Traffic content interception** — we relay encrypted bytes; we do not decrypt customer's HTTPS, ever.
- **Crypto-money-laundering services** — banking, mixer, sanctioned-jurisdiction targets are blocked

---

## Open items for Phase 1 lawyer brief

1. Should iogrid form a separate LLC for liability isolation from Dynolabs core?
2. EU AI Act considerations for GPU/AI workload (we're "compute provider" not "model provider," but the line is unclear)
3. SOC 2 Type II target date — required for any enterprise customer at Phase 3
4. Provider 1099 / VAT collection automation (currently manual via Stripe Connect)
5. Drafting of warrant canary policy
