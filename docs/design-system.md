# iogrid design system

> **Authority**: canonical. This document supersedes every prior visual-design
> sketch (none merged). For repo-wide engineering rules see the
> "Engineering principles" section of the [root `README.md`](../README.md)
> and the project [`CLAUDE.md`](../CLAUDE.md).

The iogrid surface adopts a **Linear / Notion / Vercel** aesthetic:
restrained palette, single sans typeface, whitespace as the primary
design element, borders (not shadows) for elevation, and NO decorative
illustrations. The visual identity is owned by a small set of CSS
tokens; every surface — landing, /provide, /customer, /vpn, /account,
and the future `admin/` app — consumes those tokens via Tailwind utility
classes so a revamp ships from a single edit-site.

## Source-of-truth files

| File | Role |
|---|---|
| [`web/src/styles/design-tokens.css`](../web/src/styles/design-tokens.css) | All tokens — color, type, spacing, radius, motion. Light + dark themes. |
| [`web/src/app/globals.css`](../web/src/app/globals.css) | Tailwind 4 `@theme inline` bridge — exposes the tokens as utility classes. |
| [`web/tailwind.config.ts`](../web/tailwind.config.ts) | Content globs only. Theme lives in CSS. |
| [`web/src/components/ui/*.tsx`](../web/src/components/ui) | shadcn primitives re-themed against the tokens (button, card, input). |
| [`web/src/app/page.tsx`](../web/src/app/page.tsx) | Reference landing page that exercises the system end-to-end. |

## Tokens

### Color — three layers

The palette is structured in three layers so any consumer can pick the
level of abstraction they need:

**L1 — base palette**. The raw colors. Never reference from a component.

| Token | Light | Dark |
|---|---|---|
| `--gray-0` … `--gray-950` | Achromatic ramp | (same ramp) |
| `--accent-500` / `--accent-600` | Electric blue, the ONE accent | (same) |
| `--success-500` / `--warning-500` / `--danger-500` | Semantic moods | (same) |

**L2 — semantic roles**. What components SHOULD reference.

| Token | Meaning |
|---|---|
| `--color-background` | Page background. |
| `--color-surface` | Card / popover surface. |
| `--color-surface-muted` | Subtle stripe (table headers, hover). |
| `--color-foreground` | High-emphasis text. |
| `--color-foreground-muted` | Low-emphasis text (captions, hints). |
| `--color-border` | Hairline. |
| `--color-border-strong` | Hover/focus border. |
| `--color-accent` / `--color-accent-strong` / `--color-accent-foreground` | Primary CTA only. |
| `--color-success` / `--color-warning` / `--color-danger` | Use ONLY when the meaning is unmistakable. |

**L3 — shadcn aliases**. The unprefixed names (`--background`,
`--foreground`, `--primary`, `--border`, …) map onto L2 so any shadcn
component picks up the iogrid look without changes. Use via Tailwind
utilities (`bg-background`, `text-foreground`, `border-border`,
`bg-primary text-primary-foreground`, …).

### Type

Single sans typeface — **Inter**, loaded via `next/font` so production
ships zero CDN font calls. The scale is a 1.25 modular scale anchored
at 16px:

| Token | Size | Use |
|---|---|---|
| `--text-xs` | 12px | Captions, badges. |
| `--text-sm` | 14px | Body small, table cells, buttons. |
| `--text-base` | 16px | Default body. |
| `--text-lg` | 20px | Card titles. |
| `--text-xl` | 24px | Section headings. |
| `--text-2xl` | 32px | Page titles. |
| `--text-3xl` | 48px | Hero. |
| `--text-4xl` | 64px | Landing hero only. |

Line-heights: `--leading-tight` (1.15) for display, `--leading-normal`
(1.6) for body, `--leading-snug` (1.35) for compact UI.

### Spacing

4px base, exposed as `--space-1` (4px) through `--space-32` (128px).
Use Tailwind's `p-*` / `gap-*` utilities — they index into the same
4px scale.

### Radius

Subtle rounding only. Sharp corners read as premium.

