# Provider Terms of Service

**Status:** Draft v0.1 — pre-counsel-review. Not legal advice. See [`legal/README.md`](./README.md).
**Effective date:** *[COUNSEL: insert effective date upon finalization]*
**Governing entity:** *[COUNSEL: confirm operating entity — currently "Dynolabs Inc., operator of iogrid" pending Phase 2 spin-out decision]*

> **Plain-language summary** *(non-binding, courtesy section — the binding terms are below).*
>
> By installing the iogrid daemon, you agree to let your computer act as a relay for other people's network traffic and, optionally, run their Docker containers, GPU workloads, or macOS-native iOS builds. In exchange you may earn cash (via Stripe), free unlimited VPN, a charity donation, or $GRID tokens. You can stop at any time. iogrid runs real-time filters to block illegal traffic; we keep 90 days of audit logs so that if anyone complains about something that went through your IP, we can point the finger at the actual customer instead of you. We also promise to defend you legally if you get sued or contacted by law enforcement over traffic you relayed — **as long as you did not knowingly violate this Agreement.**

---

## 1. Definitions

For the purposes of this Agreement, the following capitalized terms have the meanings set out below:

- **"iogrid"**, **"we"**, **"us"**, **"our"** — the operating entity described above and any of its affiliates that operate the iogrid network. *[COUNSEL: replace with finalized entity once chosen.]*
- **"Provider"**, **"you"**, **"your"** — the individual or legal entity that installs the Daemon and agrees to these terms.
- **"Daemon"** — the iogrid provider software, distributed as a Rust binary (with platform-specific installers for Windows, macOS, and Linux) that runs on your device.
- **"Coordinator"** — iogrid's central infrastructure (control plane, scheduler, billing, audit logging) which dispatches Workloads to Providers.
- **"Customer"** — a third party that has accepted the iogrid Customer Terms of Service and pays iogrid to use the network.
- **"Workload"** — a unit of work dispatched to your Device, including (a) Bandwidth Workloads (your IP relays a Customer's network request), (b) Docker Workloads (your Device runs a containerized job), (c) GPU Workloads (your Device runs a compute or inference job), and (d) iOS-Build Workloads (your Mac builds a Customer's iOS application via Tart VMs).
- **"Device"** — the personal computer, server, or workstation on which the Daemon is installed.
- **"Audit Log"** — the per-request record we retain identifying which Customer initiated each Workload routed through your Device, plus volume, destination, and timing metadata. Audit Logs do **not** contain traffic content (we never decrypt HTTPS or other encrypted traffic).
- **"AUP"** — the iogrid Acceptable Use Policy at [`legal/aup.md`](./aup.md), incorporated by reference.
- **"DPA"** — the iogrid Data Processing Agreement at [`legal/dpa.md`](./dpa.md), applicable to Providers in the European Economic Area, the United Kingdom, and Switzerland, and to any other Provider who elects to be bound by it.
- **"$GRID"** — the iogrid network token described in [`docs/BUSINESS-STRATEGY.md` §4 Currency model](../docs/BUSINESS-STRATEGY.md#4-currency-model--grid--fiat-hybrid) and the risk factors in [`legal/token-disclaimer.md`](./token-disclaimer.md).

*[COUNSEL: confirm definition list is complete; add jurisdiction-specific defined terms as required (e.g., "Consumer" under EU Directive 2011/83/EU, "Independent Contractor" under US tax framework).]*

---

## 2. Eligibility

You may register as a Provider only if **all** of the following are true at the time of registration and continuously thereafter:

1. **Age.** You are at least 18 years old, or the age of legal majority in your jurisdiction, whichever is greater.
2. **Capacity.** You have the legal capacity to enter into a binding contract under the laws of your jurisdiction. *[COUNSEL: in some jurisdictions individuals between 16 and 18 may register with parental consent — decide whether to support that flow.]*
3. **Device ownership.** You own the Device on which the Daemon is installed, or you have the explicit, documented authority of the Device's owner to install the Daemon. iogrid is not responsible for unauthorized installations.
4. **Network authority.** You have the explicit authority to share the internet connection used by the Device. If you are on a network you do not own (e.g., an employer's network, a school's network, a hotel network, a public Wi-Fi network, a shared apartment network where the bill-payer has not consented), **you may not register**.
5. **Sanctioned countries.** You are not resident in, ordinarily located in, or a national of, any country subject to comprehensive economic sanctions by the United States, the European Union, the United Kingdom, or the United Nations Security Council. As of the effective date this list includes (without limitation): Cuba, Iran, North Korea, Syria, Russia (sectoral sanctions), Belarus (sectoral sanctions), the Crimea, Donetsk, Luhansk, Kherson, and Zaporizhzhia regions of Ukraine. *[COUNSEL: review sanctioned-country list for currency and accuracy; this list changes frequently. Recommend automated screening at signup via Sumsub or Persona.]*
6. **Sanctioned persons.** You are not listed on, and not owned 50% or more by any person listed on, the US OFAC Specially Designated Nationals (SDN) List, the EU Consolidated Financial Sanctions List, the UK HM Treasury Consolidated List, or any analogous list maintained by a competent authority. *[COUNSEL: confirm screening regime.]*
7. **No prior termination.** You have not previously been terminated from the iogrid network for a violation of these terms or the AUP, unless we have expressly re-enabled your account in writing.

If at any time you cease to satisfy any of these eligibility requirements, you must immediately suspend or uninstall the Daemon and notify us at *[COUNSEL: insert eligibility-change contact, e.g., legal@iogrid.org]*.

---

## 3. Consent — mandatory statement

> **By registering as a Provider and clicking "I agree" or by running the Daemon, you make the following statement:**
>
> **"I authorize iogrid to make my device act as a network exit for third-party traffic. I understand my IP will be visible to those third parties. I understand my IP may be flagged or temporarily blocked by some services as a result."**

This statement is required under our compliance framework. We log the timestamp, IP address, and Daemon version at which you affirm this statement. You may revoke this consent at any time by pausing or uninstalling the Daemon (Section 9).

*[COUNSEL: confirm this consent statement is enforceable in target jurisdictions (US, EU, UK at minimum). Recommend a separate, prominently displayed click-through dialog rather than relying solely on inclusion in the ToS body. Consider double opt-in via email confirmation for EU/UK Providers.]*

---

## 4. Common-carrier and passive-intermediary framing

> **"I act as a passive bandwidth intermediary. I have no knowledge of the content of the traffic I relay. iogrid operates pre-flight filters to block illegal content."**

You acknowledge and we acknowledge that, with respect to Bandwidth Workloads:

1. **You do not select destinations.** The Coordinator chooses which Customer requests are routed through your Device. You have no input into the destination, content, or purpose of any individual request.
2. **You cannot inspect the content.** Traffic is forwarded as opaque, encrypted (TLS) bytes. Neither you nor we decrypt or otherwise read the payload of HTTPS traffic. This is by design (see Section 11).
3. **iogrid operates pre-flight filters before routing traffic through your Device,** as specified in [`docs/BUSINESS-STRATEGY.md` §6 Legal risk landscape](../docs/BUSINESS-STRATEGY.md#6-legal-risk-landscape--mitigation) §"Mandatory anti-abuse" and the operational filters listed in the AUP. These include but are not limited to: NCMEC / INTERPOL hash checks for CSAM, phishing-database checks (PhishTank, OpenPhish, Google Safe Browsing), outbound port restrictions (no SMTP, no IRC, no Tor exit ports), high-risk target blocks (.gov, .mil unconditionally; banking and adult domains restricted), and per-customer / per-provider rate limits.
4. **The legal posture we assert on your behalf** is that you operate as a passive bandwidth intermediary, comparable in function to a common carrier or to a hosting provider under the safe-harbor doctrines of the US Digital Millennium Copyright Act (17 U.S.C. §512(a)), the EU Digital Services Act (Regulation (EU) 2022/2065) Article 4 "mere conduit" exemption, and analogous safe-harbor provisions in other jurisdictions. *[COUNSEL: confirm which safe-harbor framings apply in each launch jurisdiction. The US "mere conduit" 512(a) safe harbor and the EU DSA Art. 4 framing are not identical. Some jurisdictions (e.g., Germany) have additional Telemediengesetz / DDG requirements.]*

We do not promise, and you should not rely on, any guarantee that this framing will be accepted by a court in any specific dispute. The framing represents our good-faith legal posture and is one of several reasons we maintain the pre-flight filters described above.

*[COUNSEL: this section should not over-promise immunity. Recommend rewording to make clear that common-carrier framing is asserted, not adjudicated. Also confirm whether the analogy to common-carrier status is appropriate vs. potentially misleading in each jurisdiction.]*

---

## 5. Indemnification — iogrid defends you

> **"iogrid will defend, indemnify, and hold you harmless from claims arising from third-party use of your bandwidth, EXCEPT where you have violated this Agreement (e.g., disabled anti-abuse filters, knowingly routed illegal traffic)."**

5.1 **Scope of defense.** We will, at our own expense, defend you against any third-party claim, lawsuit, regulatory inquiry, subpoena, civil-discovery request, or law-enforcement investigation that arises out of, or is alleged to arise out of, traffic that the Coordinator routed through your Device pursuant to a Bandwidth Workload, Docker Workload, GPU Workload, or iOS-Build Workload.

5.2 **What we cover.** Subject to Section 5.4 and the cap in Section 5.5, our defense includes:
- Retention of qualified counsel of our choosing on your behalf (we will consult you on counsel selection in good faith, but final selection is ours);
- Reasonable attorneys' fees and disbursements;
- Court costs;
- Reasonable costs of complying with subpoenas, including any of your costs incurred to preserve records;
- Settlements, where we determine settlement is in your interest, with your written consent (which you may not unreasonably withhold).

5.3 **Conditions to coverage.** Our obligation to defend is conditional on:
- You notifying us in writing at *[COUNSEL: insert legal contact email, e.g., legal@iogrid.org]* within **5 business days** of receiving any claim, subpoena, or law-enforcement contact arising from iogrid traffic;
- You not making any admission of liability, settlement offer, or substantive statement to the third party or authority without our prior written consent (this restriction does not prevent you from disclosing factually accurate information when legally compelled to do so);
- You providing reasonable cooperation, including access to relevant records, sworn declarations, and your presence at depositions or hearings as required;
- You allowing us to control the defense, including counsel selection and litigation strategy (Section 5.2).

5.4 **Exclusions.** We will NOT defend or indemnify you, and you instead agree to defend and indemnify us, for any claim, lawsuit, or proceeding to the extent it arises out of:
- Your violation of this Agreement or the AUP;
- Your disablement, circumvention, or tampering with the Daemon's anti-abuse filters, telemetry reporting, or audit-log forwarding;
- Your willful or grossly negligent routing of illegal traffic (e.g., you affirmatively chose to allow Workloads that you knew or had reason to know were unlawful);
- Activity that occurred on the Device but was not routed by the Coordinator (e.g., your own personal browsing or other unrelated misconduct on the same IP);
- Your failure to maintain the Device in a reasonably secure condition (e.g., the Device was compromised by malware that allowed a third party to use the IP for non-iogrid purposes, in which case the third-party use is not covered);
- Your breach of any applicable law unrelated to iogrid (e.g., tax fraud committed in connection with your payout reporting).

5.5 **Liability cap.** Our aggregate liability to defend and indemnify you under this Section 5 is capped per Section 16. *[COUNSEL: confirm whether this cap should apply to defense costs or be carved out — many indemnity defenders agree to defend without cap and cap only ultimate damages.]*

5.6 **Defense fund disclosure.** We currently maintain a legal defense fund as described in [`docs/BUSINESS-STRATEGY.md` §6 Legal risk landscape](../docs/BUSINESS-STRATEGY.md#6-legal-risk-landscape--mitigation) §"Legal defense fund," initially capitalized at $10,000 USD and replenished by 5–10% of monthly B2B revenue. This fund is the practical source of legal defense for Providers. The fund is not held in trust on your behalf; it is an unrestricted operational reserve, and our defense commitment in this Section 5 is a contractual promise from iogrid, not a beneficial interest in the fund.

*[COUNSEL: review whether describing the defense fund creates a separate fiduciary obligation. Some jurisdictions may treat a "defense fund" as a fiduciary structure if marketed that way. Recommend either (a) make clear it is an operational reserve, not a separate legal entity, or (b) actually structure it as a separate trust/escrow and adjust language accordingly.]*

---

## 6. Audit-cooperation clause

> **"iogrid retains 90 days of audit logs identifying the customer behind each request through your IP. We will use these to respond to law-enforcement inquiries and direct investigations away from you."**

6.1 **Log retention.** We retain Audit Logs for **90 days** from the time the Workload completes. After 90 days, individual Audit Log entries are deleted, and only anonymized aggregate statistics remain. Aggregate statistics never identify individual Providers, Customers, or destinations. *[COUNSEL: confirm 90-day retention is compatible with each jurisdiction's data-protection minimization principle while still meeting realistic law-enforcement subpoena windows. EU GDPR Art. 5(1)(e) requires storage limitation. UK ICO and German data-protection authorities have specific guidance on retention.]*

6.2 **What's in the logs.**
- Timestamp of the Workload (start and end);
- Customer ID (an opaque identifier that we can correlate to the Customer's iogrid account);
- Workload type (bandwidth, Docker, GPU, iOS build);
- For Bandwidth Workloads: destination domain or IP, source port, destination port, byte counts in each direction;
- For Docker / GPU / iOS-Build Workloads: container image reference (for Docker), GPU model used, runtime in seconds, exit code;
- Your Provider ID and the Device's IP address as observed by the destination;
- Geographic region of the Device (country and approximate city, not full street address).

6.3 **What's NOT in the logs.**
- The content of any traffic (we do not decrypt HTTPS);
- Customer-supplied secrets (we do not log API request bodies, OAuth tokens, etc.);
- The content of any container's standard output beyond what the Customer chose to write to our designated log channel;
- Your personal browsing or any traffic generated by the Device outside the iogrid daemon's routing scope.

6.4 **Law-enforcement response.** When we receive a valid legal process (subpoena, court order, warrant, MLAT request) seeking information about a specific IP, timestamp, or destination:
- We identify the Customer whose Workload generated the traffic in question;
- We respond to the requesting authority with the Customer's identifying information (subject to any objections we may lodge — e.g., overbroad requests, requests violating the Stored Communications Act 18 U.S.C. §2702 or the EU "GDPR conflict" defense);
- We notify you that a request was received naming your IP, **unless we are legally prohibited from doing so** (e.g., a non-disclosure order accompanies the subpoena);
- We provide you the contact information of the Customer (where legally permissible) so that you and your counsel can coordinate with the actually-responsible party.

6.5 **Audit-log access by you.** You may request a copy of all Audit Log entries naming your Provider ID within the 90-day retention window by emailing *[COUNSEL: insert privacy contact, e.g., privacy@iogrid.org]*. We will respond within 30 days. *[COUNSEL: confirm 30-day response is acceptable under GDPR Art. 12(3) (which allows 1 month, extendable by 2 months for complex requests).]*

---

## 7. Workload-specific provisions

7.1 **Bandwidth Workloads.** Your Device may be assigned to relay outbound HTTPS, HTTP, WebSocket, or other TCP/UDP traffic on behalf of a Customer. The Coordinator selects destinations subject to the AUP and the pre-flight filters described in Section 4(3). You acknowledge that destinations may include, but are not limited to: e-commerce sites, search engines, social media platforms, advertising networks, public APIs, news sites, and other publicly available web content.

7.2 **Docker Workloads.** Your Device may be assigned to run containerized jobs from the Customer's container registry. Containers are sandboxed with the following protections: non-privileged execution, no host-filesystem mount, no host-network namespace, network-namespace-isolated outbound (subject to the same anti-abuse filters as Bandwidth Workloads), cgroup resource caps, and a default 1-hour wall-clock limit (configurable by the Customer up to 24 hours). You may opt out of Docker Workloads in the Daemon settings.

7.3 **GPU Workloads.** Your Device may be assigned compute or inference jobs that use your discrete GPU (NVIDIA CUDA, Apple Metal / MLX, or AMD ROCm where supported). GPU Workloads share the sandboxing of Docker Workloads. You may opt out of GPU Workloads in the Daemon settings.

7.4 **iOS-Build Workloads.** If your Device is a Mac with Apple Silicon, you may be assigned to build iOS / macOS applications from Customer-supplied Git repositories using Apple's Tart VM technology. Tart VMs are ephemeral and destroyed after each build. Source code is never persisted to your Device beyond the build's lifetime. Builds are time-boxed to 4 hours maximum. *[COUNSEL: confirm Apple Developer Program License Agreement and Apple's terms for Tart usage do not conflict with reselling build capacity. The Apple Developer Program License Agreement §3.3.1 and Xcode license terms have historically been ambiguous on third-party build farms — recommend specific counsel review.]*

7.5 **Opt-outs.** You may disable any Workload type in the Daemon settings at any time. Changes take effect within 5 minutes. Workloads in progress are allowed to complete unless you uninstall the Daemon (Section 9).

---

## 8. Compensation and payouts

8.1 **Payout types.** At your election, you may receive compensation in one of the following forms:
- **Cash.** Paid in USD or your local equivalent via Stripe Connect or, where Stripe Connect is unavailable, via direct bank transfer with a 1% off-ramp surcharge. Minimum payout threshold $10 USD.
- **Free unlimited VPN.** Mesh-swap economics (you provide bandwidth, you receive VPN exit capacity from other Providers).
- **Charity donation.** Forwarded to a charity from our approved list (currently: Electronic Frontier Foundation, Tor Project, Wikipedia / Wikimedia Foundation; we may add or remove approved charities at our discretion).
- **$GRID token.** Subject to the lockup and vesting schedule described in [`docs/BUSINESS-STRATEGY.md` §4 Currency model](../docs/BUSINESS-STRATEGY.md#4-currency-model--grid--fiat-hybrid) and the risk factors in [`legal/token-disclaimer.md`](./token-disclaimer.md). $GRID is not available to US persons at Token Generation Event; geographic restrictions apply. **By electing $GRID payouts you confirm you have read and accept the token disclaimer.**

8.2 **Earnings calculation.** Your earnings are calculated per the formula in [`docs/BUSINESS-STRATEGY.md` §3 Unit economics and provider incentives](../docs/BUSINESS-STRATEGY.md#3-unit-economics--provider-incentives), which considers volume relayed, uptime, geographic supply/demand, and your routing-priority stake (for $GRID-tier Providers). We may modify the earnings formula prospectively with 30 days' notice (Section 21).

8.3 **Payout schedule.** Cash and $GRID payouts are processed weekly. Charity donations are batched monthly. Free-VPN entitlement is granted in real time.

8.4 **Tax compliance — US.** If you are a US person and your aggregate cash earnings from iogrid in a calendar year are $600 or more, we will issue you a Form 1099-NEC (or its then-current equivalent) and submit a copy to the IRS. You are responsible for collecting your W-9 at onboarding. *[COUNSEL: confirm $600 threshold — the IRS has periodically proposed changes; verify current threshold at finalization.]* If you receive $GRID, you will receive a Form 1099-MISC equivalent (or other appropriate form) reflecting the fair-market USD value of $GRID at time of receipt. You are responsible for your own ordinary-income tax on receipt and for any subsequent capital-gains tax on disposal.

8.5 **Tax compliance — non-US.** If you are not a US person, we will collect a W-8BEN at onboarding. You are responsible for declaring and paying any income tax, value-added tax, or other tax due in your jurisdiction. We do not currently withhold tax on payouts to non-US Providers, but we may do so in future if required by treaty or local law. *[COUNSEL: confirm withholding obligations for major Provider jurisdictions (UK, Germany, France, Netherlands, India, Brazil). VAT-reverse-charge mechanics may apply for EU B2B payouts.]*

8.6 **Provider as independent contractor.** Nothing in this Agreement makes you an employee, agent, or partner of iogrid. You are an independent contractor. You are responsible for your own income tax, social security contributions, business registration (where applicable), and any other obligations of self-employed individuals or businesses in your jurisdiction. *[COUNSEL: confirm independent-contractor framing under each launch jurisdiction's misclassification jurisprudence. AB-5 (California), the UK IR35 / "worker" tests, the German Scheinselbständigkeit doctrine, and EU Platform Work Directive 2024 all merit specific review.]*

8.7 **Token-specific provisions.** If you elect $GRID payouts, you additionally agree to the terms in [`legal/token-disclaimer.md`](./token-disclaimer.md), which is incorporated by reference. In the event of conflict between this Section 8 and the token disclaimer, the token disclaimer controls for token-specific matters.

---

## 9. Revocation rights

> **"You may pause or uninstall at any time. Pausing kills traffic instantly. Uninstalling deletes all telemetry from your device."**

9.1 **Pause.** You may click "Pause" in the Daemon UI at any time. In-flight Workloads are torn down within 5 seconds; no new Workloads are accepted while paused. Paused Providers do not earn during the pause window.

9.2 **Uninstall.** You may uninstall the Daemon at any time using your operating system's standard uninstall mechanism. Uninstallation removes:
- All Daemon binaries from your Device;
- All locally-cached telemetry pending upload;
- All locally-cached Workload artifacts (Docker layers, GPU model caches, Tart VM images);
- All Daemon configuration files including any credentials.

9.3 **What survives uninstall.** Uninstalling the Daemon does not, by itself, terminate your iogrid account or remove the server-side Audit Logs covering past Workloads (which persist for the 90-day retention window). To close your account and request full data deletion, see Section 19 (Data Subject Rights).

9.4 **Effect on earned compensation.** Uninstalling does not forfeit cash earnings already credited to you (they remain payable per the regular payout schedule). For $GRID earnings, vesting schedules continue per the rolling 30/90-day curve and the tier you elected (`docs/TOKENOMICS.md` §"Mandatory provider-earnings lockup"). Uninstalling does not trigger early-unlock penalties.

9.5 **No-penalty revocation.** There is no charge, fee, or penalty for pausing or uninstalling. We do not bill you for past usage. We will not retaliate against you in any future re-onboarding (subject to Section 2(7)).

---

## 10. Service availability

10.1 **No SLA.** iogrid does not guarantee any specific level of service availability, uptime, throughput, or earnings to Providers. The network operates on a best-effort basis. Coordinator outages, scheduler issues, demand-side fluctuations, and many other factors may reduce your earnings or pause Workload dispatch entirely.

10.2 **Maintenance windows.** We may take the Coordinator offline for maintenance with reasonable advance notice. Routine maintenance windows are announced via the Daemon UI and at *[COUNSEL: insert status page, e.g., status.iogrid.org]*.

10.3 **Emergency takedown.** We may take the Coordinator, or any Provider including yours, offline immediately and without prior notice if we have a reasonable belief that doing so is necessary to comply with law, respond to an active abuse incident, or protect the integrity of the network. We will notify affected Providers as soon as reasonably practicable after such action.

---

## 11. Data we collect from your Device

We collect only the data described in this Section 11 and in the [Privacy Policy](./privacy-policy.md). We do not collect, store, transmit, or analyze the content of any traffic that passes through your Device.

11.1 **Telemetry we collect:**
- Volume of data relayed (per Workload, per direction);
- Uptime statistics (when the Daemon was online / offline);
- Approximate geographic location (derived from your public IP — country and approximate city, never street address);
- Device hardware fingerprint (OS, CPU model, GPU model, RAM size — used for Workload-fit scheduling);
- Daemon version and runtime errors / crash reports (used for software quality).

11.2 **Telemetry we do NOT collect:**
- Content of HTTPS traffic (we forward encrypted bytes; we do not perform TLS termination);
- Content of any non-HTTPS traffic beyond what is necessary for routing decisions (destination domain extracted from SNI / Host header; URL paths and query strings are not logged);
- Files on your Device outside the Daemon's installation directory;
- Your personal browsing history;
- Audio, video, microphone, or camera input;
- Any other information not specified in Section 11.1.

11.3 **No on-Device retention beyond cache.** The Daemon caches recent telemetry locally for batch upload (typically 5-minute windows). On graceful shutdown, the cache is flushed. On crash, the cache may persist locally until next start. Uninstallation clears all cached data (Section 9.2).

11.4 **Privacy Policy reference.** Server-side handling of telemetry is governed by the [Privacy Policy](./privacy-policy.md), incorporated by reference.

---

## 12. Acceptable use

You agree to comply with the [Acceptable Use Policy](./aup.md) at all times. In particular, you agree NOT to:
- Disable, circumvent, or tamper with the Daemon's anti-abuse filters, telemetry reporting, or audit-log forwarding;
- Run modified versions of the Daemon that report falsified telemetry, that pretend to be in a different geographic location than the Device actually is, or that otherwise deceive the Coordinator;
- Resell access to your Device's IP through any other proxy or VPN service while simultaneously running the iogrid Daemon (concurrent operation creates audit ambiguity and is prohibited);
- Use the Daemon on a Device whose internet connection is provided in violation of your contract with your ISP (some residential ISP contracts prohibit "running a server" or "commercial use"; you are responsible for verifying your own ISP's terms);
- Engage in any conduct prohibited by the AUP, this Agreement, or applicable law.

Violations may result in immediate suspension or termination (Section 14) and forfeit our indemnification commitment (Section 5.4).

*[COUNSEL: review the ISP-contract clause. Some jurisdictions (e.g., several EU member states under net-neutrality rules) prohibit ISPs from restricting non-commercial peer use of residential connections; in others (US, many APAC jurisdictions) ISPs may prohibit. Verify language is not over-broad.]*

---

## 13. Intellectual property

13.1 **Daemon license.** We grant you a non-exclusive, non-transferable, revocable, royalty-free license to install and run the Daemon on Devices that you own or operate with permission, solely for the purpose of participating in the iogrid network. You may not reverse-engineer, decompile, disassemble, or otherwise attempt to derive the source code of the Daemon, except to the extent expressly permitted by applicable law (e.g., for interoperability under EU Directive 2009/24/EC).

13.2 **Telemetry license.** You grant us a non-exclusive, royalty-free, worldwide license to use the telemetry data described in Section 11 for the purposes of operating the iogrid network, calculating your payouts, satisfying our legal and audit obligations, and producing aggregate anonymized statistics. We do not sell your telemetry data (see Section 18.4).

13.3 **Feedback.** Any feedback, suggestions, or ideas you provide to us about the Daemon or network may be used by us without restriction or compensation to you.

13.4 **Trademarks.** "iogrid," the iogrid logo, "$GRID," and related marks are trademarks of *[COUNSEL: confirm trademark owner — Dynolabs entity or to-be-formed iogrid Foundation]*. You may not use these marks without our prior written consent, except for descriptive references (e.g., "I run iogrid on my home server").

---

## 14. Suspension and termination

14.1 **Termination by you.** You may terminate this Agreement at any time by uninstalling the Daemon and emailing *[COUNSEL: insert account-closure contact]* to request account closure.

14.2 **Termination by us — for cause.** We may suspend or terminate your account immediately, without prior notice, if:
- You violate this Agreement or the AUP;
- We have a reasonable belief that your Device has been compromised (malware, account takeover, etc.);
- We are required to do so by law or by a court order;
- You fail tax-reporting requirements after written notice and a 30-day cure period;
- You engage in conduct that materially disrupts the iogrid network or other Providers.

14.3 **Termination by us — without cause.** We may terminate this Agreement for any reason or no reason with **30 days' prior written notice** to the email address on your account. Termination without cause does not forfeit earned compensation.

14.4 **Effect of termination.**
- Cash earnings already credited are paid out per the regular payout schedule, except where withheld pursuant to a lawful order or where we have a good-faith belief earnings result from fraudulent or AUP-violating conduct.
- $GRID earnings: vested portions are released; unvested portions are forfeited only in cases of termination for cause under Section 14.2 first bullet (violation of Agreement / AUP). Termination without cause does not forfeit unvested $GRID.
- Audit Logs are retained for the 90-day window per Section 6.1.
- The provisions of Sections 5 (Indemnification), 11 (Data Collection), 15 (Disclaimer), 16 (Liability), 19 (Data Subject Rights), 20 (Governing Law / Arbitration), and 23 (Survival) survive termination.

*[COUNSEL: review forfeiture-of-unvested-$GRID-on-cause language for enforceability under each jurisdiction's wage-payment and consumer-protection laws. Some jurisdictions treat earned-but-vesting compensation differently from a true bonus.]*

---

## 15. Disclaimer of warranties

The Daemon and the iogrid network are provided **"AS IS" and "AS AVAILABLE,"** without warranties of any kind, whether express, implied, statutory, or otherwise. To the maximum extent permitted by applicable law, we disclaim all warranties, including but not limited to merchantability, fitness for a particular purpose, non-infringement, and any warranties arising from course of dealing or usage of trade.

We do not warrant that the Daemon will be uninterrupted, error-free, or secure; that defects will be corrected; or that the network will be free of viruses or harmful components. You assume the entire risk of using the Daemon on your Device.

*[COUNSEL: some jurisdictions (notably EU consumer law, UK Consumer Rights Act 2015, Australian Consumer Law) prohibit blanket disclaimers against consumers. Add carve-out language preserving non-excludable statutory rights, then verify carve-outs match each launch jurisdiction.]*

---

## 16. Limitation of liability

16.1 **No indirect damages.** To the maximum extent permitted by law, neither we nor any of our affiliates, officers, directors, employees, contractors, or agents shall be liable for any indirect, incidental, consequential, special, exemplary, or punitive damages — including loss of profits, loss of business, loss of data, loss of goodwill, business interruption, or any other intangible loss — arising out of or relating to this Agreement, the Daemon, or the iogrid network, even if we have been advised of the possibility of such damages.

16.2 **Direct damages cap.** Our aggregate liability to you for all direct damages arising out of or relating to this Agreement, the Daemon, or the iogrid network is limited to the greater of:
- **(a) Your most-recent monthly earnings (whether in cash, USD-equivalent value of free VPN, USD-equivalent value of charity donations made, or USD-equivalent value of $GRID at time of distribution) multiplied by twelve (12);** or
- **(b) One hundred US dollars ($100 USD).**

16.3 **Indemnity carve-out.** The liability cap in Section 16.2 does NOT apply to our defense and indemnification obligation in Section 5, which is governed by the practical limits described in that section and by the size of the legal defense fund.

16.4 **Non-excludable carve-outs.** Nothing in this Section 16 limits or excludes our liability for:
- Death or personal injury caused by our negligence;
- Fraud or fraudulent misrepresentation by us;
- Any other liability that cannot lawfully be limited or excluded under the law applicable to this Agreement.

*[COUNSEL: confirm cap formula. The "monthly earnings × 12" formula matches docs/LEGAL.md spec. Some jurisdictions may require a minimum floor (e.g., the price paid in the last 12 months for paid services), which is inapplicable here since the Provider pays nothing. Consider adding a statutory-minimum floor for jurisdictions where one is required.]*

---

## 17. Governing law; dispute resolution

17.1 **Governing law.** This Agreement is governed by and construed in accordance with the laws of *[COUNSEL: insert governing-law jurisdiction. Per docs/LEGAL.md, "Cayman Foundation jurisdiction recommended" for token-side; for Provider contracts, "governed by provider's home jurisdiction (GDPR / regional consumer-protection laws are mandatory regardless)." Most platforms select one home jurisdiction (e.g., the Cayman Islands or Delaware) with explicit preservation of mandatory consumer rights for EU/UK/AU Providers. Make the explicit choice here.]*, without giving effect to its conflict-of-laws principles.

17.2 **Provider-side mandatory rights preserved.** Notwithstanding Section 17.1, if you are a consumer ordinarily resident in the European Economic Area, the United Kingdom, Switzerland, Australia, or any other jurisdiction whose consumer-protection law applies regardless of contractual choice-of-law, you remain entitled to the protections of the mandatory provisions of your home jurisdiction's law that you would have absent this Agreement.

17.3 **Dispute resolution — arbitration.** Any dispute, claim, or controversy arising out of or relating to this Agreement, the Daemon, or the iogrid network shall be resolved by **binding arbitration administered by *[COUNSEL: select arbitral body — AAA Commercial Rules for US-rooted Providers; LCIA or ICC International for EU/UK; SIAC for APAC; the Cayman International Mediation and Arbitration Centre if Cayman Foundation is selected]***, conducted in English, in *[COUNSEL: select seat — many SaaS providers use Delaware, the Cayman Islands, or Singapore]*. The arbitral tribunal shall consist of one (1) arbitrator unless either party requests three (3), in which case three (3).

17.4 **Class-action waiver.** Each party agrees that disputes shall be resolved on an individual basis only and waives any right to participate in a class action, collective action, or consolidated arbitration.

17.5 **Small-claims exception.** Notwithstanding Sections 17.3 and 17.4, either party may bring a claim in a small-claims court of competent jurisdiction for amounts within the small-claims jurisdictional limit.

17.6 **Injunctive relief.** Either party may seek temporary or preliminary injunctive relief from a court of competent jurisdiction, in particular to protect intellectual property rights or to enforce confidentiality obligations.

*[COUNSEL: class-action waivers are not enforceable in all jurisdictions. The EU Collective Redress Directive 2020/1828, US state-level erosions (e.g., the California McGill rule), and the UK Competition Appeal Tribunal collective-action regime all merit specific review. Some jurisdictions also disallow mandatory arbitration for consumer contracts. Recommend two-tier clause: arbitration for B2B Providers (corporate entities), court-of-competent-jurisdiction for individual-consumer Providers with their home-jurisdiction protections preserved.]*

---

## 18. General provisions

18.1 **Entire agreement.** This Agreement, together with the AUP, the DPA (where applicable), the Privacy Policy, the Token Disclaimer (where applicable), and any documents expressly incorporated by reference, constitutes the entire agreement between you and us with respect to its subject matter, and supersedes all prior or contemporaneous communications.

18.2 **Severability.** If any provision of this Agreement is held to be invalid, illegal, or unenforceable, the remaining provisions remain in full force and effect. The invalid provision shall be replaced with a valid provision that most nearly approximates the parties' original intent.

18.3 **No waiver.** No failure or delay by us to exercise any right or remedy under this Agreement constitutes a waiver of that right or remedy.

18.4 **No sale of Provider data.** We do not sell, license, or otherwise transfer for value the telemetry data we collect from your Device. We share only the data necessary for the purposes described in the Privacy Policy.

18.5 **Assignment.** We may assign this Agreement to an affiliate, to a successor in connection with a merger, acquisition, or sale of all or substantially all of our assets, or as part of a corporate restructuring. You may not assign this Agreement without our prior written consent.

18.6 **No third-party beneficiaries.** This Agreement is for the benefit of you and us only. No third party has any rights under this Agreement, except as expressly provided.

18.7 **Force majeure.** Neither party is liable for any failure or delay in performance caused by circumstances beyond its reasonable control, including acts of God, war, terrorism, civil unrest, government action, internet or utility outages, pandemic, or labor disputes.

18.8 **Notices.** Notices to you may be given via the Daemon UI, by email to the address on your account, or by posting at *[COUNSEL: insert status page or notice URL]*. Notices to us must be in writing to *[COUNSEL: insert notice address]*.

18.9 **Language.** This Agreement is drafted in English. If we provide translations, the English version controls in any conflict, except where local law requires otherwise. *[COUNSEL: some jurisdictions (e.g., Quebec under the Charter of the French Language, France under the Loi Toubon for B2C) require local-language versions to control for consumer contracts.]*

---

## 19. Data subject rights

If you are a data subject under GDPR, UK GDPR, CCPA / CPRA, Brazilian LGPD, or any analogous regime, you have rights including the right to access, correct, port, and delete your personal data; the right to object to processing; and the right to lodge a complaint with a supervisory authority. See the [Privacy Policy](./privacy-policy.md) for the full list and the process to exercise these rights.

---

## 20. Updates to these terms

20.1 **Updates with notice.** We may update this Agreement from time to time. We will provide at least **30 days' prior notice** before any material change takes effect, via the Daemon UI, by email to the address on your account, and at *[COUNSEL: insert ToS-changes URL]*.

20.2 **Opt-out via uninstall.** If you do not agree to an update, your sole remedy is to uninstall the Daemon and close your account before the effective date of the update. Continued use of the Daemon after the effective date constitutes acceptance of the update.

20.3 **Immediate updates for legal or security reasons.** Where an update is required by law, by a court order, or to address an active security or abuse incident, we may make such update immediately without 30-day notice. We will provide notice as soon as reasonably practicable.

---

## 21. Modification of earnings formula

We may modify the earnings formula referenced in Section 8.2 prospectively (i.e., for Workloads not yet performed) with **30 days' prior notice** via the Daemon UI and email. Already-earned compensation is not affected by formula modifications.

---

## 22. Contact

For questions about this Agreement:
- **Legal / subpoena / law-enforcement contact:** *[COUNSEL: insert, e.g., legal@iogrid.org]*
- **Privacy / data subject rights:** *[COUNSEL: insert, e.g., privacy@iogrid.org]*
- **Provider support:** *[COUNSEL: insert, e.g., providers@iogrid.org]*
- **Postal address:** *[COUNSEL: insert finalized registered address]*

---

## 23. Survival

The following Sections survive termination or expiration of this Agreement: 1 (Definitions), 5 (Indemnification, as to events occurring before termination), 6 (Audit-cooperation, as to retained logs), 11 (Data Collection, as to retained data), 12 (Acceptable Use, as to past conduct), 13 (Intellectual Property), 15 (Disclaimer), 16 (Limitation of Liability), 17 (Governing Law / Dispute Resolution), 18 (General Provisions), 19 (Data Subject Rights), 22 (Contact), and this Section 23.

---

*End of Provider Terms of Service v0.1-draft.*

*[COUNSEL: full-document review required before publication. Key open items: governing-law selection (Section 17), liability-cap calculation including $GRID valuation (Section 16), independent-contractor framing per-jurisdiction (Section 8.6), defense-fund structural question (Section 5.6), arbitration enforceability per-jurisdiction (Section 17.3–17.4), translation-control language (Section 18.9). Total `[COUNSEL]` markers in this document: ~30.]*
