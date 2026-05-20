# $GRID Token — Risk Factors and Legal Disclaimers

**Status:** Draft v0.1 — pre-counsel-review. Not legal advice. See [`legal/README.md`](./README.md).
**Effective date:** *[COUNSEL: insert effective date upon finalization]*
**Operating entity:** *[COUNSEL: confirm — likely a non-profit Foundation jurisdiction (Cayman / BVI / Liechtenstein) per `docs/TOKENOMICS.md` §"Legal risk + mitigation strategy."]*

> **THIS DOCUMENT IS NOT AN OFFER OR SOLICITATION TO SELL SECURITIES. THIS DOCUMENT IS NOT INVESTMENT ADVICE. $GRID IS A UTILITY TOKEN OF THE iogrid NETWORK, NOT A FINANCIAL INSTRUMENT. THE STATEMENTS IN THIS DOCUMENT ARE NOT FORWARD-LOOKING REPRESENTATIONS ABOUT TOKEN PRICE OR PROJECT OUTCOMES.**
>
> *[COUNSEL: this banner language is foundational. Crypto-securities counsel must confirm wording and placement before any publication.]*

---

## 1. Plain-language summary *(non-binding)*

$GRID is the native token of the iogrid network. Providers can choose to be paid in $GRID instead of cash. Customers can pay in $GRID for a discount. We designed mechanics — emission halving, burn-from-revenue, mandatory provider-earnings lockup — to align token economics with network growth. **None of this is a promise that $GRID will go up in price. $GRID can lose all of its value. If you live in the United States or another restricted jurisdiction, you may not be able to acquire $GRID at launch and you may not be able to convert $GRID to cash. We are not your financial advisor. Read this entire document before acquiring or electing to earn $GRID.**

---

## 2. What $GRID is

$GRID is the iogrid network's native unit of work, as described in [`docs/BUSINESS-STRATEGY.md` §4 Currency model](../docs/BUSINESS-STRATEGY.md#4-currency-model--grid--fiat-hybrid). Its design goals include:

- **Unit of work:** provider compensation and customer billing denominated natively in $GRID (with fiat on-ramp / off-ramp via Stripe and MoonPay).
- **Aligned incentives:** providers earn into a vesting position that benefits from network growth.
- **Deflationary mechanic:** revenue-driven buyback-and-burn, capped emission curve, mandatory lockup of earned tokens.
- **Programmatic governance:** treasury managed by a multisig (Squads Protocol), with governance migration to a DAO planned at Phase 3+.

$GRID is implemented as an SPL token (Solana, Token-2022 with extensions) and is intended to be bridged to Base (Ethereum L2) via Wormhole NTT post-TGE. Vesting and lockup are implemented via the Streamflow program (audited).

## 3. What $GRID is NOT

- **$GRID is not equity.** Holding $GRID does not give you ownership of iogrid, Dynolabs, the iogrid Foundation, or any related entity. You have no voting rights in corporate governance (token holders may have governance rights over certain network parameters once DAO migration occurs, but this is governance over the protocol, not over the corporate entity).
- **$GRID is not a debt instrument.** No party is obligated to redeem $GRID for cash, to maintain a price floor, or to repay you any amount.
- **$GRID is not a deposit.** $GRID is not held by a bank or trust company. There is no FDIC, FSCS, or other deposit-insurance protection.
- **$GRID is not a regulated financial product** in the jurisdictions in which we currently intend to make it available. *[COUNSEL: this assertion must be reviewed in every launch jurisdiction. The classification of utility tokens varies — and the same token may be classified differently by different regulators.]*
- **$GRID is not, in our intent, a security.** We have structured $GRID to function as a utility token of the iogrid network. See Section 6. **However, no court or regulator has ruled on $GRID's classification, and a court could disagree with our framing.**

---

## 4. Risk factors

By acquiring or electing to earn $GRID, you acknowledge that you have read, understood, and accepted the following risks. This is not an exhaustive list.

### 4.1 Price volatility

Token prices, including $GRID, can be highly volatile and may decline rapidly, including to zero. There is no price floor. There is no commitment from any party to buy back or support the price. Volatility can be driven by general crypto-market conditions, regulatory developments, project-specific news, sub-processor disruption, smart-contract exploits, exchange listings or delistings, large holders' sell decisions, or many other factors. **You can lose all of the value of $GRID you hold or earn.**

### 4.2 No investment promise

