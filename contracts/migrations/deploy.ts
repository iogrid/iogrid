// Anchor deploy hook — invoked by `anchor migrate` after a successful `anchor deploy`.
//
// Bootstraps the $GRID economy on devnet (or localnet) with placeholder metadata. Mainnet TGE
// follows a separate, multisig-gated runbook (see header of original scaffold in PR #123 and
// `contracts/README.md`). This script is safe to re-run — every PDA init is `init` (not
// `init_if_needed`) for the one-shot ops, and idempotent for the metadata write.
//
// Steps (devnet path):
//   1. Print program IDs for verification against Anchor.toml.
//   2. Create the $GRID Token-2022 mint (decimals=9), `admin` = the deployer keypair.
//   3. Call `grid_token::initialize_config` (records hard-cap + minted-counter PDA).
//   4. Call `grid_token::set_metadata("iogrid", "GRID", "<placeholder-uri>")`.
//   5. Call `burn::initialize_registry` with `attestor = admin` (rotate later via
//      `rotate_attestor` to the billing-svc hot key).
//   6. Call `emission::initialize` with `tge_unix = now` and `billing_signer = admin` (rotate
//      later when billing-svc HSM key is provisioned).
//   7. Call `staking::initialize_pool` with `annual_yield_bps = 500` (5%).
//
// NOT done by this script (deliberate — multisig + audit-gated):
//   - Token allocation mints (50% provider pool, 15% team, etc.) — requires Cayman Foundation
//     incorporation + Squads multisig (issues #103, #96).
//   - `transfer_mint_authority` to emission PDA — requires audit sign-off (issue #97).
//   - `lock_authorities` — one-way switch, only after all of the above complete.
//   - Raydium CLMM pool seed — requires DEX-bound USDC (issue #94).

import * as anchor from "@coral-xyz/anchor";
import { PublicKey, SystemProgram, Keypair } from "@solana/web3.js";
import {
  createMint,
  TOKEN_2022_PROGRAM_ID,
  getAssociatedTokenAddressSync,
  ASSOCIATED_TOKEN_PROGRAM_ID,
} from "@solana/spl-token";

