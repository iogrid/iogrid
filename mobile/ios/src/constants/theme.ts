/**
 * iogrid design system v2 — Mullvad-style monochrome + single accent.
 *
 * Locked decisions (founder direction 2026-06-02):
 * - 95% greyscale palette, single accent for connected state
 * - Two status colors: amber warning, red error
 * - Generous spacing scale (4/8/12/16/24/32/48/64)
 * - SF Pro Display for headlines, SF Pro Text for body
 * - Radii 8/12/16/24
 * - Reference: Mullvad iOS app (https://mullvad.net/en/download/app/ios)
 *
 * Reference: mobile/ios/docs/ux-wireframes-v2.md
 */

import '@/global.css';

import { Platform } from 'react-native';

// ── Color palette ─────────────────────────────────────────────────
//
// Mullvad-inspired. 95% greyscale. Single accent for the connected
// state. Two status colors for warnings + errors. Avoid using accent
// for non-status surfaces (buttons stay greyscale; selection states
// use background shifts, not color).

const palette = {
  // Pure black/white anchors
  black: '#000000',
  white: '#FFFFFF',

  // Grayscale ramp (named by lightness, 50 = lightest)
  gray50: '#FAFAFA',
  gray100: '#F4F4F5',
  gray200: '#E4E4E7',
  gray300: '#D4D4D8',
  gray400: '#A1A1AA',
  gray500: '#71717A',
  gray600: '#52525B',
  gray700: '#3F3F46',
  gray800: '#27272A',
  gray900: '#18181B',
  gray950: '#0A0A0B',

  // Single accent — used ONLY for connected state ring + balance
  // tickers when actively burning $GRID
  accentGreen: '#34C759', // SF green — matches iOS system "connected"
  accentGreenDim: '#1F7A3C',

  // Status palette — strictly for state communication, never decoration
  warnAmber: '#FF9F0A', // SF orange — low balance, slow speed
  warnAmberDim: '#9A5C00',
  errorRed: '#FF453A', // SF red — disconnect button, error states
  errorRedDim: '#9A1F1A',
} as const;

// ── Semantic color tokens ─────────────────────────────────────────
//
// Components reference these (not raw palette) so a Mullvad-style
// monochrome theme stays consistent. Each token has a light + dark
// variant; `useTheme()` selects based on `useColorScheme()`.

export const Colors = {
  light: {
    // Surfaces
    background: palette.white,
    backgroundElevated: palette.gray50,
    backgroundCard: palette.white,
    backgroundElement: palette.gray100,
    backgroundSelected: palette.gray200,
    backgroundOverlay: 'rgba(0,0,0,0.4)',

    // Text
    text: palette.gray900,
    textSecondary: palette.gray600,
    textTertiary: palette.gray400,
    textInverse: palette.white,

    // Borders & dividers
    border: palette.gray200,
    borderSubtle: palette.gray100,
    borderStrong: palette.gray300,

    // States — used ONLY for state communication
    accent: palette.accentGreen,
    accentDim: palette.accentGreenDim,
    warning: palette.warnAmber,
    warningDim: palette.warnAmberDim,
    error: palette.errorRed,
    errorDim: palette.errorRedDim,

    // Connect button states
    ringOff: palette.gray400,
    ringConnecting: palette.gray700, // sweep arc color
    ringConnected: palette.accentGreen,
    // Disc fills behind the ring — give the control body so it reads as
    // a button, not a hollow outline (#684 Mullvad-quality pass).
    connectFill: palette.gray100,
    connectFillConnected: 'rgba(52, 199, 89, 0.10)',
  },
  dark: {
    // Surfaces — Mullvad uses a near-black not pure black, for OLED comfort
    background: palette.gray950,
    backgroundElevated: palette.gray900,
    backgroundCard: palette.gray900,
    backgroundElement: palette.gray800,
    backgroundSelected: palette.gray700,
    backgroundOverlay: 'rgba(0,0,0,0.7)',

    // Text
    text: palette.white,
    textSecondary: palette.gray400,
    textTertiary: palette.gray500,
    textInverse: palette.gray950,

    // Borders & dividers
    border: palette.gray800,
    borderSubtle: palette.gray900,
    borderStrong: palette.gray700,

    // States
    accent: palette.accentGreen,
    accentDim: palette.accentGreenDim,
    warning: palette.warnAmber,
    warningDim: palette.warnAmberDim,
    error: palette.errorRed,
    errorDim: palette.errorRedDim,

    // Connect button states
    ringOff: palette.gray600,
    ringConnecting: palette.gray400,
    ringConnected: palette.accentGreen,
    // Disc fills behind the ring (see light-scheme note).
    connectFill: palette.gray800,
    connectFillConnected: 'rgba(52, 199, 89, 0.16)',
  },
} as const;

export type ThemeColor = keyof typeof Colors.light & keyof typeof Colors.dark;

// ── Typography ────────────────────────────────────────────────────
//
// SF Pro Display for headlines (28pt+), SF Pro Text for body (≤17pt).
// iOS auto-substitutes the right cut based on point size when using
// `system-ui`, so we don't need to register the fonts separately.

