/**
 * SettingsRow + SettingsToggleRow — primitives for the settings
 * SectionList. Same Apple/Mullvad-grade row shape, both pressable
 * and pure-toggle variants.
 *
 * Refs #580, #593.
 */

import { Pressable, Switch, StyleSheet, View, type GestureResponderEvent } from 'react-native';

import { ThemedText } from '@/components/themed-text';
import { Card, Spacing, TypeScale } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';

interface RowProps {
  testID?: string;
  label: string;
  hint?: string;
  value?: string;
  chevron?: boolean;
  disabled?: boolean;
  destructive?: boolean;
  onPress?: (e: GestureResponderEvent) => void;
  last?: boolean;
}

export function SettingsRow({
  testID,
  label,
  hint,
  value,
  chevron,
  disabled,
  destructive,
  onPress,
  last,
}: RowProps) {
  const theme = useTheme();
  const isPressable = !!onPress && !disabled;

  const labelColor = destructive
    ? theme.error
    : disabled
      ? theme.textTertiary
      : theme.text;

  const inner = (
    <>
      <View style={styles.text}>
        <ThemedText style={[styles.label, { color: labelColor }]}>{label}</ThemedText>
        {hint ? (
          <ThemedText style={[styles.hint, { color: theme.textSecondary }]}>
            {hint}
          </ThemedText>
        ) : null}
      </View>
      <View style={styles.trailing}>
        {value ? (
          <ThemedText
            style={[
              styles.value,
              { color: disabled ? theme.textTertiary : theme.textSecondary },
            ]}
          >
            {value}
          </ThemedText>
        ) : null}
        {chevron ? (
          <ThemedText style={[styles.chevron, { color: theme.textTertiary }]}>›</ThemedText>
        ) : null}
      </View>
    </>
  );

  const baseStyle = [
    styles.row,
    {
      borderBottomColor: theme.border,
      borderBottomWidth: last ? 0 : StyleSheet.hairlineWidth,
    },
  ];

  if (isPressable) {
    return (
      <Pressable
        testID={testID}
        onPress={onPress}
        accessibilityLabel={label}
        accessibilityRole="button"
        style={({ pressed }) => [...baseStyle, pressed ? { opacity: 0.7 } : null]}
      >
        {inner}
      </Pressable>
    );
  }
  return (
    <View testID={testID} accessibilityLabel={label} style={baseStyle}>
      {inner}
    </View>
  );
}

interface ToggleRowProps {
  testID?: string;
  label: string;
  hint?: string;
  value: boolean;
  onChange: (v: boolean) => void;
  last?: boolean;
}

export function SettingsToggleRow({
  testID,
  label,
  hint,
  value,
  onChange,
  last,
}: ToggleRowProps) {
  const theme = useTheme();
  return (
    <View
      testID={testID}
      accessibilityLabel={`${label} ${value ? 'enabled' : 'disabled'}`}
      accessibilityRole="switch"
      style={[
        styles.row,
        {
          borderBottomColor: theme.border,
          borderBottomWidth: last ? 0 : StyleSheet.hairlineWidth,
        },
      ]}
    >
      <View style={styles.text}>
        <ThemedText style={[styles.label, { color: theme.text }]}>{label}</ThemedText>
        {hint ? (
          <ThemedText style={[styles.hint, { color: theme.textSecondary }]}>
            {hint}
          </ThemedText>
        ) : null}
      </View>
      <Switch
        value={value}
        onValueChange={onChange}
        trackColor={{ false: theme.border, true: theme.accent }}
        thumbColor={theme.background}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  row: {
    minHeight: 56,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: Card.padding,
    paddingVertical: Spacing.md,
  },
  text: {
    flex: 1,
    paddingRight: Spacing.md,
  },
  label: {
    ...TypeScale.bodyL,
    fontWeight: '500',
  },
  hint: {
    ...TypeScale.bodyS,
    marginTop: 2,
  },
  trailing: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: Spacing.sm,
  },
  value: {
    ...TypeScale.bodyM,
  },
  chevron: {
    fontSize: 22,
    fontWeight: '300',
  },
});
