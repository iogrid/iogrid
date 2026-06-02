/**
 * ThemedText — v2-aware text primitive.
 *
 * Supports two prop styles for migration safety:
 *
 *   - LEGACY `type`: default / title / small / smallBold / subtitle /
 *     link / linkPrimary / code. Preserved so old v1 components keep
 *     compiling unchanged.
 *
 *   - NEW v2 `type` (TypeScale): display-l/m/s, body-l/m/s, caption,
 *     caption-strong, status-label, mono-l/m/s, button, button-small.
 *     Pulls directly from `TypeScale` tokens in constants/theme.ts.
 *
 *   - `color`: shorthand for explicit color override (used by Button
 *     and other primitives that need to override the inherited text
 *     color per-variant).
 *
 *   - `themeColor`: legacy hook into the Colors token bag.
 *
 * Refs #580, #589.
 */

import { Platform, StyleSheet, Text, type TextProps } from 'react-native';

import { Fonts, ThemeColor, TypeScale, type TypeVariant } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';

type LegacyType =
  | 'default'
  | 'title'
  | 'small'
  | 'smallBold'
  | 'subtitle'
  | 'link'
  | 'linkPrimary'
  | 'code';

// Maps v2 friendly names ('body-l', 'display-m') to the camelCased
// TypeScale keys ('bodyL', 'displayM') so consumers can write the
// kebab-case form most React developers expect on a `type` prop.
const V2_TYPE_MAP: Record<string, TypeVariant> = {
  'display-l': 'displayL',
  'display-m': 'displayM',
  'display-s': 'displayS',
  'body-l': 'bodyL',
  'body-m': 'bodyM',
  'body-s': 'bodyS',
  caption: 'caption',
  'caption-strong': 'captionStrong',
  'status-label': 'statusLabel',
  'mono-l': 'monoL',
  'mono-m': 'monoM',
  'mono-s': 'monoS',
  button: 'button',
  'button-small': 'buttonSmall',
};

type V2Type = keyof typeof V2_TYPE_MAP;

export type ThemedTextProps = TextProps & {
  type?: LegacyType | V2Type;
  themeColor?: ThemeColor;
  color?: string;
};

export function ThemedText({
  style,
  type = 'default',
  themeColor,
  color,
  ...rest
}: ThemedTextProps) {
  const theme = useTheme();
  const resolvedColor = color ?? theme[themeColor ?? 'text'];

  // v2 variant?
  if (type in V2_TYPE_MAP) {
    const tsKey = V2_TYPE_MAP[type as V2Type];
    return (
      <Text style={[{ color: resolvedColor }, TypeScale[tsKey], style]} {...rest} />
    );
  }

  // Legacy variant
  const legacyType = type as LegacyType;
  return (
    <Text
      style={[
        { color: resolvedColor },
        legacyType === 'default' && styles.default,
        legacyType === 'title' && styles.title,
        legacyType === 'small' && styles.small,
        legacyType === 'smallBold' && styles.smallBold,
        legacyType === 'subtitle' && styles.subtitle,
        legacyType === 'link' && styles.link,
        legacyType === 'linkPrimary' && styles.linkPrimary,
        legacyType === 'code' && styles.code,
        style,
      ]}
      {...rest}
    />
  );
}

const styles = StyleSheet.create({
  small: { fontSize: 14, lineHeight: 20, fontWeight: '500' },
  smallBold: { fontSize: 14, lineHeight: 20, fontWeight: '700' },
  default: { fontSize: 16, lineHeight: 24, fontWeight: '500' },
  title: { fontSize: 48, fontWeight: '600', lineHeight: 52 },
  subtitle: { fontSize: 32, lineHeight: 44, fontWeight: '600' },
  link: { lineHeight: 30, fontSize: 14 },
  linkPrimary: { lineHeight: 30, fontSize: 14, color: '#3c87f7' },
  code: {
    fontFamily: Fonts.mono,
    fontWeight: Platform.select({ android: '700' as const }) ?? '500',
    fontSize: 12,
  },
});