module.exports = async function (providerLike: any) {
  // Anchor passes either a Provider or a Provider-like object depending on CLI version.
  const provider =
    providerLike && providerLike.connection
      ? providerLike
      : anchor.AnchorProvider.env();
  anchor.setProvider(provider);

  const conn = provider.connection;
  const admin = (provider.wallet as anchor.Wallet).payer;

  const gridToken = anchor.workspace.GridToken as anchor.Program<any>;
  const emission = anchor.workspace.Emission as anchor.Program<any>;
  const vesting = anchor.workspace.Vesting as anchor.Program<any>;
  const staking = anchor.workspace.Staking as anchor.Program<any>;
  const burn = anchor.workspace.Burn as anchor.Program<any>;

  console.log("======================================================");
  console.log("$GRID deploy migration");
  console.log("======================================================");
  console.log("Program IDs:");
  console.log("  grid-token:", gridToken.programId.toBase58());
  console.log("  emission:  ", emission.programId.toBase58());
  console.log("  vesting:   ", vesting.programId.toBase58());
  console.log("  staking:   ", staking.programId.toBase58());
  console.log("  burn:      ", burn.programId.toBase58());
  console.log("Deployer admin:", admin.publicKey.toBase58());

  // 1. Mint
  const mint = await createMint(
    conn,
    admin,
    admin.publicKey,
    admin.publicKey,
    9,
    Keypair.generate(),
    undefined,
    TOKEN_2022_PROGRAM_ID,
  );
  console.log("Created $GRID mint:", mint.toBase58());

  // 2. grid_token::initialize_config
  const [config] = PublicKey.findProgramAddressSync(
    [Buffer.from("grid-config"), mint.toBuffer()],
    gridToken.programId,
  );
  await gridToken.methods
    .initializeConfig()
    .accountsPartial({
      config,
      mint,
      admin: admin.publicKey,
      systemProgram: SystemProgram.programId,
    })
    .rpc();
  console.log("grid-token config initialized:", config.toBase58());

  // 3. grid_token::set_metadata
  const [metadata] = PublicKey.findProgramAddressSync(
    [Buffer.from("grid-metadata"), mint.toBuffer()],
    gridToken.programId,
  );
  await gridToken.methods
    .setMetadata("iogrid", "GRID", "https://iogrid.org/token/grid.json")
    .accountsPartial({
      config,
      metadata,
      admin: admin.publicKey,
      systemProgram: SystemProgram.programId,
    })
    .rpc();
  console.log("grid-token metadata set:", metadata.toBase58());

  // 4. burn::initialize_registry
  const [burnRegistry] = PublicKey.findProgramAddressSync(
    [Buffer.from("burn-registry"), mint.toBuffer()],
    burn.programId,
  );
  await burn.methods
    .initializeRegistry()
    .accountsPartial({
      registry: burnRegistry,
      mint,
      attestor: admin.publicKey,
      admin: admin.publicKey,
      systemProgram: SystemProgram.programId,
    })
    .rpc();
  console.log("burn registry initialized:", burnRegistry.toBase58());

  // 5. emission::initialize
  const [emissionConfig] = PublicKey.findProgramAddressSync(
    [Buffer.from("emission-config"), mint.toBuffer()],
    emission.programId,
  );
  const [emissionVaultAuthority] = PublicKey.findProgramAddressSync(
    [Buffer.from("emission-vault-authority"), mint.toBuffer()],
    emission.programId,
  );
  const emissionVault = getAssociatedTokenAddressSync(
    mint,
    emissionVaultAuthority,
    true,
    TOKEN_2022_PROGRAM_ID,
    ASSOCIATED_TOKEN_PROGRAM_ID,
  );
  // emission vault is created out-of-band (ATA) — caller pre-creates with createAssociatedTokenAccount
  // We init via the program; pre-create with the SPL helper.
  const { createAssociatedTokenAccount } = await import("@solana/spl-token");
  await createAssociatedTokenAccount(
    conn,
    admin,
    mint,
    emissionVaultAuthority,
    undefined,
    TOKEN_2022_PROGRAM_ID,
    ASSOCIATED_TOKEN_PROGRAM_ID,
    true,
  );
  const now = Math.floor(Date.now() / 1000);
  await emission.methods
    .initialize(new anchor.BN(now))
    .accountsPartial({
      config: emissionConfig,
      mint,
      vault: emissionVault,
      vaultAuthority: emissionVaultAuthority,
      billingSigner: admin.publicKey,
      admin: admin.publicKey,
      tokenProgram: TOKEN_2022_PROGRAM_ID,
      systemProgram: SystemProgram.programId,
    })
    .rpc();
  console.log("emission config initialized:", emissionConfig.toBase58());

  // 6. staking::initialize_pool
  const [stakingPool] = PublicKey.findProgramAddressSync(
    [Buffer.from("staking-pool"), mint.toBuffer()],
    staking.programId,
  );
  const [stakingVaultAuthority] = PublicKey.findProgramAddressSync(
    [Buffer.from("staking-vault-authority"), mint.toBuffer()],
    staking.programId,
  );
  const [stakeVault] = PublicKey.findProgramAddressSync(
    [Buffer.from("staking-stake-vault"), mint.toBuffer()],
    staking.programId,
  );
  const [rewardVault] = PublicKey.findProgramAddressSync(
    [Buffer.from("staking-reward-vault"), mint.toBuffer()],
    staking.programId,
  );
  await staking.methods
    .initializePool(500) // 5% annual yield
    .accountsPartial({
      pool: stakingPool,
      mint,
      stakeVault,
      rewardVault,
      vaultAuthority: stakingVaultAuthority,
      admin: admin.publicKey,
      tokenProgram: TOKEN_2022_PROGRAM_ID,
      systemProgram: SystemProgram.programId,
    })
    .rpc();
  console.log("staking pool initialized:", stakingPool.toBase58());

  console.log("======================================================");
  console.log("Deploy complete. Summary:");
  console.log("  Mint:           ", mint.toBase58());
  console.log("  GridConfig:     ", config.toBase58());
  console.log("  GridMetadata:   ", metadata.toBase58());
  console.log("  BurnRegistry:   ", burnRegistry.toBase58());
  console.log("  EmissionConfig: ", emissionConfig.toBase58());
  console.log("  EmissionVault:  ", emissionVault.toBase58());
  console.log("  StakingPool:    ", stakingPool.toBase58());
  console.log("======================================================");
  console.log("Next steps (multisig-gated, NOT run by this script):");
  console.log("  - Mint 5 allocation tranches (50/15/10/10/10/5%)");
  console.log("  - Rotate attestor + billing_signer to billing-svc HSM key");
  console.log("  - transfer_mint_authority -> emission PDA");
  console.log("  - lock_authorities (one-way switch)");
  console.log("======================================================");
};
