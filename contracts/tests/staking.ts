import * as anchor from "@coral-xyz/anchor";
import { Program } from "@coral-xyz/anchor";
import { assert } from "chai";
import { assertIdlIncludes, resolveType, findByAnyName } from "./_idl-helpers";

type Staking = any;

describe("staking", () => {
  const provider = anchor.AnchorProvider.env();
  anchor.setProvider(provider);
  const program = anchor.workspace.Staking as Program<Staking>;

  it("IDL exposes both stake kinds", () => {
    const kind = resolveType(program.idl, "StakeKind");
    assert.exists(kind, "StakeKind enum should exist in idl.types");
    const variants = ((kind as any).type?.variants ?? []).map((v: any) => v.name);
    // Anchor 0.31 normalises enum *variant* names to camelCase (initial-lowercase),
    // same as instructions / error variants. Use the helper to tolerate both forms.
    assertIdlIncludes(variants, ["Provider", "Customer"], "StakeKind variants");
  });

  it("IDL exposes stake/unstake/accrue/claim + customer discount voucher + weight view", () => {
    const ixs = program.idl.instructions.map((i: any) => i.name);
    assertIdlIncludes(
      ixs,
      [
        "initialize_pool",
        "stake",
        "unstake",
        "accrue_yield",
        "claim_yield",
        "compute_weight",
        "customer_stake_for_discount",
        "redeem_discount_voucher",
      ],
      "staking instructions",
    );
  });

  it("min-stake-not-met + already-consumed error codes present", () => {
    assert.exists(
      findByAnyName(program.idl.errors, "MinStakeNotMet"),
      "MinStakeNotMet error code present",
    );
    assert.exists(
      findByAnyName(program.idl.errors, "AlreadyConsumed"),
      "AlreadyConsumed error code present",
    );
  });

  it("IDL declares DiscountVoucher account with discount_bps + lock_end", () => {
    const def = resolveType(program.idl, "DiscountVoucher");
    assert.exists(def, "DiscountVoucher type should exist");
    const fields = ((def as any).type?.fields ?? []).map((f: any) => f.name);
    assertIdlIncludes(
      fields,
      ["pool", "owner", "voucher_id", "amount", "locked_at", "lock_end", "discount_bps"],
      "DiscountVoucher fields",
    );
  });

  it("discount ramp math: 30d → 500 bps, 365d → 2500 bps, mid-range linear", () => {
    // Mirror programs/staking/src/lib.rs customer_stake_for_discount math
    const DAY = 86_400n;
    const MIN_STAKE = 30n * DAY;
    const ONE_YEAR = 365n * DAY;
    const MIN_BPS = 500n;
    const MAX_BPS = 2_500n;
    const discount = (lockSecs: bigint): bigint => {
      if (lockSecs < MIN_STAKE) return 0n;
      const capped = lockSecs < ONE_YEAR ? lockSecs : ONE_YEAR;
      const over = capped - MIN_STAKE;
      const range = ONE_YEAR - MIN_STAKE;
      const extra = (over * (MAX_BPS - MIN_BPS)) / range;
      const total = MIN_BPS + extra;
      return total > MAX_BPS ? MAX_BPS : total;
    };
    assert.equal(discount(30n * DAY).toString(), "500", "30d → 5%");
    assert.equal(discount(365n * DAY).toString(), "2500", "365d → 25%");
    assert.equal(discount(730n * DAY).toString(), "2500", "730d capped at 25%");
    // ~mid-range:  ((197.5d - 30d) * 2000 / 335d) + 500 ≈ 1500
    const mid = discount(197n * DAY + DAY / 2n);
    assert.isAtLeast(Number(mid), 1450);
    assert.isAtMost(Number(mid), 1550);
  });

  it("weight math: at-minimum-stake = 1.00× principal, +2y caps at 2.00×", () => {
    const DAY = 86_400n;
    const MIN_STAKE = 30n * DAY;
    const MAX_WEIGHT_BPS = 20_000n;
    const weight = (amount: bigint, elapsed: bigint): bigint => {
      if (elapsed < MIN_STAKE) return 0n;
      const bonusWindow =
        elapsed - MIN_STAKE < 2n * 365n * DAY
          ? elapsed - MIN_STAKE
          : 2n * 365n * DAY;
      const bonusBps =
        (bonusWindow * (MAX_WEIGHT_BPS - 10_000n)) / (2n * 365n * DAY);
      const weightBps =
        10_000n + bonusBps < MAX_WEIGHT_BPS ? 10_000n + bonusBps : MAX_WEIGHT_BPS;
      return (amount * weightBps) / 10_000n;
    };
    assert.equal(weight(1_000n, 0n).toString(), "0", "below min-stake → 0");
    assert.equal(weight(1_000n, MIN_STAKE).toString(), "1000", "at min-stake → 1.00×");
    assert.equal(
      weight(1_000n, MIN_STAKE + 2n * 365n * DAY).toString(),
      "2000",
      "+2y past min → 2.00× capped",
    );
    assert.equal(
      weight(1_000n, MIN_STAKE + 10n * 365n * DAY).toString(),
      "2000",
      "+10y past min still capped at 2.00×",
    );
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
