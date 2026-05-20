// jest-dom matchers — augment vitest's `Assertion<T>` interface so the
// per-test `import "@testing-library/jest-dom/vitest"` side-effect is
// honoured by tsc as well as by the vitest runner. In jest-dom 6.6+ the
// upstream `vitest.d.ts` declares against `module 'vitest'`, but
// `vitest@2.1.x` re-exports `Assertion` from `'@vitest/expect'` — the
// downstream module the `expect(...)` call site actually resolves to.
// Without a matching augmentation on `'@vitest/expect'` every
// `expect(...).toBeInTheDocument()` in `src/test/*.test.tsx` fails
// TS2339 (#361). The augmentation below mirrors jest-dom's own
// `types/vitest.d.ts` but targets the correct module.
import "@testing-library/jest-dom";
import type { TestingLibraryMatchers } from "@testing-library/jest-dom/matchers";

declare module "@vitest/expect" {
  interface Assertion<T = unknown>
    extends TestingLibraryMatchers<unknown, T> {}
  interface AsymmetricMatchersContaining
    extends TestingLibraryMatchers<unknown, unknown> {}
}

declare module "vitest" {
  interface Assertion<T = unknown>
    extends TestingLibraryMatchers<unknown, T> {}
  interface AsymmetricMatchersContaining
    extends TestingLibraryMatchers<unknown, unknown> {}
}
