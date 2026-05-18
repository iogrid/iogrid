import * as anchor from "@coral-xyz/anchor";

// Runs BEFORE all other tests (alphabetical order — `_` sorts before letters).
// Dumps every program's IDL top-level keys + a few sample names so CI logs surface the
// actual Anchor-CLI-generated shape (instruction names, account names, error names, types).
// This makes it trivial to update other tests when the Anchor IDL format changes between
// versions (e.g., 0.30 -> 0.31 normalised many identifiers to camelCase).

describe("_idl-dump (informational, never fails)", () => {
  const provider = anchor.AnchorProvider.env();
  anchor.setProvider(provider);

  const programs = [
    "Burn",
    "Emission",
    "Vesting",
    "Staking",
    "GridToken",
  ] as const;

  for (const name of programs) {
    it(`dumps ${name} IDL surface`, () => {
      const program = (anchor.workspace as any)[name];
      if (!program) {
        console.log(`[idl-dump] workspace.${name} is not available — skipping`);
        return;
      }
      const idl = program.idl;
      const summary = {
        topKeys: Object.keys(idl),
        version: idl.metadata?.version ?? idl.version,
        spec: idl.metadata?.spec,
        instructions: (idl.instructions ?? []).map((i: any) => i.name),
        accounts: (idl.accounts ?? []).map((a: any) => a.name),
        errors: (idl.errors ?? []).map((e: any) => e.name),
        types: (idl.types ?? []).map((t: any) => t.name),
      };
      console.log(`[idl-dump] ${name}: ${JSON.stringify(summary, null, 2)}`);
    });
  }
});
