/**
 * RegionRow + CityRow — primitives extracted from the region-picker
 * SectionList. Standalone so they're reusable (e.g. main-screen
 * "current region" card may want the same flag + city + ping
 * affordance later).
 *
 * Refs #580, #592.
 */

import { Pressable, StyleSheet, View } from 'react-native';

import { ThemedText } from '@/components/themed-text';
import { Card, Spacing, TypeScale } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';

interface RegionRowProps {
  testID?: string;
  flag: string;
  name: string;
  /** Optional second line (e.g. '3 cities • 12 ms'). */
  subtitle?: string;
  expanded?: boolean;
  selected?: boolean;
  onPress?: () => void;
  /** When true, drop the bottom border (last row in a group). */
  last?: boolean;
}

export function RegionRow({
  testID,
  flag,
  name,
  subtitle,
  expanded,
  selected,
  onPress,
  last,
}: RegionRowProps) {
  const theme = useTheme();
  return (
    <Pressable
      testID={testID}
      onPress={onPress}
      accessibilityLabel={`${name}${subtitle ? `, ${subtitle}` : ''}`}
      accessibilityRole="button"
      accessibilityState={{ expanded: !!expanded, selected: !!selected }}
      style={({ pressed }) => [
        styles.row,
        {
          borderBottomColor: theme.border,
          borderBottomWidth: last ? 0 : StyleSheet.hairlineWidth,
        },
        pressed ? { opacity: 0.7 } : null,
      ]}
    >
      <ThemedText style={styles.flag}>{flag}</ThemedText>
      <View style={styles.text}>
        <ThemedText style={[styles.name, { color: theme.text }]}>{name}</ThemedText>
        {subtitle ? (
          <ThemedText style={[styles.subtitle, { color: theme.textSecondary }]}>
            {subtitle}
          </ThemedText>
        ) : null}
      </View>
      {selected ? (
        <ThemedText style={[styles.checkmark, { color: theme.accent }]}>✓</ThemedText>
      ) : (
        <ThemedText
          style={[
            styles.chevron,
            { color: theme.textTertiary },
            expanded ? { transform: [{ rotate: '90deg' }] } : null,
          ]}
        >
          ›
        </ThemedText>
      )}
    </Pressable>
  );
}

interface CityRowProps {
  testID?: string;
  city: string;
  /** e.g. '5 of 8 online · 23 ms'. */
  subtitle?: string;
  selected?: boolean;
  onPress?: () => void;
  last?: boolean;
}

export function CityRow({ testID, city, subtitle, selected, onPress, last }: CityRowProps) {
  const theme = useTheme();
  return (
    <Pressable
      testID={testID}
      onPress={onPress}
      accessibilityLabel={`${city}${subtitle ? `, ${subtitle}` : ''}`}
      accessibilityRole="button"
      accessibilityState={{ selected: !!selected }}
      style={({ pressed }) => [
        styles.cityRow,
        {
          borderBottomColor: theme.border,
          borderBottomWidth: last ? 0 : StyleSheet.hairlineWidth,
          backgroundColor: selected ? theme.backgroundSelected : 'transparent',
        },
        pressed ? { opacity: 0.7 } : null,
      ]}
    >
      <View style={styles.text}>
        <ThemedText style={[styles.cityName, { color: theme.text }]}>{city}</ThemedText>
        {subtitle ? (
          <ThemedText style={[styles.subtitle, { color: theme.textSecondary }]}>
            {subtitle}
          </ThemedText>
        ) : null}
      </View>
      {selected ? (
        <ThemedText style={[styles.checkmark, { color: theme.accent }]}>✓</ThemedText>
      ) : null}
    </Pressable>
  );
}

const styles = StyleSheet.create({
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: Card.padding,
    paddingVertical: Spacing.md,
    gap: Spacing.md,
    minHeight: 56,
  },
  flag: {
    fontSize: 24,
  },
  text: {
    flex: 1,
  },
  name: {
    ...TypeScale.bodyL,
    fontWeight: '500',
  },
  subtitle: {
    ...TypeScale.bodyS,
    marginTop: 2,
  },
  chevron: {
    fontSize: 22,
    fontWeight: '300',
  },
  checkmark: {
    fontSize: 20,
    fontWeight: '700',
  },
  cityRow: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: Card.padding,
    paddingLeft: Card.padding + 36 + Spacing.md,
    paddingVertical: Spacing.md,
    minHeight: 52,
  },
  cityName: {
    ...TypeScale.bodyL,
  },
});
