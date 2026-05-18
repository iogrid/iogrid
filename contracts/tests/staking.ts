import * as anchor from "@coral-xyz/anchor";
import { Program } from "@coral-xyz/anchor";
import { assert } from "chai";

type Staking = any;

describe("staking", () => {
  const provider = anchor.AnchorProvider.env();
  anchor.setProvider(provider);
  const program = anchor.workspace.Staking as Program<Staking>;

  it("IDL exposes both stake kinds", () => {
    const kind = program.idl.types.find((t: any) => t.name === "StakeKind");
    assert.exists(kind);
    const variants = (kind as any).type.variants.map((v: any) => v.name);
    assert.includeMembers(variants, ["Provider", "Customer"]);
  });

  it("IDL exposes stake/unstake/accrue/claim", () => {
    const ixs = program.idl.instructions.map((i: any) => i.name);
    assert.includeMembers(ixs, [
      "initialize_pool",
      "stake",
      "unstake",
      "accrue_yield",
      "claim_yield",
    ]);
  });

  it("min-stake-not-met error code present", () => {
    const errs = program.idl.errors.map((e: any) => e.name);
    assert.include(errs, "MinStakeNotMet");
  });

  it("yield math: 1y at 5% APR = 5% of principal", () => {
    const principal = 10_000n;
    const annualBps = 500n;
    const secsPerYear = 365n * 86_400n;
    const dt = secsPerYear;
    const yield_ = (principal * annualBps * dt) / (10_000n * secsPerYear);
    assert.equal(yield_.toString(), "500");
  });

  it("yield math: 30d at 5% APR ≈ 0.41% of principal", () => {
    const principal = 100_000n;
    const annualBps = 500n;
    const secsPerYear = 365n * 86_400n;
    const dt = 30n * 86_400n;
    const yield_ = (principal * annualBps * dt) / (10_000n * secsPerYear);
    // 100000 * 500 * (30/365) / 10000 = 410 (integer)
    assert.equal(yield_.toString(), "410");
  });
});
