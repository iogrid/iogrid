# iogrid brand identity

The single source of truth for iogrid's **brand expression** — logo, voice, and palette intent. Everything that ships under the iogrid name — landing site, web management plane, daemon installer screens, App Store assets — aligns to this folder.

> **Implementation note.** The *live, code-enforced* design tokens (colors, type, spacing, radius, motion) live in [`web/src/styles/design-tokens.css`](../web/src/styles/design-tokens.css) and are documented in [`docs/design-system.md`](../docs/design-system.md) — the canonical authority for the Linear/Notion/Vercel surface aesthetic. The palette here mirrors those tokens (indigo `primary` + minty-green `accent` + warm neutral ramp). When the two ever diverge, `design-tokens.css` wins for anything that renders in the product; this folder governs brand-asset usage (logo, voice, clearspace).

## Contents

| Path | Purpose |
|------|---------|
| `logo/wordmark.svg` | Primary horizontal wordmark "iogrid" |
| `logo/mark.svg` | Standalone icon (hexagonal mesh motif) |
| `logo/monochrome.svg` | Single-color wordmark for one-ink prints, T-shirts, dark UI overlays |
| `logo/reversed.svg` | Light-on-dark wordmark for use on the brand-primary background |
| `colors.md` | Palette reference (primary, accent, neutral, semantic) — mirrors `design-tokens.css` |
| `typography.md` | Type system spec (UI / display / code / numerical) |
| `tokens/colors.json` | Machine-readable color tokens for brand assets |
| `tokens/spacing.json` | Spacing scale |

## Brand voice

| Audience | Tone | Example |
|----------|------|---------|
| Providers (supply side) | Friendly, candid, plain-spoken. Slightly conspiratorial-in-a-good-way. Always disclose what their PC is doing. | "Your PC is idle 16 hours a day. Let's put it to work — and you'll see every byte." |
| B2B customers (demand side) | Professional, technical, confident. Show pricing, show SLAs, show the audit log. | "Residential proxy with cryptographic audit. 30% cheaper than Bright Data. Same coverage." |
| Token / community | Utility-first, no price talk. The token is a unit of work, not a speculation. | "Earn $GRID for the work your PC contributes. Spend it on services. Or hold it. Your choice." |

Never use crypto-bro language, hype emojis, "to the moon," "wagmi," or any pump phrasing. iogrid is a network, not a meme coin.

## Logo construction

The mark is a **hexagonal mesh** evoking both the grid (orthogonal) and the network (connections between nodes). Six nodes arranged on the vertices of a regular hexagon, connected by edges. Reads as:

- a **node in a mesh** (provider perspective)
- a **distributed graph** (network perspective)
- a **molecular structure** (precision / engineered)

The wordmark uses a lowercase **`iogrid`** in a geometric sans (Inter ExtraBold customized — the only modification is a slight straightening of the lowercase `g` descender). All lowercase, no period, no space. The `o` is the only round glyph and aligns with the mark.

## Clearspace + minimum size

- Clear space around any logo asset: `0.5 × cap-height` on all sides
- Minimum width: wordmark 80px, mark 24px
- Below those minimums, use the monochrome variant
