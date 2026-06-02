// Settings row — left label + optional sublabel, right control (#593).
// 44pt min tap target per Apple HIG.

import type { ReactNode } from 'react';
import { Pressable, StyleSheet, Switch, View } from 'react-native';

import { ThemedText } from '@/components/themed-text';
import { Radii, Spacing } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';

export interface SettingsRowProps {
  testID?: string;
  label: string;
  sublabel?: string;
  /** Trailing right-side value text. */
  value?: string;
  toggle?: { value: boolean; onChange: (next: boolean) => void };
  destructive?: boolean;
  navigable?: boolean;
  onPress?: () => void;
  trailing?: ReactNode;
}

export function SettingsRow({
  testID,
  label,
  sublabel,
  value,
  toggle,
  destructive = false,
  navigable = false,
  onPress,
  trailing,
}: SettingsRowProps) {
  const theme = useTheme();
  const labelColor = destructive ? theme.error : theme.text;

  const inner = (
    <View
      style={[
        styles.row,
        { backgroundColor: theme.backgroundElement, borderRadius: Radii.md },
      ]}
    >
      <View style={styles.text}>
        <ThemedText type="body-m" color={labelColor} style={styles.label}>
          {label}
        </ThemedText>
        {sublabel ? (
          <ThemedText type="body-s" color={theme.textSecondary}>
            {sublabel}
          </ThemedText>
        ) : null}
      </View>
      <View style={styles.trailing}>
        {value ? (
          <ThemedText type="body-m" color={theme.textSecondary}>
            {value}
          </ThemedText>
        ) : null}
        {toggle ? (
          <Switch
            value={toggle.value}
            onValueChange={toggle.onChange}
            trackColor={{ true: theme.accent, false: theme.border }}
          />
        ) : null}
        {trailing ?? null}
        {navigable ? (
          <ThemedText type="body-m" color={theme.textSecondary}>
            {'›'}
          </ThemedText>
        ) : null}
      </View>
    </View>
  );

  if (onPress) {
    return (
      <Pressable
        testID={testID}
        accessibilityRole="button"
        onPress={onPress}
        style={({ pressed }) => [
          { borderRadius: Radii.md, opacity: pressed ? 0.85 : 1 },
        ]}
      >
        {inner}
      </Pressable>
    );
  }
  return (
    <View testID={testID} style={{ borderRadius: Radii.md }}>
      {inner}
    </View>
  );
}

const styles = StyleSheet.create({
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    minHeight: 44,
    paddingVertical: Spacing.md,
    paddingHorizontal: Spacing.lg,
    gap: Spacing.md,
  },
  text: { flex: 1, gap: 2 },
  label: { fontWeight: '500' },
  trailing: { flexDirection: 'row', alignItems: 'center', gap: Spacing.md },
});
