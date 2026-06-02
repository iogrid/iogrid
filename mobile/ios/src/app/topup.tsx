// Top-up — Screen 10 of the v2 wireframes (#594).
//
// Quick-amount chips + payment method picker + Continue button.
// Continue builds a deeplink via wallet.buildTopupURL() (Track 2)
// and hands off to the bound wallet. Modal presentation.

import { useEffect, useState } from 'react';
import { Linking, ScrollView, StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { useRouter } from 'expo-router';

import { Button } from '@/components/button';
import { SectionCard } from '@/components/section-card';
import { ThemedText } from '@/components/themed-text';
import { ThemedView } from '@/components/themed-view';
import { Radii, Spacing } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';
import { wallet, type WalletBalance, type WalletState } from '@/lib/wallet';

const QUICK_AMOUNTS = [
  { grid: 500, usd: 5 },
  { grid: 2500, usd: 25 },
  { grid: 10000, usd: 100 },
] as const;

const PAYMENT_METHODS = [
  { id: 'apple_pay', label: 'Apple Pay' },
  { id: 'card', label: 'Card' },
  { id: 'bitcoin', label: 'Bitcoin' },
  { id: 'usdc', label: 'USDC' },
  { id: 'transfer', label: 'Transfer $GRID from another wallet' },
] as const;

export default function TopupScreen() {
  const router = useRouter();
  const theme = useTheme();
  const [amountGrid, setAmountGrid] = useState<number>(QUICK_AMOUNTS[1].grid);
  const [method, setMethod] = useState<string>(PAYMENT_METHODS[0].id);
  const [balance, setBalance] = useState<WalletBalance>({ balanceGrid: 0, balanceUsd: 0 });
  const [walletState, setWalletState] = useState<WalletState | null>(null);

  useEffect(() => {
    wallet.getBalance().then(setBalance).catch(() => undefined);
    wallet.getStored().then(setWalletState).catch(() => undefined);
  }, []);

  const onContinue = async () => {
    try {
      const url = await wallet.buildTopupURL(amountGrid, method);
      const can = await Linking.canOpenURL(url);
      if (can) await Linking.openURL(url);
      else console.warn('topup: cannot open URL', url);
    } catch (e) {
      console.warn('topup buildURL failed', e);
    } finally {
      router.back();
    }
  };

  return (
    <ThemedView style={styles.container}>
      <SafeAreaView style={styles.safe} edges={['bottom']}>
        <ScrollView contentContainerStyle={styles.scroll}>
          <View style={styles.header}>
            <ThemedText type="display-m">Add $GRID</ThemedText>
            <ThemedText type="body-m" color={theme.textSecondary}>
              Current balance: {balance.balanceGrid.toLocaleString()} $GRID ≈ ${balance.balanceUsd.toFixed(2)}
            </ThemedText>
          </View>

          <ThemedText type="caption" color={theme.textSecondary} style={styles.sectionHeader}>
            QUICK AMOUNTS
          </ThemedText>
          <View style={styles.chipRow}>
            {QUICK_AMOUNTS.map((q) => {
              const selected = q.grid === amountGrid;
              return (
                <SectionCard
                  key={q.grid}
                  testID={`topup-quick-${q.grid}`}
                  onPress={() => setAmountGrid(q.grid)}
                  style={[
                    styles.chip,
                    {
                      borderColor: selected ? theme.text : theme.border,
                      borderWidth: 1,
                    },
                  ]}
                >
                  <ThemedText type="body-l" style={styles.chipTitle}>
                    +{q.grid.toLocaleString()} $GRID
                  </ThemedText>
                  <ThemedText type="body-s" color={theme.textSecondary}>
                    ${q.usd}
                  </ThemedText>
                </SectionCard>
              );
            })}
          </View>

          <ThemedText type="caption" color={theme.textSecondary} style={styles.sectionHeader}>
            PAY WITH
          </ThemedText>
          <View style={styles.methods}>
            {PAYMENT_METHODS.map((m) => {
              const selected = m.id === method;
              return (
                <SectionCard
                  key={m.id}
                  testID={`topup-method-${m.id}`}
                  onPress={() => setMethod(m.id)}
                  style={[
                    styles.method,
                    {
                      borderColor: selected ? theme.text : theme.border,
                      borderWidth: 1,
                    },
                  ]}
                >
                  <ThemedText type="body-m" style={styles.methodLabel}>
                    {m.label}
                  </ThemedText>
                  {selected ? (
                    <ThemedText type="body-m" color={theme.accent}>
                      ✓
                    </ThemedText>
                  ) : null}
                </SectionCard>
              );
            })}
          </View>

          <ThemedText type="body-s" color={theme.textTertiary} style={styles.poweredBy}>
            {walletState ? `Powered by ${walletState.provider}` : 'Powered by ping'}
          </ThemedText>
        </ScrollView>
        <View style={styles.footer}>
          <Button
            label="Continue"
            variant="primary"
            size="lg"
            fullWidth
            onPress={onContinue}
            testID="topup-continue"
          />
        </View>
      </SafeAreaView>
    </ThemedView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1 },
  safe: { flex: 1 },
  scroll: {
    paddingHorizontal: Spacing.xl,
    paddingTop: Spacing.lg,
    paddingBottom: Spacing.xl,
    gap: Spacing.sm,
  },
  header: { gap: 4, paddingBottom: Spacing.lg },
  sectionHeader: {
    paddingTop: Spacing.xl,
    paddingBottom: Spacing.sm,
    textTransform: 'uppercase',
    letterSpacing: 1.5,
  },
  chipRow: { flexDirection: 'row', flexWrap: 'wrap', gap: Spacing.md },
  chip: {
    minWidth: '30%',
    paddingVertical: Spacing.md,
    paddingHorizontal: Spacing.lg,
    borderRadius: Radii.md,
  },
  chipTitle: { fontWeight: '600' },
  methods: { gap: Spacing.sm },
  method: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingVertical: Spacing.lg,
    paddingHorizontal: Spacing.lg,
    borderRadius: Radii.md,
  },
  methodLabel: { fontWeight: '500' },
  poweredBy: { textAlign: 'center', paddingTop: Spacing.xxl },
  footer: {
    paddingHorizontal: Spacing.xl,
    paddingBottom: Spacing.lg,
    paddingTop: Spacing.md,
  },
});
