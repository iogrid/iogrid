# Color system

Source of truth: `brand/tokens/colors.json`. This document is the human-readable companion.

## Primary palette

The brand-primary is a deep, slightly desaturated blue-violet — readable on light/dark, distinguishable from datacenter-blue (AWS / Azure / NordVPN) and crypto-purple (Solana). It says **infrastructure with personality**.

| Token | Hex | Use |
|-------|-----|-----|
| `primary-50`  | `#EEF0FF` | Very light tint, subtle hover states |
| `primary-100` | `#D9DEFF` | Background washes |
| `primary-200` | `#B3BCFF` | Disabled / muted accents |
| `primary-300` | `#8C99FF` | Decorative highlights |
| `primary-400` | `#6577FF` | Interactive hover |
| `primary-500` | `#4257F5` | **Base brand** — buttons, links, mark color |
| `primary-600` | `#2F3FC9` | Pressed / active |
| `primary-700` | `#22309C` | Headings on light bg |
| `primary-800` | `#19236E` | Deep accents |
| `primary-900` | `#10174A` | **Darkest brand** — dark-mode surfaces |

## Accent palette — mesh-green

Used sparingly: success states, "you're earning" pulse, transparency dashboard "all clear" badges.

| Token | Hex | Use |
|-------|-----|-----|
| `accent-300` | `#7BE0B7` | Subtle accents |
| `accent-500` | `#2EC78B` | **Base accent** — earnings counter, online indicators |
| `accent-700` | `#1B8C5F` | Hover on green CTAs |

## Neutral palette

Warm gray, slightly toward stone. Pairs better with the violet primary than a pure-cool gray.

| Token | Hex | Use |
|-------|-----|-----|
| `neutral-0`   | `#FFFFFF` | Page background (light) |
| `neutral-50`  | `#FAFAF9` | Section background (light) |
| `neutral-100` | `#F2F1EE` | Card background (light) |
| `neutral-200` | `#E5E3DE` | Borders, dividers |
| `neutral-300` | `#C9C6BE` | Disabled text |
| `neutral-400` | `#A29E94` | Placeholder text |
| `neutral-500` | `#7B776D` | Secondary text |
| `neutral-600` | `#56524A` | Body text on light |
| `neutral-700` | `#3A3731` | Headings on light |
| `neutral-800` | `#23211D` | Dark-mode body text |
| `neutral-900` | `#15140F` | Page background (dark) |

## Semantic colors

| Token | Hex | Use |
|-------|-----|-----|
| `success-500` | `#2EC78B` | Same as `accent-500` |
| `warning-500` | `#E6A23C` | Soft amber — provider notices |
| `danger-500`  | `#E5484D` | Errors, blocked traffic, abuse flags |
| `info-500`    | `#4257F5` | Same as `primary-500` |

## Usage rules

1. **Primary is for action**, not decoration. Only one primary-button per screen.
2. **Accent-green is for state**, not branding. Don't make the logo green.
3. **Neutral text-on-light** uses `neutral-700` for headings, `neutral-600` for body. Never `neutral-900` on white — too harsh.
4. **Dark mode** flips: `neutral-900` for page bg, `neutral-100` for body text.
5. **Contrast minimum** WCAG AA: all text/bg combinations in this palette are pre-verified for AA compliance.
