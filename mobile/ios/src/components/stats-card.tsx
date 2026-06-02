/**
 * StatsCard — live traffic + latency surface shown when CONNECTED.
 *
 * Per mobile/ios/docs/ux-wireframes-v2.md Screen 7. Renders received/
 * sent byte counters, speed (Mbps), latency (ms), and an optional
 * egress IP with tap-to-copy. Numbers use SF Mono for tabular feel.
 *
 * Driven by props; the parent screen subscribes to
 * TunnelControl.onStatsUpdate and refreshes on each tick.
 *
 * Refs #580, #591.
 */

import { useState } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import * as Clipboard from 'expo-clipboard';

import { ThemedText } from '@/components/themed-text';
import { Card, Spacing, TypeScale } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';

interface Props {
  sentBytes: number;
  receivedBytes: number;
  /** Megabits per second (instantaneous), or null if not available yet. */
  speedMbps?: number | null;
  /** Latency in ms (peer RTT), or null. */
  latencyMs?: number | null;
  /** Optional egress IP (tap to copy). */
  egressIP?: string | null;
  /** Optional flag emoji + city for the header line. */
  flag?: string | null;
  city?: string | null;
  testID?: string;
}

export function StatsCard({
  sentBytes,
  receivedBytes,
  speedMbps,
  latencyMs,
  egressIP,
  flag,
  city,
  testID = 'stats-card',
}: Props) {
  const theme = useTheme();
  const [copied, setCopied] = useState(false);

  const copyIP = async () => {
    if (!egressIP) return;
    try {
      await Clipboard.setStringAsync(egressIP);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // ignore
    }
  };

  return (
    <View
      testID={testID}
      style={[
        styles.card,
        { backgroundColor: theme.backgroundCard, borderColor: theme.border },
      ]}
    >
      {/* ── Optional location header ─────────────────────────── */}
      {(flag || city) ? (
        <View style={styles.locationRow}>
          {flag ? <ThemedText style={styles.flag}>{flag}</ThemedText> : null}
          {city ? (
            <ThemedText
              testID="stats-card-city"
              style={[styles.city, { color: theme.text }]}
            >
              {city}
            </ThemedText>
          ) : null}
        </View>
      ) : null}

      {/* ── Egress IP (tap to copy) ──────────────────────────── */}
      {egressIP ? (
        <Pressable
          testID="stats-card-egress-ip"
          onPress={copyIP}
          accessibilityLabel={`Copy egress IP ${egressIP}`}
          accessibilityRole="button"
          hitSlop={8}
          style={({ pressed }) => [styles.ipRow, pressed ? { opacity: 0.7 } : null]}
        >
          <ThemedText style={[styles.ip, { color: theme.textSecondary }]}>
            {egressIP}
          </ThemedText>
          <ThemedText style={[styles.copyHint, { color: theme.textTertiary }]}>
            {copied ? 'COPIED' : 'TAP TO COPY'}
          </ThemedText>
        </Pressable>
      ) : null}

      {/* ── Stats grid ───────────────────────────────────────── */}
      <View style={styles.statsGrid}>
        <Stat
          theme={theme}
          label="Received"
          value={formatBytes(receivedBytes)}
          arrow="↓"
        />
        <Stat
          theme={theme}
          label="Sent"
          value={formatBytes(sentBytes)}
          arrow="↑"
        />
      </View>

      {(speedMbps != null || latencyMs != null) ? (
        <View style={styles.metaRow}>
          {speedMbps != null ? (
            <Meta theme={theme} label="Speed" value={`${speedMbps} Mbps`} />
          ) : null}
          {latencyMs != null ? (
            <Meta theme={theme} label="Latency" value={`${latencyMs} ms`} />
          ) : null}
        </View>
      ) : null}
    </View>
  );
}

// ── Sub-components ───────────────────────────────────────────────

function Stat({
  theme,
  label,
  value,
  arrow,
}: {
  theme: ReturnType<typeof useTheme>;
  label: string;
  value: string;
  arrow: string;
}) {
  return (
    <View style={styles.statCol}>
      <ThemedText style={[styles.statLabel, { color: theme.textTertiary }]}>
        {label.toUpperCase()}
      </ThemedText>
      <ThemedText style={[styles.statValue, { color: theme.text }]}>
        <ThemedText style={[styles.arrow, { color: theme.textSecondary }]}>
          {arrow}{' '}
        </ThemedText>
        {value}
      </ThemedText>
    </View>
  );
}

function Meta({
  theme,
  label,
  value,
}: {
  theme: ReturnType<typeof useTheme>;
  label: string;
  value: string;
}) {
  return (
    <View style={styles.metaCol}>
      <ThemedText style={[styles.metaLabel, { color: theme.textTertiary }]}>
        {label}
      </ThemedText>
      <ThemedText style={[styles.metaValue, { color: theme.textSecondary }]}>
        {value}
      </ThemedText>
    </View>
  );
}

// ── Helpers ──────────────────────────────────────────────────────

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

// ── Styles ───────────────────────────────────────────────────────

const styles = StyleSheet.create({
  card: {
    padding: Card.padding,
    borderRadius: 16,
    borderWidth: StyleSheet.hairlineWidth,
    marginTop: Card.marginVertical,
    gap: Spacing.md,
  },
  locationRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: Spacing.sm,
  },
  flag: {
    fontSize: 22,
  },
  city: {
    ...TypeScale.bodyL,
    fontWeight: '600',
  },
  ipRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
  },
  ip: {
    ...TypeScale.monoM,
  },
  copyHint: {
    ...TypeScale.caption,
    letterSpacing: 1,
  },
  statsGrid: {
    flexDirection: 'row',
    gap: Spacing.xxl,
  },
  statCol: {
    flex: 1,
    gap: 2,
  },
  statLabel: {
    ...TypeScale.captionStrong,
    letterSpacing: 1.5,
  },
  statValue: {
    ...TypeScale.monoL,
  },
  arrow: {
    ...TypeScale.bodyL,
  },
  metaRow: {
    flexDirection: 'row',
    gap: Spacing.xxl,
    paddingTop: Spacing.sm,
    borderTopWidth: 0,
  },
  metaCol: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'baseline',
    gap: Spacing.sm,
  },
  metaLabel: {
    ...TypeScale.bodyS,
  },
  metaValue: {
    ...TypeScale.monoS,
  },
});
