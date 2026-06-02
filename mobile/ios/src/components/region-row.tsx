// Region row — one country/region tile in the SectionList (#592).
// Tappable. Renders flag + name + sublabel ("N cities • Xms").
// Used both as the "Best (auto)" pinned tile and for live region rows.

import { StyleSheet, View } from 'react-native';

import { SectionCard } from '@/components/section-card';
import { ThemedText } from '@/components/themed-text';
import { Spacing } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';

export interface RegionRowProps {
  testID?: string;
  flag?: string;
  label: string;
  subtitle?: string;
  pingMs?: number;
  selected?: boolean;
  onPress: () => void;
  expandable?: boolean;
  expanded?: boolean;
}

export function RegionRow({
  testID,
  flag,
  label,
  subtitle,
  pingMs,
  selected = false,
  onPress,
  expandable = false,
  expanded = false,
}: RegionRowProps) {
  const theme = useTheme();
  const selectedStyle =
    selected ? { borderColor: theme.accent, borderWidth: 1 } : undefined;
  return (
    <SectionCard
      onPress={onPress}
      testID={testID}
      style={[styles.row, selectedStyle].filter(Boolean) as never}
    >
      <View style={styles.left}>
        {flag ? (
          <ThemedText type="display-s" style={styles.flag}>
            {flag}
          </ThemedText>
        ) : null}
        <View style={styles.text}>
          <ThemedText type="body-m" style={styles.label}>
            {label}
          </ThemedText>
          {subtitle ? (
            <ThemedText type="body-s" color={theme.textSecondary}>
              {subtitle}
            </ThemedText>
          ) : null}
        </View>
      </View>
      <View style={styles.right}>
        {pingMs !== undefined ? (
          <ThemedText type="body-s" color={theme.textSecondary} style={styles.ping}>
            {pingMs}ms
          </ThemedText>
        ) : null}
        {selected ? (
          <ThemedText type="body-m" color={theme.accent}>
            {'✓'}
          </ThemedText>
        ) : expandable ? (
          <ThemedText type="body-m" color={theme.textSecondary}>
            {expanded ? '⌄' : '›'}
          </ThemedText>
        ) : (
          <ThemedText type="body-m" color={theme.textSecondary}>
            {'›'}
          </ThemedText>
        )}
      </View>
    </SectionCard>
  );
}

const styles = StyleSheet.create({
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: Spacing.lg,
  },
  left: { flexDirection: 'row', alignItems: 'center', gap: Spacing.md, flex: 1 },
  text: { flex: 1, gap: 2 },
  right: { flexDirection: 'row', alignItems: 'center', gap: Spacing.md },
  flag: { fontSize: 24 },
  label: { fontWeight: '600' },
  ping: { fontVariant: ['tabular-nums'] },
});
