import * as anchor from "@coral-xyz/anchor";
import { Program } from "@coral-xyz/anchor";
import { assert } from "chai";
import { assertIdlIncludes, resolveType, findByAnyName } from "./_idl-helpers";

// Generated IDL types will live under target/types after `anchor build`. We use a structural
// type here so the test file compiles even before the first build.
type GridToken = any;

// This suite is IDL-shape only. The end-to-end mint / lock-authorities flow lives in the
// integration suite (added once a Token-2022 mint helper exists that boots a deterministic
// validator with the SPL Token-2022 program already loaded). `anchor test --skip-deploy`
// uses the bundled local validator without loading our programs, so we cannot do RPC calls
// here without a full deploy step.

describe("grid-token", () => {
  const provider = anchor.AnchorProvider.env();
  anchor.setProvider(provider);
  const program = anchor.workspace.GridToken as Program<GridToken>;

  it("IDL exposes initialize_config + initialize_mint + mint_to_recipient + set_metadata + lock_authorities", () => {
    const ixs = program.idl.instructions.map((i: any) => i.name);
    assertIdlIncludes(
      ixs,
      [
        "initialize_config",
        "initialize_mint",
        "mint_to_recipient",
        "set_metadata",
        "transfer_mint_authority",
        "lock_authorities",
      ],
      "grid-token instructions",
    );
  });

  it("IDL declares the GridMetadata account with name/symbol/uri", () => {
    const def = resolveType(program.idl, "GridMetadata");
    assert.exists(def, "GridMetadata type definition should exist");
    const fields = ((def as any).type?.fields ?? []).map((f: any) => f.name);
    assertIdlIncludes(
      fields,
      ["mint", "name", "symbol", "uri", "name_len", "symbol_len", "uri_len"],
      "GridMetadata fields",
    );
  });

  it("MetadataFieldTooLong error code present", () => {
    assert.exists(
      findByAnyName(program.idl.errors, "MetadataFieldTooLong"),
      "MetadataFieldTooLong error code",
    );
  });

  it("IDL declares the GridConfig account with hard_cap + minted_so_far + authority_locked", () => {
    const def = resolveType(program.idl, "GridConfig");
    assert.exists(def, "GridConfig type definition should exist");
    const fields = ((def as any).type?.fields ?? []).map((f: any) => f.name);
    assertIdlIncludes(
      fields,
      ["admin", "mint", "hard_cap", "minted_so_far", "decimals", "authority_locked"],
      "GridConfig fields",
    );
  });

  it("hard-cap error code present", () => {
    assert.exists(
      findByAnyName(program.idl.errors, "HardCapExceeded"),
      "HardCapExceeded error code",
    );
    assert.exists(
      findByAnyName(program.idl.errors, "AuthorityLocked"),
      "AuthorityLocked error code",
    );
  });

  it("hard-cap math: 1B * 10^9 raw fits in u64", () => {
    // docs/TOKENOMICS.md: total supply = 1,000,000,000 $GRID, 9 decimals
    const totalSupply = 1_000_000_000n;
    const decimals = 9n;
    const raw = totalSupply * 10n ** decimals;
    assert.equal(raw.toString(), "1000000000000000000");
    const u64Max = (1n << 64n) - 1n;
    assert.isTrue(raw < u64Max, "hard cap must fit in u64");
  });

  it("authority lock is a one-way switch (documentation parity)", () => {
    // The `lock_authorities` instruction sets `authority_locked = true` permanently.
    // No matching unlock instruction is exposed.
    const ixs = program.idl.instructions.map((i: any) => i.name);
    assert.notInclude(
      ixs,
      "unlockAuthorities",
      "no instruction should exist that reverses the authority lock (camelCase form)",
    );
    assert.notInclude(
      ixs,
      "unlock_authorities",
      "no instruction should exist that reverses the authority lock (snake_case form)",
    );
  });
});
