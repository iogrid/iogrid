// WalletCard — $GRID balance + USD optional + burn ticker (#594).
//
// Two display modes:
//   - default (DISCONNECTED): balance + "~N days at usual usage" + Top up CTA
//   - connected: balance + live burn rate (e.g. "0.002 $GRID/min") + Top up CTA
// State owned by parent — this is a pure renderer.

import { StyleSheet, View } from 'react-native';

import { Button } from '@/components/button';
import { SectionCard } from '@/components/section-card';
import { ThemedText } from '@/components/themed-text';
import { Spacing } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';

export interface WalletCardProps {
  balanceGrid: number;
  balanceUsd?: number;
  /** Burn rate while CONNECTED, in $GRID/min. Omit to render the
   *  static "X days at usual usage" copy instead. */
  burnRateGridPerMin?: number;
  estimatedDays?: number;
  onTopupPress?: () => void;
  testID?: string;
}

export function WalletCard({
  balanceGrid,
  balanceUsd,
  burnRateGridPerMin,
  estimatedDays,
  onTopupPress,
  testID = 'wallet-card',
}: WalletCardProps) {
  const theme = useTheme();
  return (
    <SectionCard testID={testID}>
      <ThemedText type="caption" color={theme.textSecondary}>
        WALLET
      </ThemedText>
      <View style={styles.balanceRow}>
        <ThemedText type="display-s" style={styles.balance} testID="wallet-balance-grid">
          {balanceGrid.toLocaleString()} $GRID
        </ThemedText>
        {balanceUsd !== undefined ? (
          <ThemedText type="body-m" color={theme.textSecondary} style={styles.usd}>
            ≈ ${balanceUsd.toFixed(2)}
          </ThemedText>
        ) : null}
      </View>
      {burnRateGridPerMin !== undefined ? (
        <ThemedText type="body-s" color={theme.textSecondary}>
          Burning {burnRateGridPerMin} $GRID/min
        </ThemedText>
      ) : estimatedDays !== undefined ? (
        <ThemedText type="body-s" color={theme.textSecondary}>
          ~{estimatedDays} days at usual usage
        </ThemedText>
      ) : null}
      {onTopupPress ? (
        <Button
          label="Top up"
          variant="secondary"
          size="sm"
          onPress={onTopupPress}
          testID="wallet-topup-button"
          style={styles.cta}
        />
      ) : null}
    </SectionCard>
  );
}

const styles = StyleSheet.create({
  balanceRow: {
    flexDirection: 'row',
    alignItems: 'baseline',
    gap: Spacing.sm,
    marginTop: Spacing.xs,
  },
  balance: {
    fontVariant: ['tabular-nums'],
    fontWeight: '700',
  },
  usd: {
    fontVariant: ['tabular-nums'],
  },
  cta: {
    marginTop: Spacing.md,
    alignSelf: 'flex-start',
  },
});
