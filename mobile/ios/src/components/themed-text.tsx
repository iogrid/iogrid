import { Platform, StyleSheet, Text, type TextProps } from 'react-native';

import { Fonts, ThemeColor, TypeScale, type TypeVariant } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';

/**
 * v2 design system variants (#589):
 *   - display-l / display-m / display-s
 *   - body-l / body-m / body-s
 *   - caption
 *   - mono
 *
 * Legacy variants (`default`, `title`, `small`, etc.) are mapped to v2
 * variants so the existing call-sites keep typechecking during the
 * cut-over.
 */
export type ThemedTextVariant =
  | 'display-l'
  | 'display-m'
  | 'display-s'
  | 'body-l'
  | 'body-m'
  | 'body-s'
  | 'caption'
  | 'mono'
  | 'default'
  | 'title'
  | 'small'
  | 'smallBold'
  | 'subtitle'
  | 'link'
  | 'linkPrimary'
  | 'code';

export type ThemedTextProps = TextProps & {
  type?: ThemedTextVariant;
  themeColor?: ThemeColor;
  /** Override the resolved color with an explicit value. */
  color?: string;
};

// Map any variant (v2 or legacy) to a TypeScale token in the upstream
// design system. The TypeScale uses camelCase keys (bodyL/etc.).
const variantToScale: Record<ThemedTextVariant, TypeVariant> = {
  'display-l': 'displayL',
  'display-m': 'displayM',
  'display-s': 'displayS',
  'body-l': 'bodyL',
  'body-m': 'bodyM',
  'body-s': 'bodyS',
  caption: 'caption',
  mono: 'monoS',
  // Legacy aliases
  default: 'bodyL',
  title: 'displayL',
  small: 'bodyS',
  smallBold: 'bodyS',
  subtitle: 'displayM',
  link: 'bodyS',
  linkPrimary: 'bodyS',
  code: 'monoS',
};

export function ThemedText({
  style,
  type = 'body-m',
  themeColor,
  color,
  ...rest
}: ThemedTextProps) {
  const theme = useTheme();
  const scaleKey = variantToScale[type] ?? 'bodyM';
  const baseStyle = TypeScale[scaleKey];

  return (
    <Text
      style={[
        { color: color ?? theme[themeColor ?? 'text'] },
        baseStyle,
        type === 'smallBold' && styles.smallBold,
        type === 'link' && styles.link,
        type === 'linkPrimary' && [styles.link, { color: theme.accent }],
        (type === 'code' || type === 'mono') && styles.mono,
        style,
      ]}
      {...rest}
    />
  );
}

const styles = StyleSheet.create({
  smallBold: { fontWeight: '700' },
  link: { textDecorationLine: 'underline' },
  mono: {
    fontFamily: Fonts.mono,
    fontWeight: Platform.select({ android: '700' }) ?? '500',
  },
});
