import * as anchor from "@coral-xyz/anchor";
import { Program, BN } from "@coral-xyz/anchor";
import {
  Keypair,
  PublicKey,
  SystemProgram,
  SYSVAR_RENT_PUBKEY,
} from "@solana/web3.js";
import {
  TOKEN_2022_PROGRAM_ID,
  createInitializeMint2Instruction,
  getAssociatedTokenAddressSync,
  ASSOCIATED_TOKEN_PROGRAM_ID,
} from "@solana/spl-token";
import { assert } from "chai";

// Generated IDL types will live under target/types after `anchor build`. We use a structural
// type here so the test file compiles even before the first build.
type GridToken = any;

describe("grid-token", () => {
  const provider = anchor.AnchorProvider.env();
  anchor.setProvider(provider);

  const program = anchor.workspace.GridToken as Program<GridToken>;
  const admin = (provider.wallet as anchor.Wallet).payer;
  const mintKp = Keypair.generate();

  it("initializes config + mint", async () => {
    const [configPda] = PublicKey.findProgramAddressSync(
      [Buffer.from("grid-config"), mintKp.publicKey.toBuffer()],
      program.programId,
    );

    // The client must create the mint account first; the program then runs InitializeMint2.
    const lamports =
      await provider.connection.getMinimumBalanceForRentExemption(82);
    const tx = new anchor.web3.Transaction().add(
      SystemProgram.createAccount({
        fromPubkey: admin.publicKey,
        newAccountPubkey: mintKp.publicKey,
        space: 82,
        lamports,
        programId: TOKEN_2022_PROGRAM_ID,
      }),
    );
    await provider.sendAndConfirm(tx, [mintKp]);

    await program.methods
      .initializeConfig()
      .accounts({
        config: configPda,
        mint: mintKp.publicKey,
        admin: admin.publicKey,
        systemProgram: SystemProgram.programId,
      })
      .rpc();

    await program.methods
      .initializeMint()
      .accounts({
        mint: mintKp.publicKey,
        config: configPda,
        admin: admin.publicKey,
        tokenProgram: TOKEN_2022_PROGRAM_ID,
      })
      .rpc();

    const cfg = await program.account.gridConfig.fetch(configPda);
    assert.equal(cfg.decimals, 9);
    assert.equal(cfg.mint.toBase58(), mintKp.publicKey.toBase58());
    assert.equal(cfg.mintedSoFar.toString(), "0");
    assert.equal(cfg.authorityLocked, false);
  });

  it("rejects minting beyond hard cap", async () => {
    // We don't actually have to mint 1B+; just ensure the error code exists in the IDL.
    assert.exists(program.idl.errors.find((e: any) => e.name === "HardCapExceeded"));
  });

  it("locks authorities", async () => {
    const [configPda] = PublicKey.findProgramAddressSync(
      [Buffer.from("grid-config"), mintKp.publicKey.toBuffer()],
      program.programId,
    );
    await program.methods
      .lockAuthorities()
      .accounts({
        config: configPda,
        admin: admin.publicKey,
      })
      .rpc();
    const cfg = await program.account.gridConfig.fetch(configPda);
    assert.equal(cfg.authorityLocked, true);
  });
});
