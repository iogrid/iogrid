// SectionCard — generic section container primitive (#589 DoD).
//
// Named `SectionCard` to avoid the export-name collision with the
// `Card` sizing token in @/constants/theme. Linear/Vercel pattern:
// subtle background tint, no hard shadow, generous inner padding.
// Tappable cards expose `onPress` and apply a pressed state.

import type { ReactNode } from 'react';
import { Pressable, View, ViewStyle } from 'react-native';

import { Card as CardTokens, Radii } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';

export interface SectionCardProps {
  children: ReactNode;
  onPress?: () => void;
  bare?: boolean;
  transparent?: boolean;
  style?: ViewStyle | ViewStyle[];
  testID?: string;
  accessibilityLabel?: string;
}

export function SectionCard({
  children,
  onPress,
  bare = false,
  transparent = false,
  style,
  testID,
  accessibilityLabel,
}: SectionCardProps) {
  const theme = useTheme();
  const innerStyle: ViewStyle = {
    backgroundColor: transparent ? 'transparent' : theme.backgroundElement,
    borderRadius: Radii.lg,
    padding: bare ? 0 : CardTokens.padding,
  };

  const composed: ViewStyle[] = [
    innerStyle,
    ...(Array.isArray(style) ? style : style ? [style] : []),
  ];

  if (onPress) {
    return (
      <Pressable
        testID={testID}
        accessibilityRole="button"
        accessibilityLabel={accessibilityLabel}
        onPress={onPress}
        style={({ pressed }) => [
          ...composed,
          pressed && {
            backgroundColor: transparent ? 'transparent' : theme.backgroundSelected,
          },
        ]}
      >
        {children}
      </Pressable>
    );
  }
  return (
    <View testID={testID} accessibilityLabel={accessibilityLabel} style={composed}>
      {children}
    </View>
  );
}