We have not made, and you should not infer, any promise that $GRID will appreciate in value, that the deflationary mechanism will offset emission, that the lockup will produce upside, that DEX liquidity will be sufficient at any future moment, or that any other outcome will obtain. The descriptions in `docs/TOKENOMICS.md` are mechanism descriptions, not promises. The buyback-and-burn rate, the emission curve, and all token-economic parameters may be modified prospectively by the Foundation or by DAO vote (post-handoff).

### 4.3 Geographic restrictions

At Token Generation Event (TGE) and during an initial restriction period:
- **US persons** (as defined under Regulation S, including US residents, US-incorporated entities, and certain US-connected persons) may not acquire $GRID. We will geo-block US IPs from the token-purchase flow and from $GRID-payout election.
- **Other restricted jurisdictions** may include (subject to confirmation): jurisdictions subject to comprehensive sanctions (Section 2.5 of Provider ToS), and any jurisdiction whose regulator has issued specific guidance restricting public token offerings. *[COUNSEL: enumerate the launch-time geo-block list.]*
- Restrictions may be lifted in specific jurisdictions if subsequent regulatory clarity permits. **No specific timeline is committed.**

If you acquire $GRID in violation of these restrictions, you may face civil or criminal penalties in your jurisdiction, and we may be required to forfeit, freeze, or refuse to honor your $GRID holdings.

### 4.4 Regulatory risk and securities classification

We have designed $GRID to function as a utility token. **However**, regulators in various jurisdictions may classify $GRID differently than we do. In particular:

- **United States.** The Securities and Exchange Commission's (SEC) approach to crypto-token classification has, in 2024–2025, encompassed enforcement actions against Coinbase, Binance, Kraken, and others under the *Howey* investment-contract framework. The SEC could classify $GRID as a security, which could result in:
  - Forced delisting of $GRID from US-accessible venues;
  - A rescission offer to any US holders;
  - Civil monetary penalties against iogrid and its officers;
  - Injunctive relief restricting further $GRID activity in the US.
- **European Union.** The Markets in Crypto-Assets Regulation (MiCA), in force from December 2024, classifies tokens as "utility tokens," "asset-referenced tokens," or "e-money tokens," each with distinct disclosure, authorization, and supervisory requirements. $GRID's intended classification is "utility token" with a corresponding white-paper-notification obligation. **A national competent authority (NCA) may disagree** and require treatment under a different category.
- **United Kingdom.** The Financial Conduct Authority (FCA) regulates "qualifying cryptoassets" under the Financial Services and Markets Act 2000 (as amended). The financial-promotions regime applies to invitations and inducements relating to investments. $GRID promotions in the UK must comply or be exempt.
- **Other jurisdictions** have their own regimes (Singapore Payment Services Act; Hong Kong VASP regime; Liechtenstein TVTG; Switzerland FINMA categorization; Japan FSA; etc.) — all subject to ongoing change.

**iogrid does not control regulatory outcomes.** Adverse regulatory action could materially harm the value, liquidity, and utility of $GRID.

### 4.5 Foundation structure and governance

$GRID is intended to be issued by a **non-profit Foundation** to be incorporated in a jurisdiction selected by counsel (Cayman, BVI, or Liechtenstein candidates per `docs/TOKENOMICS.md`). The Foundation is intended to be separate from iogrid's commercial operating entity (Dynolabs Inc. / iogrid Inc.). Foundation governance is by a multisig (Squads Protocol, 3-of-5 signers) until DAO handoff. **You have no rights against the operating entity for Foundation actions, and vice-versa.** The Foundation's failure to obtain licenses, audit completions, banking relationships, or other operational prerequisites could delay or prevent token launch.

### 4.6 Smart-contract risk

$GRID-related smart contracts (token mint, vesting, staking, burn, DEX pool) are software. Software has bugs. Even audited software (we plan an audit by OtterSec or Halborn) is not bug-free. A smart-contract exploit could result in:
- Theft of treasury tokens;
- Theft of provider vesting positions;
- Manipulation of the burn registry;
- Drain of DEX liquidity;
- Inability to claim earned $GRID.

**We have no obligation to compensate you for smart-contract exploit losses**, although we will pursue mitigations (bug bounty program, multisig pause-functionality, post-incident user-funds-recovery efforts where technically possible).

### 4.7 Solana network risk

$GRID is deployed on the Solana network. Solana has experienced multi-hour outages (most recently in 2022–2023). During an outage, $GRID transactions, payouts, swaps, and burns may be delayed or impossible. We do not control Solana. Bridge risk to Base (via Wormhole NTT) introduces additional dependency.

