/**
 * Card — section card primitive used across iogrid v2 surfaces.
 *
 * Two variants:
 *   - <Card>    plain container with theme-aware bg + hairline border
 *   - <PressableCard> same shape, tap feedback, ARIA button role
 *
 * Both honor:
 *   - Theme tokens (backgroundCard + border from constants/theme.ts)
 *   - 16pt corner radius (Card.padding lives in tokens too)
 *   - Optional `tone` prop for warning/error variants (used by the
 *     low-balance banner on the wallet card)
 *
 * Refs #580, #589.
 */

import { forwardRef } from 'react';
import {
  Pressable,
  StyleSheet,
  View,
  type GestureResponderEvent,
  type PressableProps,
  type ViewProps,
} from 'react-native';

import { Card as CardTokens, Radii } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';

export type CardTone = 'default' | 'warning' | 'error';

interface CardProps extends ViewProps {
  tone?: CardTone;
  /** Disable padding for cards that want edge-to-edge children (e.g.
   *  the settings SectionGroup which renders its own row dividers). */
  noPadding?: boolean;
}

interface PressableCardProps extends Omit<PressableProps, 'style'> {
  tone?: CardTone;
  noPadding?: boolean;
  style?: PressableProps['style'];
  onPress: (e: GestureResponderEvent) => void;
}

function resolveToneColors(theme: ReturnType<typeof useTheme>, tone: CardTone) {
  switch (tone) {
    case 'warning':
      return { background: theme.backgroundCard, border: theme.warning };
    case 'error':
      return { background: theme.backgroundCard, border: theme.error };
    default:
      return { background: theme.backgroundCard, border: theme.border };
  }
}

export const Card = forwardRef<View, CardProps>(function Card(
  { tone = 'default', noPadding, style, children, ...rest }: CardProps,
  ref,
) {
  const theme = useTheme();
  const { background, border } = resolveToneColors(theme, tone);
  return (
    <View
      ref={ref}
      {...rest}
      style={[
        styles.card,
        { backgroundColor: background, borderColor: border },
        noPadding ? styles.noPadding : null,
        style,
      ]}
    >
      {children}
    </View>
  );
});

export function PressableCard({
  tone = 'default',
  noPadding,
  style,
  onPress,
  children,
  ...rest
}: PressableCardProps) {
  const theme = useTheme();
  const { background, border } = resolveToneColors(theme, tone);
  return (
    <Pressable
      {...rest}
      onPress={onPress}
      accessibilityRole={rest.accessibilityRole ?? 'button'}
      style={({ pressed }) => [
        styles.card,
        { backgroundColor: background, borderColor: border },
        noPadding ? styles.noPadding : null,
        pressed ? styles.pressed : null,
        typeof style === 'function' ? style({ pressed }) : style,
      ]}
    >
      {children}
    </Pressable>
  );
}

const styles = StyleSheet.create({
  card: {
    padding: CardTokens.padding,
    borderRadius: Radii.lg,
    borderWidth: StyleSheet.hairlineWidth,
    marginTop: CardTokens.marginVertical,
  },
  noPadding: {
    padding: 0,
  },
  pressed: {
    opacity: 0.7,
  },
});
