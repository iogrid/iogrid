/**
 * WalletCard — $GRID balance surface.
 *
 * Per mobile/ios/docs/ux-wireframes-v2.md Screens 5 & 7. Shows:
 *   - Bound wallet provider (Phantom / Ping / not connected)
 *   - $GRID balance + optional USD equivalent
 *   - Burn ticker when CONNECTED (live spend rate)
 *   - Top up CTA → routes to /topup
 *
 * Track 2 PR #602 wires the real `useGridBalance` hook + wallet
 * provider state. This primitive accepts both as props so it stays
 * usable for both the connected/disconnected states and for testing.
 *
 * Refs #580, #594.
 */

import { Pressable, StyleSheet, View } from 'react-native';
import { router } from 'expo-router';

import { ThemedText } from '@/components/themed-text';
import { Card, Radii, Spacing, TypeScale } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';

export type WalletProvider = 'phantom' | 'ping' | null;

interface Props {
  /** Which wallet the user has bound. Null = not connected yet. */
  provider: WalletProvider;
  /** $GRID balance as decimal. Null = unknown / loading. */
  balanceGrid: number | null;
  /** USD equivalent (optional, may be hidden if no oracle). */
  usdEquivalent?: number | null;
  /** When tunnel is up, show a live burn-rate ticker. */
  isBurning?: boolean;
  /** $GRID per minute burn (only shown when isBurning). */
  burnRateGridPerMin?: number | null;
  /** Manual refresh trigger (e.g. for post-topup balance re-poll). */
  onRefresh?: () => void;
  /** Override the default /topup navigation. */
  onTopUp?: () => void;
  testID?: string;
}

export function WalletCard({
  provider,
  balanceGrid,
  usdEquivalent,
  isBurning,
  burnRateGridPerMin,
  onRefresh,
  onTopUp,
  testID = 'wallet-card',
}: Props) {
  const theme = useTheme();
  const lowBalance =
    balanceGrid != null && balanceGrid > 0 && balanceGrid < 0.001;
  const empty = balanceGrid != null && balanceGrid === 0;

  const handleTopUp = onTopUp ?? (() => router.push('/topup'));

  return (
    <View
      testID={testID}
      style={[
        styles.card,
        {
          backgroundColor: theme.backgroundCard,
          borderColor: lowBalance || empty ? theme.warning : theme.border,
        },
      ]}
    >
      {/* ── Header: provider + refresh ────────────────────────── */}
      <View style={styles.headerRow}>
        <ThemedText style={[styles.headerLabel, { color: theme.textTertiary }]}>
          WALLET
        </ThemedText>
        {onRefresh ? (
          <Pressable
            testID={`${testID}-refresh`}
            onPress={onRefresh}
            hitSlop={12}
            accessibilityLabel="Refresh balance"
            accessibilityRole="button"
          >
            <ThemedText style={[styles.refresh, { color: theme.textSecondary }]}>
              ↻
            </ThemedText>
          </Pressable>
        ) : null}
      </View>

      {/* ── Provider line ─────────────────────────────────────── */}
      <ThemedText
        testID={`${testID}-provider`}
        style={[styles.provider, { color: theme.textSecondary }]}
      >
        {provider === 'phantom'
          ? 'Phantom · Solana'
          : provider === 'ping'
            ? 'Ping wallet'
            : 'Not connected'}
      </ThemedText>

      {/* ── Balance ──────────────────────────────────────────── */}
      <ThemedText
        testID={`${testID}-balance`}
        style={[styles.balance, { color: theme.text }]}
      >
        {balanceGrid != null ? `${formatGrid(balanceGrid)} $GRID` : '— $GRID'}
      </ThemedText>

      {usdEquivalent != null && balanceGrid != null ? (
        <ThemedText style={[styles.usd, { color: theme.textTertiary }]}>
          ≈ ${usdEquivalent.toFixed(2)}
        </ThemedText>
      ) : null}

      {/* ── Burn ticker (CONNECTED only) ──────────────────────── */}
      {isBurning && burnRateGridPerMin != null ? (
        <ThemedText
          testID={`${testID}-burn`}
          style={[styles.burn, { color: theme.accent }]}
        >
          –{formatGrid(burnRateGridPerMin)} $GRID/min
        </ThemedText>
      ) : null}

      {/* ── Low-balance banner ────────────────────────────────── */}
      {(lowBalance || empty) ? (
        <ThemedText
          testID={`${testID}-low-balance`}
          style={[styles.lowBalanceMsg, { color: theme.warning }]}
        >
          {empty ? 'Top up to connect' : 'Low balance — top up soon'}
        </ThemedText>
      ) : null}

      {/* ── Top up CTA ────────────────────────────────────────── */}
      <Pressable
        testID={`${testID}-topup`}
        onPress={handleTopUp}
        style={({ pressed }) => [
          styles.topupButton,
          { backgroundColor: theme.backgroundElement },
          pressed ? { opacity: 0.7 } : null,
        ]}
        accessibilityLabel="Top up wallet"
        accessibilityRole="button"
      >
        <ThemedText style={[styles.topupLabel, { color: theme.text }]}>
          Top up ›
        </ThemedText>
      </Pressable>
    </View>
  );
}

// ── Helpers ──────────────────────────────────────────────────────

function formatGrid(n: number): string {
  if (n >= 10000) return Math.round(n).toLocaleString();
  if (n >= 100) return n.toFixed(0);
  if (n >= 1) return n.toFixed(2);
  return n.toFixed(4);
}

const styles = StyleSheet.create({
  card: {
    padding: Card.padding,
    borderRadius: Radii.lg,
    borderWidth: StyleSheet.hairlineWidth,
    marginTop: Card.marginVertical,
    gap: Spacing.xs,
  },
  headerRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    marginBottom: Spacing.xs,
  },
  headerLabel: {
    ...TypeScale.captionStrong,
    letterSpacing: 1.5,
  },
  refresh: {
    fontSize: 18,
    fontWeight: '400',
  },
  provider: {
    ...TypeScale.bodyS,
  },
  balance: {
    ...TypeScale.displayS,
    fontVariant: ['tabular-nums'],
  },
  usd: {
    ...TypeScale.bodyS,
  },
  burn: {
    ...TypeScale.bodyS,
    fontWeight: '600',
    fontVariant: ['tabular-nums'],
    marginTop: Spacing.xs,
  },
  lowBalanceMsg: {
    ...TypeScale.bodyS,
    fontWeight: '500',
    marginTop: Spacing.xs,
  },
  topupButton: {
    marginTop: Spacing.md,
    paddingVertical: Spacing.md,
    borderRadius: Radii.md,
    alignItems: 'center',
  },
  topupLabel: {
    ...TypeScale.button,
  },
});