### 4.8 Liquidity risk

$GRID's primary trading venue is the Raydium CLMM pool to be seeded at TGE. Liquidity may be insufficient for large transactions. Large provider claim or sell events may move price significantly. Liquidity may be removed by liquidity providers other than iogrid (if any exist). Centralized-exchange listings are not committed.

### 4.9 Lockup risk

If you elect $GRID payouts as a provider, your earned $GRID is subject to mandatory lockup (rolling 30/90-day per `docs/TOKENOMICS.md` §"Mandatory provider-earnings lockup"). During the lockup period:
- You cannot sell, transfer, or withdraw the locked $GRID;
- The price of $GRID may decline materially before your tokens vest;
- The early-unlock penalty (50% burn) makes liquidating before vesting expensive and reduces your effective compensation.

**The lockup is irrevocable. You cannot "undo" $GRID-payout election retroactively.**

### 4.10 Tax risk

In most jurisdictions, $GRID received as compensation is taxable as ordinary income at fair market value on receipt; subsequent disposal is taxable as a capital gain or loss. **The valuation may be uncertain at lockup events, and the IRS / HMRC / equivalent authority may disagree with your fair-value methodology.** Tax compliance is your responsibility, not iogrid's. Volatile crypto markets can result in a tax liability denominated in USD that exceeds the realized cash from the underlying token at the time the tax is due. **Plan accordingly. Consult a qualified tax advisor.**

### 4.11 Operational risk

iogrid's coordinator infrastructure (which routes Workloads, calculates earnings, executes daily Jupiter swaps, manages payouts) is operated by iogrid personnel. Operational failures — including but not limited to hot-wallet compromise, scheduler bugs, billing-svc miscalculations, sub-processor disruptions — can delay or prevent payouts. We maintain a 2-of-3 multisig for the hot wallet to mitigate single-key compromise, but no security measure is perfect.

### 4.12 Concentration risk

Token allocation (per `docs/TOKENOMICS.md`):
- 50% provider rewards (vesting linear over 10 years);
- 15% team (4-year vest, 1-year cliff);
- 10% treasury;
- 10% strategic investors (12-month cliff + 24-month vest);
- 10% community / ecosystem;
- 5% initial liquidity.

Team, strategic-investor, and treasury allocations together represent up to 35% of supply. **Coordinated selling by these holders, in the unlocked windows, could meaningfully depress $GRID price.** Vesting schedules are designed to mitigate but do not eliminate this risk.

### 4.13 Project-execution risk

iogrid is an early-stage project. We may fail to achieve the milestones described in the roadmap. Founders or key engineers may leave. Competitors (Bright Data, Honeygain, Pawns.app, Salad, others) may capture market share. Our cost structure may turn out to be unsustainable. The network may not reach the scale required for token economics to function as designed. **Project failure would likely render $GRID worthless.**

### 4.14 Forward-looking statements

`docs/TOKENOMICS.md`, the iogrid website, marketing materials, and any related communications contain forward-looking statements — descriptions of intended mechanisms, planned launches, expected outcomes. **Forward-looking statements are not promises.** Actual outcomes may differ materially. We undertake no obligation to update forward-looking statements except where required by law.

### 4.15 Lockup-token characterization risk

We have designed lockup periods to reduce immediate sell pressure. **A regulator might interpret lockup design as evidence of investment-contract framing** (since lockup is a feature commonly associated with security offerings — restricted-stock vesting being the most direct analogy). The lockup is a tokenomics design choice intended to align incentives; it is not, in our intent, characteristic of a securities offering.

### 4.16 Customer-payment-discount characterization risk

The 20% discount for Customers paying in $GRID is a volume / loyalty discount tied to the use of the network's native unit. It is not, in our intent, an inducement to invest. A regulator or court might disagree.

---

## 5. Specific representations and acknowledgments

By acquiring $GRID or electing $GRID payouts, you specifically represent, warrant, and acknowledge:

5.1 You are not a US person (as defined by Regulation S) at the time of acquisition, and you are not acquiring $GRID for the benefit of any US person. **If you are a US person**, you may not acquire $GRID; if you nonetheless acquire $GRID, you do so in violation of these terms and we may exercise the remedies in Section 7.

5.2 You are not a resident of, ordinarily located in, or acquiring $GRID on behalf of any person in, any jurisdiction whose laws prohibit your acquisition (sanctioned countries; jurisdictions with active regulatory orders against $GRID). The current restricted list is *[COUNSEL: enumerate]*.

