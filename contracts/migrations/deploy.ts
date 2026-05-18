// Anchor deploy hook — invoked by `anchor migrate` after a successful `anchor deploy`.
//
// For the v0 scaffold this script is intentionally a no-op stub. The real bootstrap sequence
// (run once, manually, by the deployer holding the multisig keys) will:
//
//   1. `anchor deploy --provider.cluster devnet` (or mainnet-beta) to publish all 5 programs.
//   2. Create the $GRID mint as a Keypair-owned Token-2022 account (clients/`mint-bootstrap.ts`).
//   3. Call `grid_token::initialize_config` + `initialize_mint`.
//   4. Mint the 5 fixed allocations per docs/TOKENOMICS.md "Token allocation":
//        - 50%  provider rewards pool   → emission program vault PDA (locked behind halving)
//        - 15%  team                    → Streamflow vesting (4y vest, 1y cliff)
//        - 10%  treasury                → Squads 3-of-5 multisig
//        - 10%  strategic investors     → Streamflow vesting (12mo cliff + 24mo linear)
//        - 10%  community / ecosystem   → governance-controlled grant wallet
//        -  5%  initial DEX liquidity   → Raydium CLMM seed wallet
//   5. Call `grid_token::transfer_mint_authority` → emission program's PDA.
//   6. Call `grid_token::lock_authorities` to disable further admin minting.
//   7. Call `burn::initialize_registry` pointing at the billing-svc attestor key.
//   8. Call `staking::initialize_pool` with annual_yield_bps from governance config.
//   9. Seed Raydium CLMM with 5M $GRID + $250K USDC in [$0.05, $5.00] concentrated range
//      (see docs/TOKENOMICS.md "Concentrated liquidity strategy").
//
// All of step 4–9 require multisig signatures and the Cayman Foundation legal sign-off
// (open issue: "Foundation incorporation: Cayman Foundation for $GRID treasury"). They will
// be performed as part of the TGE event, NOT in CI, NOT via this migration script.

module.exports = async function (_provider: any) {
  // Intentionally empty. See header comment above for the post-deploy runbook.
};
