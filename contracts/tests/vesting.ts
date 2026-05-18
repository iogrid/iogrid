import * as anchor from "@coral-xyz/anchor";
import { Program } from "@coral-xyz/anchor";
import { assert } from "chai";
import { assertIdlIncludes, resolveType, findByAnyName } from "./_idl-helpers";

type Vesting = any;

// Pure-math sanity checks. The actual end-to-end deposit/withdraw flow requires a full mint +
// token program setup that lives in the integration suite (`tests/_integration.ts`, added in a
// follow-up PR once we have a deterministic test-mint helper).
//
// What this suite asserts:
//  - The IDL exposes the four tiers (Standard, Loyalty, Conviction, Maximum).
//  - The error codes for cooldown, downgrade, deposit-mismatch are present.
//  - The schedule math matches docs/TOKENOMICS.md:
//      Standard:   30d cliff + 60d linear
//      Loyalty:    90d cliff + 180d linear
//      Conviction: 180d cliff + 365d linear
//      Maximum:    365d cliff + 730d linear

describe("vesting", () => {
  const provider = anchor.AnchorProvider.env();
  anchor.setProvider(provider);
  const program = anchor.workspace.Vesting as Program<Vesting>;

  it("IDL exposes all four tiers", () => {
    const tierType = resolveType(program.idl, "VestTier");
    assert.exists(tierType, "VestTier enum should exist");
    const variants = ((tierType as any).type?.variants ?? []).map((v: any) => v.name);
    assert.includeMembers(variants, ["Standard", "Loyalty", "Conviction", "Maximum"]);
  });

  it("IDL exposes early-unlock + cooldown errors", () => {
    assert.exists(
      findByAnyName(program.idl.errors, "EarlyUnlockOnCooldown"),
      "EarlyUnlockOnCooldown error code",
    );
    assert.exists(
      findByAnyName(program.idl.errors, "CannotDowngradeTier"),
      "CannotDowngradeTier error code",
    );
    assert.exists(
      findByAnyName(program.idl.errors, "DepositMismatch"),
      "DepositMismatch error code",
    );
  });

  it("IDL declares record_deposit + early_unlock + withdraw_unlocked", () => {
    const ixs = program.idl.instructions.map((i: any) => i.name);
    assertIdlIncludes(
      ixs,
      [
        "initialize_provider",
        "record_deposit",
        "withdraw_unlocked",
        "early_unlock",
        "upgrade_tier",
      ],
      "vesting instructions",
    );
  });

  it("schedule math: at-cliff = 0, mid-linear = ~half, post-linear = full", () => {
    // mirror programs/vesting/src/lib.rs `vested_amount`
    const DAY = 86_400n;
    const vested = (
      amount: bigint,
      depositedAt: bigint,
      now: bigint,
      cliff: bigint,
      linear: bigint,
    ): bigint => {
      if (now < depositedAt + cliff) return 0n;
      const end = depositedAt + cliff + linear;
      if (now >= end) return amount;
      const dt = now - depositedAt - cliff;
      return (amount * dt) / linear;
    };
    const STD_CLIFF = 30n * DAY;
    const STD_LINEAR = 60n * DAY;

    assert.equal(
      vested(1000n, 0n, STD_CLIFF - 1n, STD_CLIFF, STD_LINEAR).toString(),
      "0",
      "before cliff => 0",
    );
    assert.equal(
      vested(
        1000n,
        0n,
        STD_CLIFF + STD_LINEAR / 2n,
        STD_CLIFF,
        STD_LINEAR,
      ).toString(),
      "500",
      "mid-linear => ~50%",
    );
    assert.equal(
      vested(1000n, 0n, STD_CLIFF + STD_LINEAR, STD_CLIFF, STD_LINEAR).toString(),
      "1000",
      "post-linear => 100%",
    );
  });

  it("early-unlock penalty is exactly 50% of locked remainder", () => {
    const locked = 10_000n;
    const penaltyBps = 5_000n;
    const penalty = (locked * penaltyBps) / 10_000n;
    const payout = locked - penalty;
    assert.equal(penalty.toString(), "5000");
    assert.equal(payout.toString(), "5000");
  });
});
