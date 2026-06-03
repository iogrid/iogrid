# Typography system

## Type stack

| Role | Family | Weights | Source | Rationale |
|------|--------|---------|--------|-----------|
| **UI / Body** | Inter | 400, 500, 600, 700 | next/font/google | The clearest, most-tested geometric sans. Excellent at small sizes. |
| **Display** | Inter (700, 800, tight tracking) | 700, 800 | next/font/google | Reuse Inter at heavier weights with tighter tracking. Avoids the cost + risk of a second display face. |
| **Code / Mono** | JetBrains Mono | 400, 500, 700 | next/font/google | Ligatures off (we're a serious infra brand, not playful). Pairs well with Inter. |
| **Numerical (tabular)** | Inter (tabular-nums feature) | 500, 600 | next/font/google | OpenType `tnum` feature for pricing tables + earnings counters. |

We deliberately do NOT ship a custom display face at launch. It's a tax (extra weight, FOUT risk, design-system lock-in) for a benefit (slight uniqueness) we don't yet need. Phase 3 we can revisit if the brand calls for a hero font.

## Scale (Tailwind)

| Class | Size | Line height | Use |
|-------|------|-------------|-----|
| `text-xs` | 12px | 16px | Captions, labels, table footers |
| `text-sm` | 14px | 20px | Secondary UI text |
| `text-base` | 16px | 24px | Body |
| `text-lg` | 18px | 28px | Lead paragraphs |
| `text-xl` | 20px | 28px | Card titles |
| `text-2xl` | 24px | 32px | Subsection headers |
| `text-3xl` | 30px | 36px | Section headers |
| `text-4xl` | 36px | 40px | Page titles |
| `text-5xl` | 48px | 1 | Hero h1 |
| `text-6xl` | 60px | 1 | Hero h1 (large) |
| `text-7xl` | 72px | 1 | Landing hero only |

## Heading rules

- **Hero h1**: `text-5xl md:text-7xl font-extrabold tracking-tight`
- **Section h2**: `text-3xl md:text-4xl font-bold tracking-tight`
- **Card h3**: `text-xl font-semibold`
- Body line-height stays at 1.5–1.6 for readability
- Never use letter-spacing > 0 except for ALL-CAPS labels at `text-xs` (use `tracking-widest`)

## Code blocks

- Background: `neutral-100` light / `neutral-800` dark
- Padding: 16px
- Border-radius: 8px
- Mono at `text-sm` for inline, `text-base` for block

## Tabular numbers

For pricing tables, earnings counters, latency metrics:

```css
font-feature-settings: "tnum" 1, "lnum" 1;
```

Exposed in Tailwind via a `font-tabular` utility in the `web/` Tailwind setup (theme tokens live in [`web/src/styles/design-tokens.css`](../web/src/styles/design-tokens.css)).
