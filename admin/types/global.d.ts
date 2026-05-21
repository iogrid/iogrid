/// <reference types="react/canary" />

// React canary types — required so async Server Components (return
// Promise<Element>) typecheck as valid JSX components and
// `<form action={serverAction}>` accepts a function value. Mirrors
// `web/types/global.d.ts` (see #166 for the original rationale).
//
// This file is intentionally a SCRIPT (no top-level imports / exports)
// so the triple-slash reference is program-wide. The vitest Assertion
// augmentation lives in `vitest-matchers.d.ts` next to it as a MODULE
// file; both contribute to the compilation unit via tsconfig's
// `**/*.ts` include glob.