5.3 You have read and understood this entire document, including the risk factors.

5.4 You have made an independent evaluation, with or without your own advisors (legal, tax, financial), and you accept all risks of acquiring or earning $GRID.

5.5 You will independently determine your tax position, file required returns, and pay required taxes.

5.6 You will safeguard your wallet credentials. iogrid is not responsible for losses arising from your loss, theft, or unauthorized use of your wallet credentials. iogrid does not custody your $GRID after distribution.

5.7 You have not relied on any oral statement, marketing material, social-media post, or other communication beyond this document and `docs/TOKENOMICS.md` in making your decision.

---

## 6. No solicitation; jurisdiction

This document does not constitute an offer to sell, or a solicitation of an offer to buy, $GRID in any jurisdiction in which such offer or solicitation would be unlawful. The publication of this document and the availability of the iogrid Service in any jurisdiction does not, by itself, constitute a representation that $GRID is or may lawfully be acquired in that jurisdiction.

Any offering of $GRID to accredited / professional investors prior to TGE will be made through separate documentation under applicable private-placement exemptions (e.g., Regulation D / Regulation S in the US, equivalent regimes elsewhere). *[COUNSEL: confirm strategic-raise documentation will be separate and properly papered.]*

---

## 7. Remedies for unauthorized acquisition

If iogrid determines that a holder acquired $GRID in violation of geographic, sanctions, or other restrictions:
- We may **refuse to distribute** $GRID to the holder's wallet (for provider payouts);
- We may **freeze or burn** $GRID held in iogrid-controlled vesting or staking contracts attributable to the violator (only where technically feasible — once distributed to a self-custody wallet, on-chain seizure is generally not possible);
- We may **refer the violator to law enforcement** for sanctions or other regulatory violations;
- We may **terminate the violator's account** without refund.

*[COUNSEL: confirm the technical and legal feasibility of these remedies — particularly token freezing under Token-2022 freeze authority, which iogrid will retain at launch for sanctions compliance. The retention of freeze authority itself may be commented on by regulators; confirm the balance of compliance utility vs. perceived centralization.]*

---

## 8. Updates to this disclaimer

We may update this Token Disclaimer from time to time. Material updates will be notified via the iogrid website, the Daemon UI (for providers earning $GRID), and the Customer management plane (for Customers paying in $GRID), at least 30 days before the effective date. **Continued earning, holding, or use of $GRID after the effective date constitutes acceptance.**

---

## 9. Conflict with other documents

In the event of conflict between this Token Disclaimer and any other document (Provider ToS, Customer ToS, Privacy Policy, DPA, marketing material), **this Token Disclaimer controls** as to token-specific matters and risk-factor representations.

In the event of conflict between this Token Disclaimer and **mandatory provisions of applicable law**, the law controls.

---

## 10. Contact

- **Token-specific inquiries:** *[COUNSEL: insert, e.g., token@iogrid.org or foundation@iogrid.org]*
- **Compliance / sanctions inquiries:** *[COUNSEL: insert, e.g., compliance@iogrid.org]*
- **Legal:** *[COUNSEL: insert]*

---

## 11. Required regulator-style disclosures (placeholders)

*[COUNSEL: depending on launch jurisdiction(s), specific regulator-mandated disclosures may be required, such as:*
- *MiCA Title II white paper — content prescribed by MiCA Art. 6 + Annex I;*
- *FCA financial-promotions risk warnings — UK Conduct of Business Sourcebook (COBS) 4.12A;*
- *Singapore MAS recognized-product placeholder warnings;*
- *Liechtenstein TVTG disclosure language if registered there.*

*These are to be inserted by counsel once jurisdictional posture is finalized.]*

---

*End of $GRID Token Risk Factors and Legal Disclaimers v0.1-draft.*

*[COUNSEL: full-document review required by qualified crypto-securities counsel. This is the single highest-legal-risk document in the iogrid set. Key open items: securities-classification analysis per jurisdiction (Section 4.4), Foundation jurisdiction selection (Section 4.5), restricted-jurisdiction list (Sections 4.3, 5.2), MiCA white-paper compliance (Section 11), Token-2022 freeze-authority policy and disclosure (Section 7), forward-looking-statements safe-harbor language (Section 4.14). Total `[COUNSEL]` markers: ~10. Expected counsel cost: $25K–75K per `docs/TOKENOMICS.md`.]*