export const Fonts = Platform.select({
  ios: {
    sans: 'system-ui', // SF Pro (Display/Text auto-selected by size)
    serif: 'ui-serif',
    rounded: 'ui-rounded', // SF Pro Rounded (for the big circular button label)
    mono: 'ui-monospace', // SF Mono (for $GRID amounts + egress IP)
  },
  default: {
    sans: 'normal',
    serif: 'serif',
    rounded: 'normal',
    mono: 'monospace',
  },
  web: {
    sans: 'var(--font-display)',
    serif: 'var(--font-serif)',
    rounded: 'var(--font-rounded)',
    mono: 'var(--font-mono)',
  },
});

// ── Type scale ────────────────────────────────────────────────────
//
// Component variants reference these by name (not raw values). Pair
// fontSize with the right family above. Letter-spacing matters for
// the all-caps status labels (DISCONNECTED, CONNECTING, CONNECTED).

export const TypeScale = {
  displayL: { fontSize: 32, lineHeight: 40, fontWeight: '600' as const, letterSpacing: -0.4 },
  displayM: { fontSize: 28, lineHeight: 36, fontWeight: '600' as const, letterSpacing: -0.3 },
  displayS: { fontSize: 22, lineHeight: 28, fontWeight: '600' as const, letterSpacing: -0.2 },

  bodyL: { fontSize: 17, lineHeight: 24, fontWeight: '400' as const, letterSpacing: 0 },
  bodyM: { fontSize: 15, lineHeight: 20, fontWeight: '400' as const, letterSpacing: 0 },
  bodyS: { fontSize: 13, lineHeight: 18, fontWeight: '400' as const, letterSpacing: 0 },

  caption: { fontSize: 12, lineHeight: 16, fontWeight: '400' as const, letterSpacing: 0.2 },
  captionStrong: { fontSize: 12, lineHeight: 16, fontWeight: '600' as const, letterSpacing: 0.2 },

  // Status labels — all-caps, letter-spaced (Mullvad pattern)
  statusLabel: { fontSize: 14, lineHeight: 18, fontWeight: '600' as const, letterSpacing: 2 },

  // Monospace — $GRID amounts, egress IPs, account fragments
  monoL: { fontSize: 24, lineHeight: 32, fontWeight: '500' as const, letterSpacing: 1 },
  monoM: { fontSize: 17, lineHeight: 24, fontWeight: '500' as const, letterSpacing: 0.5 },
  monoS: { fontSize: 14, lineHeight: 20, fontWeight: '500' as const, letterSpacing: 0.5 },

  // Button labels
  button: { fontSize: 17, lineHeight: 22, fontWeight: '600' as const, letterSpacing: 0 },
  buttonSmall: { fontSize: 14, lineHeight: 18, fontWeight: '600' as const, letterSpacing: 0 },
} as const;

export type TypeVariant = keyof typeof TypeScale;

// ── Spacing scale ─────────────────────────────────────────────────
//
// Mullvad-grade spacing means generous gaps. Section gap of 24pt is
// the default; tight stacks use 8pt; element padding uses 16pt.
//
// Backwards-compat: old (half/one/two/three/four/five/six) keys
// preserved so existing components don't break during migration.

export const Spacing = {
  // New named scale (use this in new code)
  xs: 4,
  sm: 8,
  md: 12,
  lg: 16,
  xl: 24,
  xxl: 32,
  xxxl: 48,
  xxxxl: 64,

  // Legacy aliases (old code references these — DO NOT BREAK)
  half: 2,
  one: 4,
  two: 8,
  three: 16,
  four: 24,
  five: 32,
  six: 64,
} as const;

// ── Corner radii ──────────────────────────────────────────────────
//
// Cards 16pt, buttons 12pt, chips 8pt, big surfaces 24pt. The
// circular connect button is a special case — its radius equals
// half its diameter (set inline as `borderRadius: ConnectButton.size / 2`).

export const Radii = {
  none: 0,
  sm: 8,
  md: 12,
  lg: 16,
  xl: 24,
  full: 9999,
} as const;

// ── Connect button sizing ─────────────────────────────────────────
//
// The single most distinctive UI element. Mullvad uses a giant
// circular button as the primary affordance. iogrid matches:
// 180pt diameter, centered, with a thick stroke ring that changes
// color based on state.

export const ConnectButton = {
  size: 180, // diameter
  ringStrokeWidth: 6,
  tapTargetExpansion: 12, // hitSlop, so the actual tap target is ≥204pt
} as const;

// ── Card sizing ───────────────────────────────────────────────────

export const Card = {
  padding: Spacing.lg, // 16pt inside the card
  gap: Spacing.md, // 12pt between rows inside a card
  marginVertical: Spacing.sm, // 8pt between stacked cards
} as const;

// ── Misc ──────────────────────────────────────────────────────────

export const BottomTabInset = Platform.select({ ios: 50, android: 80 }) ?? 0;
export const MaxContentWidth = 800;

// ── Animation durations ───────────────────────────────────────────
//
// Mullvad-style: animations are subtle, never over-celebratory.
// 200ms for state changes, 1000ms for the rotating arc.

export const Motion = {
  fast: 150,
  base: 200,
  slow: 350,

  // The connecting-state arc rotation — one full revolution per
  // second matches the visual rhythm of a heartbeat-like ambient.
  connectingArcRotation: 1000,
} as const;
