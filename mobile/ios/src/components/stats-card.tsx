// Stats card — live VPN stats (down/up bytes, speed, latency) rendered
// while CONNECTED. Receives the snapshot via props; the parent screen
// owns the IPC subscription and re-renders.

import { StyleSheet, View } from 'react-native';

import { SectionCard } from '@/components/section-card';
import { ThemedText } from '@/components/themed-text';
import { Spacing } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';

export interface TunnelStats {
  bytesIn: number;
  bytesOut: number;
  speedMbps: number;
  latencyMs: number;
}

export interface StatsCardProps {
  stats: TunnelStats;
  testID?: string;
}

export function StatsCard({ stats, testID }: StatsCardProps) {
  const theme = useTheme();
  return (
    <SectionCard testID={testID}>
      <View style={styles.row}>
        <View style={styles.cell}>
          <ThemedText type="caption" color={theme.textSecondary}>
            DOWN
          </ThemedText>
          <ThemedText type="body-l" style={styles.value}>
            {formatBytes(stats.bytesIn)}
          </ThemedText>
        </View>
        <View style={styles.cell}>
          <ThemedText type="caption" color={theme.textSecondary}>
            UP
          </ThemedText>
          <ThemedText type="body-l" style={styles.value}>
            {formatBytes(stats.bytesOut)}
          </ThemedText>
        </View>
      </View>
      <View
        style={[styles.row, styles.rowDivider, { borderTopColor: theme.borderSubtle }]}
      >
        <View style={styles.cell}>
          <ThemedText type="caption" color={theme.textSecondary}>
            SPEED
          </ThemedText>
          <ThemedText type="body-l" style={styles.value}>
            {stats.speedMbps.toFixed(1)} Mbps
          </ThemedText>
        </View>
        <View style={styles.cell}>
          <ThemedText type="caption" color={theme.textSecondary}>
            LATENCY
          </ThemedText>
          <ThemedText type="body-l" style={styles.value}>
            {stats.latencyMs} ms
          </ThemedText>
        </View>
      </View>
    </SectionCard>
  );
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MB`;
  return `${(n / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

const styles = StyleSheet.create({
  row: {
    flexDirection: 'row',
    paddingVertical: Spacing.sm,
  },
  rowDivider: {
    borderTopWidth: StyleSheet.hairlineWidth,
    marginTop: Spacing.sm,
    paddingTop: Spacing.md,
  },
  cell: {
    flex: 1,
    gap: 2,
  },
  value: {
    fontVariant: ['tabular-nums'],
    fontWeight: '600',
  },
});