| Token | Px | Use |
|---|---|---|
| `--radius-sm` | 4 | Inputs, small buttons. |
| `--radius-md` | 6 | Buttons, cards, popovers. |
| `--radius-lg` | 8 | Section containers. |
| `--radius-pill` | 9999 | Status pills only. |

### Elevation

**Borders > shadows.** A `border-border` against `bg-card` is the
default elevation. `--shadow-overlay` exists for transient overlays
(toasts, dropdowns) where a hairline alone is insufficient.

### Motion

`--motion-fast` (120ms) for hover/press, `--motion-base` (180ms) for
panel slides, `--motion-slow` (280ms) only for full-screen transitions.
All use `--motion-ease` (`cubic-bezier(0.4, 0, 0.2, 1)`).

## Components

### Button

`src/components/ui/button.tsx` — six variants:

| Variant | When |
|---|---|
| `default` (primary) | The ONE accent CTA per surface. |
| `secondary` | Neutral, bordered. Pairs with `default`. |
| `outline` | Same shape on tinted surfaces. |
| `ghost` | Text-only row/toolbar action. |
| `destructive` | Irreversible mutation (delete, revoke, wipe). |
| `link` | Inline anchor styling. |

Sizes: `sm` (36px), `default` (40px), `lg` (44px), `icon` (square).

### Card

`src/components/ui/card.tsx` — `border + bg-card`. No shadow.

### Input

`src/components/ui/input.tsx` — `border-input`, `bg-background`,
`focus-visible:ring-2 focus-visible:ring-ring`.

## Do / Don't

| Do | Don't |
|---|---|
| Use **one** primary CTA per page. | Stack three colored buttons next to each other. |
| Reach for whitespace before reaching for color. | Add a colored card to "make the section pop". |
| Use the single sans typeface at the documented sizes. | Introduce a display font or a second family. |
| Use icons from `lucide-react` for affordance. | Insert decorative illustrations of human figures, isometric scenes, or "abstract tech" graphics. |
| Show real numbers in trust sections. | Fabricate "X providers in Y countries" metrics. |
| Use `bg-primary` for the one accent CTA. | Use rainbow gradients or purple-pink "techy" cliches. |
| Reference L2/L3 tokens from components. | Reference `--gray-500` or raw hex from a component. |
| Use `border-border` for elevation. | Reach for `shadow-lg` on a card. |

## Migration status

| Surface | Status | Notes |
|---|---|---|
| `web/src/app/page.tsx` (landing) | ✅ migrated in this PR | Reference implementation. |
| `web/src/components/ui/button.tsx` | ✅ migrated | All variants on tokens. |
| `web/src/components/ui/card.tsx` | ✅ migrated | Border-elevation. |
| `web/src/components/ui/input.tsx` | ✅ migrated | Ring on focus. |
| `web/src/app/{provide,customer,account,vpn,install,onboard,wallet,burn}/**` | 🔴 Phase 2.2 follow-up | Still references zinc-* directly — drop-in token replacement per file. ~49 files. |
| `web/src/components/layout/portal-shell.tsx` | 🔴 Phase 2.2 follow-up | Portal chrome (shared by 4 surfaces) migrates with the surfaces. |
| `admin/` | 🔴 Phase 1 (parallel) → Phase 2.3 | New repo will adopt tokens from day-1. |

The bridge in `globals.css` keeps the legacy palette working while the
49 remaining files migrate one-by-one — there is no big-bang flag day.

## Banned visual patterns

- Decorative illustrations of human figures, isometric "developer at desk"
  scenes, "abstract tech particles", or any stock-vector art.
- Multiple font families (e.g. body + display).
- Purple-pink / "cyberpunk" gradients on hero or CTAs.
- Heavy drop-shadows (`shadow-xl` and above).
- More than one accent color on a single surface.
- Fabricated trust metrics (made-up provider/country/byte counters).

## See also

- [`docs/ARCHITECTURE.md`](./ARCHITECTURE.md) — system architecture (links the design-system file in the inventory).
- EPIC [#422](https://github.com/iogrid/iogrid/issues/422) — Phase plan (this PR is Phase 2.1).
