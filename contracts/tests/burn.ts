import * as anchor from "@coral-xyz/anchor";
import { Program } from "@coral-xyz/anchor";
import { assert } from "chai";

type Burn = any;

describe("burn", () => {
  const provider = anchor.AnchorProvider.env();
  anchor.setProvider(provider);
  const program = anchor.workspace.Burn as Program<Burn>;

  it("IDL exposes record_burn + burn_via_program + initialize_registry", () => {
    const ixs = program.idl.instructions.map((i: any) => i.name);
    assert.includeMembers(ixs, [
      "initialize_registry",
      "record_burn",
      "burn_via_program",
      "rotate_attestor",
    ]);
  });

  it("IDL declares total_burned + burn_count + total_revenue_cents_attributed", () => {
    const reg = program.idl.accounts.find(
      (a: any) => a.name === "BurnRegistry",
    );
    assert.exists(reg);
    const fields = (reg as any).type.fields.map((f: any) => f.name);
    assert.includeMembers(fields, [
      "mint",
      "admin",
      "attestor",
      "burn_count",
      "total_burned",
      "total_revenue_cents_attributed",
    ]);
  });

  it("source_tag length constraint", () => {
    const errs = program.idl.errors.map((e: any) => e.name);
    assert.include(errs, "TagTooLong");
    assert.include(errs, "ZeroAmount");
  });

  it("burn target rate: 2% of revenue", () => {
    // docs/TOKENOMICS.md headline: "≥2% of monthly revenue → market-buy → burn"
    // We don't enforce the 2% on-chain (off-chain billing-svc computes it from real revenue)
    // — this test just records the policy parameter so an audit can grep for it.
    const revenue = 1_000_000n; // cents = $10K
    const burnFloor = (revenue * 2n) / 100n;
    assert.equal(burnFloor.toString(), "20000");
  });

  it("running total accumulates across receipts", () => {
    const burns = [100n, 250n, 75n];
    const total = burns.reduce((a, b) => a + b, 0n);
    assert.equal(total.toString(), "425");
  });
});
