import type { Config } from "tailwindcss";

/**
 * Tailwind 4 config — kept lean. The visual system is owned by
 * `src/styles/design-tokens.css` (color/type/spacing/radius/motion
 * tokens) and exposed to Tailwind via the `@theme inline { … }` block
 * in `src/app/globals.css`. This file only declares the content globs
 * Tailwind should scan and the rare extension that doesn't make sense
 * as a CSS variable (none yet).
 *
 * Why no `theme.extend.colors` here: with Tailwind 4 the source of
 * truth for colors is `@theme` in CSS, not the TS config. Mirroring
 * tokens into both places guarantees drift. We pick CSS — closer to
 * the runtime, closer to dark-mode token flips, single edit-site.
 *
 * Legacy `brand-*` palette (the sky-blue 50/500/900 triplet) is removed
 * here: it was unused at call sites (rg shows zero hits in src/) and
 * conflicted with the new restrained palette. If a future surface
 * needs a brand color, it should consume the semantic `accent` token
 * instead of re-introducing a parallel ramp.
 */
const config: Config = {
  content: [
    "./src/app/**/*.{ts,tsx,mdx}",
    "./src/components/**/*.{ts,tsx,mdx}",
    "./src/lib/**/*.{ts,tsx,mdx}",
    "./src/styles/**/*.css",
  ],
  theme: {
    extend: {},
  },
  plugins: [],
};

export default config;
