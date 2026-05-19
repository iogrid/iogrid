# iogrid Foundation — Foundation Rules (template)

**Status:** Draft v0.1 — pre-counsel-review template. Not legal advice. **Counsel must
draft the final Articles of Association and Foundation Rules before incorporation.** This
template captures iogrid's intended governance and operational policies; counsel will
restructure into the Cayman Foundation Companies Act 2017-compliant form.

**Related issues:**
- [#103](https://github.com/iogrid/iogrid/issues/103) — Foundation jurisdiction selection
- [#122](https://github.com/iogrid/iogrid/issues/122) — Cayman Foundation incorporation

---

## Preamble

These Foundation Rules ("**Rules**") supplement the Memorandum of Association and
Articles of Association of **iogrid Foundation** (the "**Foundation**"), a Cayman Islands
Foundation Company incorporated under the Foundation Companies Act 2017 (as amended).

In the event of conflict between these Rules and the Articles, the Articles control.
In the event of conflict between these Rules and the Cayman Foundation Companies Act 2017,
the Act controls.

These Rules are adopted by the Foundation's Directors and may be amended only:
1. By unanimous resolution of the Foundation's Directors; AND
2. With the affirmative consent of a majority of the Foundation's Supervisors; AND
3. Where the amendment relates to token economics (Sections 4, 5, 6 below), with prior
   notification to the iogrid community via the Foundation's published channels at least
   30 days before the effective date.

---

## 1. Foundation purpose

Pursuant to the Memorandum, the Foundation's purposes are:

1. To promote and govern the development of the iogrid decentralized work-marketplace
   protocol (the "**Protocol**") and the `$GRID` utility token (the "**Token**").
2. To issue, manage, and oversee the Token in accordance with the Tokenomics document
   published at https://github.com/iogrid/iogrid/blob/main/docs/TOKENOMICS.md (the
   "**Tokenomics**"), as amended from time to time by the Foundation's governance.
3. To hold, manage, and disburse the Foundation's treasury for purposes consistent with
   (1) and (2).
4. To engage with regulators, auditors, and counsel as required for the lawful operation
   of the Protocol.
5. To execute the Phase-3 DAO migration described in the Tokenomics, on a timeline and
   under conditions approved by the Foundation's Supervisors.

---

## 2. Governance

### 2.1 Directors

- The Foundation shall have not fewer than one (1) Director and not more than three (3)
  Directors at any time.
- Initial Directors:
  - **Director 1**: [Founder name], also Chief Executive Officer of Dynolabs Inc.
  - **Director 2**: [Independent director name], appointed for crypto-foundation
    experience.
- Directors are appointed for an initial term of two (2) years, renewable by Foundation
  resolution.
- A Director may be removed by:
  - Voluntary resignation (with 30 days' written notice); OR
  - Resolution of the remaining Directors with the affirmative consent of a majority of
    Supervisors; OR
  - Action by the Supervisors pursuant to Section 2.3 (Supervisor enforcement).

### 2.2 Supervisors

- The Foundation shall have not fewer than two (2) Supervisors at any time.
- Initial Supervisors:
  - **Supervisor 1**: [Independent supervisor name], with crypto-foundation governance
    experience.
  - **Supervisor 2**: [Second independent supervisor name].
- Supervisors are responsible for ensuring the Directors act in accordance with the
  Memorandum, Articles, these Rules, and applicable law.
- Supervisors are appointed for an initial term of three (3) years, renewable.
- Supervisors may not be Directors of the Foundation. Supervisors may not be employees of
  Dynolabs Inc. or the Foundation. Supervisors are compensated with an annual retainer
  set in Section 7.

### 2.3 Supervisor enforcement

If a Supervisor reasonably believes the Directors are acting in breach of the Foundation's
purposes, the Memorandum, the Articles, or applicable law, the Supervisor may:

1. Issue a written notice to the Directors specifying the alleged breach and requesting
   remediation within 30 days.
2. If unsatisfied with the response, convene an extraordinary general meeting of the
   Foundation's members (i.e., the Directors and other Supervisors) to vote on
   remediation, including potential removal of a Director.
3. Apply to the Grand Court of the Cayman Islands for enforcement orders pursuant to the
   Foundation Companies Act 2017 §10–13.

### 2.4 Squads multisig (token-treasury governance)

The Foundation maintains the iogrid Token treasury (the 10% treasury allocation per
Tokenomics §"Token allocation") via a Squads Protocol multisig on Solana mainnet,
configured 3-of-5. Initial signers:

1. Director 1 (Founder)
2. Director 2 (Independent director)
3. Lead engineer of the Protocol (employee of Dynolabs Inc., contractually bound to the
   Foundation)
4. Community-elected signer (elected per Section 2.5)
5. Legal counsel (Cayman registered agent's designated key-holder)

Any token outflow from the Foundation treasury requires three (3) signatures from the
above five (5) signers, executed via the Squads multisig.

Signer rotation procedure:
- Voluntary signer departure: 14 days' written notice to other signers. Replacement
  signer added by Director resolution with Supervisor consent.
- Compromised key: signer is removed immediately by emergency resolution; backup
  replacement signer designated at the time of multisig setup.

### 2.5 Community-elected signer

One of the five (5) Squads multisig signers is community-elected:

- **Eligibility**: any Token holder with a wallet that has held ≥ 100,000 $GRID (gross of
  vesting) for at least 90 days at the time of nomination.
- **Election**: held annually via Snapshot.org or equivalent off-chain governance tool.
  Each $GRID held = 1 vote weight. Election is conducted by simple plurality.
- **Term**: 12 months, renewable by re-election.
- **Removal**: by recall vote of the same electorate, or by Director resolution with
  Supervisor consent if the signer breaches duty.

### 2.6 DAO migration

Per Tokenomics §"Token launch sequence", the Foundation is intended to migrate to DAO
governance in Phase 3. Trigger conditions for migration:

1. At least 24 months of operational track record post-TGE; AND
2. At least 30% of Token circulating supply held by wallets other than the Foundation,
   Dynolabs Inc., team, and strategic investors; AND
3. Supervisor vote to authorise migration (unanimous).

Migration mechanics:
- Squads multisig signers transition from Foundation-appointed signers to
  community-elected signers (5-of-9 or similar expanded configuration, TBD by Supervisor
  resolution).
- Protocol parameter changes (burn rate above 2% floor, emission curve adjustments
  within the halving schedule, etc.) become DAO-votable.
- Foundation continues to exist for regulatory compliance and operational purposes; the
  DAO assumes economic governance.

---

## 3. Treasury management

### 3.1 Treasury assets

The Foundation's treasury consists of:
1. The 10% Token allocation (100M $GRID at TGE) held in the Squads multisig.
2. Strategic raise proceeds (if any) — USDC + USD in operating bank accounts.
3. Revenue share from Protocol operations (~2% buyback-and-burn carve goes to burn, not
   treasury; gross operating revenue flows through Dynolabs Inc. service contract).
4. Bond / reserve allocations from time to time.

### 3.2 Investment policy

The Foundation's treasury is managed conservatively:
- Minimum 12 months of operating expenses held in stablecoins (USDC) or fiat (USD).
- Up to 50% of remaining treasury may be deployed in: protocol-controlled liquidity
  (Raydium CLMM), conservative DeFi (Marinade liquid-staked SOL only), or held in $GRID.
- No leverage, no margin, no derivatives, no exotic protocols.

### 3.3 Disbursement categories

The Foundation may disburse treasury funds for:
1. **Legal + audit**: ongoing counsel retainers, audit firm engagements, regulatory
   filings.
2. **Engineering**: contractor payments to Dynolabs Inc. under the service agreement,
   bounty payouts via Immunefi.
3. **Community grants**: protocol-improvement grants, ecosystem development grants.
4. **Liquidity provision**: seed and maintain Raydium CLMM pools.
5. **Operational overhead**: registered-agent fees, banking fees, director / supervisor
   compensation per Section 7.
6. **Strategic reserves**: war-chest for adverse regulatory developments or material
   smart-contract incidents.

### 3.4 Treasury reporting

The Foundation publishes a quarterly treasury report at https://iogrid.org/foundation/reports,
including:
- Squads multisig address + verified balance
- USD-denominated stablecoin balances
- Disbursements by category for the quarter
- Major counterparty engagements (counsel, audit, custody)

The report is signed by the Foundation's Director 1 (Founder) and reviewed by Supervisor 1
before publication.

---

## 4. Token economics — operational parameters

The following parameters codify the Tokenomics document into Foundation rules. Changes
require amendment per the preamble.

### 4.1 Hard cap

The Token's hard cap is 1,000,000,000 (1B) $GRID. This is enforced on-chain in the
`grid-token` Anchor program (`GRID_HARD_CAP` constant). The Foundation does not, and
cannot, mint Tokens past this cap.

### 4.2 Emission curve

Tokens are emitted to the provider rewards pool per the halving curve in
Tokenomics §"Layer 2 — Emission halving":
- Year 0–2: 50M / year
- Year 2–4: 25M / year
- Year 4–6: 12.5M / year
- (continuing geometric halving)
- Year 10+: 0

This is enforced on-chain in the `emission` Anchor program (`budget_for_window` formula).
The Foundation does not, and cannot, override this curve.

### 4.3 Buyback-and-burn rate

The Foundation directs `billing-svc` to execute a daily buyback-and-burn equal to
**2%** of net Protocol revenue. The Foundation may, by Director resolution with
Supervisor consent, increase this rate (e.g., to 3% or 5%). The Foundation may NOT
decrease this rate below 2% without amendment to these Rules.

### 4.4 Provider lockup tiers

The Foundation maintains four provider lockup tiers per Tokenomics §"Optional bonus
lockup tiers":

| Tier | Cliff | Vest | Multiplier |
|------|-------|------|------------|
| Standard | 30 days | 60 days | 1.00× |
| Loyalty | 90 days | 180 days | 1.25× |
| Conviction | 180 days | 365 days | 1.50× |
| Maximum | 365 days | 730 days | 2.00× |

These are enforced on-chain in the `vesting` Anchor program. Tier values may be revised
by Foundation resolution with 90 days' advance notice to the community, but cannot be
reduced below the values shown for Token holders who joined under the prior values.

### 4.5 Early-unlock penalty

The early-unlock penalty is **50%** of the locked amount, burned. This is enforced on-
chain in the `vesting` Anchor program (`EARLY_UNLOCK_PENALTY_BPS = 5_000`). It is a
compile-time constant and not adjustable without re-deploying the program (and re-audit).

### 4.6 Customer payment discount

Customers paying in $GRID receive a **20% discount** on list price per Tokenomics
§"Customer payment options". This may be adjusted by Director resolution.

### 4.7 Customer staking discount cap

Customer-side stakers may receive up to **25% off** list price per Tokenomics
§"Customer-side staking". Enforced on-chain in `staking` (`MAX_DISCOUNT_BPS = 2_500`).

---

## 5. Risk management

### 5.1 Sanctions compliance

The Foundation:
- Geo-blocks token-purchase flows from US IPs and OFAC-sanctioned jurisdictions.
- Maintains the Token-2022 freeze authority for sanctions enforcement, exercised only
  upon written legal advice or compelled court order.
- Conducts monthly sanctions screening of identified Token holders.

### 5.2 Smart-contract incident response

If a Critical-severity smart-contract bug is identified post-launch:

1. **Immediate**: Squads multisig executes the upgrade path with a pre-prepared no-op
   stub binary to pause the affected program, per the operator runbook.
2. **Within 4 hours**: Foundation publishes an incident notice via official channels.
3. **Within 48 hours**: external auditor engaged to verify the fix.
4. **Within 7 days**: fix deployed, audit report published, normal operations resumed.

### 5.3 Liability and indemnification

Directors, Supervisors, and key personnel are indemnified by the Foundation for
liabilities arising from good-faith actions within the scope of their duties. Standard
exclusions apply (fraud, willful breach, gross negligence). The Foundation maintains a
D&O insurance policy sized to its operational scale.

---

## 6. Relationship with Dynolabs Inc. (operating entity)

### 6.1 Tech-license agreement

The Foundation licenses the iogrid technology stack from Dynolabs Inc. (Delaware C-corp)
under a tech-license agreement. Key terms:
- Royalty: $1 per year (nominal).
- Term: perpetual, terminable by either party with 12 months' notice.
- Scope: all iogrid IP including the Protocol, daemon, coordinator, web plane, and
  Anchor contracts.

### 6.2 Service agreement

The Foundation contracts with Dynolabs Inc. to operate the Protocol day-to-day. Key
terms:
- Compensation: cost-plus 15% (cost = audited engineering + ops + infrastructure spend).
- Term: 12 months, renewable.
- Termination: 90 days' notice from either party.

The service agreement is reviewed annually by the Foundation's Supervisors.

### 6.3 Conflict-of-interest policy

Director 1 (Founder) is also the CEO of Dynolabs Inc. To manage conflict-of-interest:
- Director 1 abstains from votes on the service agreement renewal and on royalty
  amendments.
- Supervisor approval is required for any Dynolabs Inc.-affiliated counterparty
  engagement exceeding $50K/quarter.
- Director 1 makes annual conflict-of-interest disclosures, published in the treasury
  report.

---

## 7. Compensation

| Role | Annual compensation (USD) | Notes |
|------|---------------------------|-------|
| Director 1 (Founder) | $0 | Compensated via Dynolabs Inc. equity / salary |
| Director 2 (Independent) | $15–25K | Plus per-meeting fees if applicable |
| Supervisor 1 | $10–20K | Plus per-meeting fees |
| Supervisor 2 | $10–20K | Plus per-meeting fees |
| Lead engineer (multisig signer) | $0 | Compensated via Dynolabs Inc. salary |
| Community-elected signer | $0 | Volunteer role; small honorarium ($5K/year) optional by Director resolution |
| Legal counsel (multisig signer) | $0 | Compensated via counsel retainer |

Compensation amounts may be revised annually by Director resolution with Supervisor
consent.

---

## 8. Amendment

These Rules may be amended per the procedure in the preamble. The amendment becomes
effective upon:
1. Unanimous Director resolution; AND
2. Majority Supervisor consent; AND
3. (For token-economics-related amendments) 30 days' community notification via the
   Foundation's published channels.

Amendments are published at https://iogrid.org/foundation/rules with a redline against
the prior version.

---

## 9. Effective date

These Rules become effective upon adoption by unanimous resolution of the initial
Directors and majority consent of the initial Supervisors, following incorporation of
the Foundation.

---

## 10. Signatures (template — for counsel to format)

```text
ADOPTED by the Foundation's Directors:

____________________________
Director 1: [Founder name]
Date: ___________

____________________________
Director 2: [Independent director name]
Date: ___________

CONSENTED by the Foundation's Supervisors:

____________________________
Supervisor 1: [Supervisor 1 name]
Date: ___________

____________________________
Supervisor 2: [Supervisor 2 name]
Date: ___________
```

---

*End of iogrid Foundation Foundation Rules template v0.1-draft.*

*[COUNSEL: full review and rewriting required. This template captures iogrid's intended
governance and policy; the final Articles of Association and Foundation Rules will be
drafted by Cayman counsel in compliance with the Foundation Companies Act 2017 and
relevant CIMA / TIA requirements. Particular attention required on: Supervisor powers
(Section 2.3) vs the Act's prescribed Supervisor enforcement mechanisms; tech-license
$1-royalty (Section 6.1) and its Delaware / Cayman tax treatment; freeze-authority
policy (Section 5.1) and disclosure obligations; DAO migration trigger conditions
(Section 2.6) and securities-law implications of governance handoff. Counsel cost
estimate: included within the $30–80K incorporation budget per `cayman-setup.md`.]*
