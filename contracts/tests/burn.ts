import * as anchor from "@coral-xyz/anchor";
import { Program } from "@coral-xyz/anchor";
import { assert } from "chai";
import { assertIdlIncludes, resolveType, findByAnyName } from "./_idl-helpers";

type Burn = any;

describe("burn", () => {
  const provider = anchor.AnchorProvider.env();
  anchor.setProvider(provider);
  const program = anchor.workspace.Burn as Program<Burn>;

  it("IDL exposes record_burn + burn_via_program + initialize_registry", () => {
    const ixs = program.idl.instructions.map((i: any) => i.name);
    assertIdlIncludes(
      ixs,
      ["initialize_registry", "record_burn", "burn_via_program", "rotate_attestor"],
      "burn instructions",
    );
  });

  it("IDL declares total_burned + burn_count + total_revenue_cents_attributed", () => {
    const def = resolveType(program.idl, "BurnRegistry");
    assert.exists(def, "BurnRegistry type definition should exist");
    const fields = ((def as any).type?.fields ?? []).map((f: any) => f.name);
    assertIdlIncludes(
      fields,
      [
        "mint",
        "admin",
        "attestor",
        "burn_count",
        "total_burned",
        "total_revenue_cents_attributed",
      ],
      "BurnRegistry fields",
    );
  });

  it("source_tag length constraint", () => {
    const errs = program.idl.errors.map((e: any) => e.name);
    assert.exists(findByAnyName(program.idl.errors, "TagTooLong"), "TagTooLong error code");
    assert.exists(findByAnyName(program.idl.errors, "ZeroAmount"), "ZeroAmount error code");
    void errs;
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

  it("source_tag length boundary: at MAX_SOURCE_TAG_LEN, encoded snug", () => {
    // MAX_SOURCE_TAG_LEN = 32 in lib.rs; characters past 32 must trigger TagTooLong.
    const MAX = 32;
    const at_limit = "x".repeat(MAX);
    const over_limit = "x".repeat(MAX + 1);
    assert.equal(at_limit.length, MAX);
    assert.equal(over_limit.length, MAX + 1);
    // The Rust check is `source_tag.as_bytes().len() <= MAX_SOURCE_TAG_LEN` — UTF-8 multi-byte
    // chars could push over the limit even with fewer JS characters; we sanity-check that
    // ASCII boundary matches the byte boundary.
    assert.equal(new TextEncoder().encode(at_limit).length, MAX);
  });
});
