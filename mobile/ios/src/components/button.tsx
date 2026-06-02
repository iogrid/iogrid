// Button primitive — Linear/Vercel-adjacent monochrome shape (#589 DoD).
//
// Variants:
//   - primary       — solid black/white on contrasting bg, used for CTAs
//   - secondary     — bordered, neutral fill
//   - destructive   — error-tinted text on neutral background (Mullvad
//                     "Disconnect" + "Sign out" pattern — destructive
//                     should never be the loudest element on screen)
//   - ghost         — text-only, tight padding (inline actions)
//
// Sizes:
//   - sm (36pt minH), md (default 48pt), lg (56pt)
//
// All ≥36pt min tap target; default 48pt aligns with iOS HIG.

import { ReactNode } from 'react';
import {
  ActivityIndicator,
  Pressable,
  StyleSheet,
  View,
  ViewStyle,
  type PressableProps,
} from 'react-native';

import { Radii, Spacing, TypeScale } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';
import { ThemedText } from './themed-text';

export type ButtonVariant = 'primary' | 'secondary' | 'destructive' | 'ghost';
export type ButtonSize = 'sm' | 'md' | 'lg';

export interface ButtonProps extends Omit<PressableProps, 'style' | 'children'> {
  label: string;
  variant?: ButtonVariant;
  size?: ButtonSize;
  loading?: boolean;
  leading?: ReactNode;
  trailing?: ReactNode;
  fullWidth?: boolean;
  style?: ViewStyle | ViewStyle[];
}

export function Button({
  label,
  variant = 'primary',
  size = 'md',
  loading = false,
  leading,
  trailing,
  fullWidth = false,
  disabled,
  style,
  testID,
  ...rest
}: ButtonProps) {
  const theme = useTheme();
  const dims = SIZE[size];

  const surface =
    variant === 'primary'
      ? theme.text
      : variant === 'secondary'
        ? theme.backgroundElement
        : 'transparent';

  const labelColor =
    variant === 'primary'
      ? theme.textInverse
      : variant === 'destructive'
        ? theme.error
        : theme.text;

  const borderColor =
    variant === 'secondary' ? theme.border : 'transparent';

  return (
    <Pressable
      testID={testID}
      disabled={disabled || loading}
      accessibilityRole="button"
      accessibilityState={{ disabled: !!disabled, busy: !!loading }}
      style={({ pressed }) => [
        styles.base,
        {
          backgroundColor: surface,
          borderColor,
          borderWidth: variant === 'secondary' ? 1 : 0,
          opacity: disabled ? 0.5 : pressed ? 0.85 : 1,
          paddingVertical: dims.padV,
          paddingHorizontal: dims.padH,
          minHeight: dims.minH,
        },
        fullWidth && styles.fullWidth,
        ...(Array.isArray(style) ? style : style ? [style] : []),
      ]}
      {...rest}
    >
      <View style={styles.row}>
        {loading ? (
          <ActivityIndicator size="small" color={labelColor} style={styles.spinner} />
        ) : leading ? (
          <View style={styles.adornment}>{leading}</View>
        ) : null}
        <ThemedText
          type={size === 'sm' ? 'body-s' : 'body-m'}
          color={labelColor}
          style={styles.label}
        >
          {label}
        </ThemedText>
        {trailing ? <View style={styles.adornment}>{trailing}</View> : null}
      </View>
    </Pressable>
  );
}

const SIZE = {
  sm: { padV: Spacing.sm, padH: Spacing.lg, minH: 36 },
  md: { padV: Spacing.md, padH: Spacing.xl, minH: 48 },
  lg: { padV: Spacing.lg, padH: Spacing.xxl, minH: 56 },
} as const;

const styles = StyleSheet.create({
  base: {
    borderRadius: Radii.md,
    alignItems: 'center',
    justifyContent: 'center',
  },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  fullWidth: { alignSelf: 'stretch' },
  label: { ...TypeScale.button },
  adornment: { justifyContent: 'center', alignItems: 'center' },
  spinner: { marginRight: 4 },
});
