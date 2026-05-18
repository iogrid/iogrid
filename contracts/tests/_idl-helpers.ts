// Helpers for asserting against Anchor-generated IDL shapes in a version-tolerant way.
//
// Background: between Anchor 0.30 and 0.31 the IDL spec changed (`spec: "0.1.0"`):
//   - instruction names are normalised to camelCase (snake_case Rust fn -> mixedCase IDL)
//   - error variant names follow the same camelCase convention
//   - struct/enum *type* names appear to retain PascalCase, but they live under `idl.types`
//     (with `idl.accounts` being a sibling list of names-only headers, no embedded fields)
//
// Rather than hard-coding one convention per test (and re-failing CI every time a future
// Anchor release flips a switch), tests assert via these helpers which accept BOTH the
// snake_case/PascalCase source form AND the camelCase IDL form.

export function snakeToCamel(s: string): string {
  return s.replace(/_([a-z])/g, (_m, c) => c.toUpperCase());
}

export function lowerFirst(s: string): string {
  return s.length === 0 ? s : s[0].toLowerCase() + s.slice(1);
}

/** Generate every name shape Anchor might emit for an identifier. */
export function nameVariants(name: string): string[] {
  const out = new Set<string>();
  out.add(name);
  out.add(snakeToCamel(name));
  out.add(lowerFirst(name));
  out.add(lowerFirst(snakeToCamel(name)));
  return Array.from(out);
}

/** assert.includeMembers but each `wanted` is matched against any of its name variants. */
export function assertIdlIncludes(
  haystack: string[],
  wanted: string[],
  label: string,
): void {
  const missing: string[] = [];
  for (const w of wanted) {
    const variants = nameVariants(w);
    if (!variants.some((v) => haystack.includes(v))) {
      missing.push(w);
    }
  }
  if (missing.length > 0) {
    throw new Error(
      `${label}: missing ${missing.join(", ")} in [${haystack.join(", ")}]`,
    );
  }
}

/** Find a named entry in an IDL list, accepting any name variant. */
export function findByAnyName<T extends { name: string }>(
  list: T[] | undefined,
  name: string,
): T | undefined {
  if (!list) return undefined;
  const variants = new Set(nameVariants(name));
  return list.find((t) => variants.has(t.name));
}

/** Resolve a type definition by name from `idl.types` (preferred) falling back to `idl.accounts`. */
export function resolveType(idl: any, name: string): any | undefined {
  return (
    findByAnyName(idl.types, name) ??
    findByAnyName(idl.accounts, name)
  );
}
