/**
 * Top-up screen — Screen 10 per mobile/ios/docs/ux-wireframes-v2.md.
 *
 * User picks an amount and payment method. Continue deeplinks to the
 * bound wallet (Phantom or Ping) for the actual transaction. iogrid
 * polls balance every 5s for 60s after return to detect the credit.
 *
 * Refs #580, #594.
 *
 * Track 2 (PR #602) owns the wallet deeplink builders; this screen
 * stubs the bind state until those land.
 */

import { useEffect, useState } from 'react';
import { Alert, Linking, Pressable, ScrollView, StyleSheet, TextInput, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { router } from 'expo-router';

import { ThemedText } from '@/components/themed-text';
import { ThemedView } from '@/components/themed-view';
import { Card, Radii, Spacing, TypeScale } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';
import {
  buildVpnApproveUrl,
  onVpnApproveReturn,
  verifyApprovalBestEffort,
  vpnVault,
} from '@/lib/wallets/ping-pay';

// Default VPN region/length encoded into the Ping approve memo
// (iogrid.v1:vpn:<region>:<days>). The top-up screen tops up a generic
// $GRID balance; until per-region selection lands we use a 30-day
// global-pool memo so the backend can attribute the approved pull.
const DEFAULT_VPN_REGION = 'global';
const DEFAULT_VPN_DAYS = 30;

interface AmountChip {
  grid: number;
  usd: number;
}

const QUICK_AMOUNTS: AmountChip[] = [
  { grid: 500, usd: 5 },
  { grid: 2500, usd: 25 },
  { grid: 10000, usd: 100 },
];

type PaymentMethod = 'apple_pay' | 'card' | 'bitcoin' | 'usdc' | 'transfer';

interface MethodOption {
  id: PaymentMethod;
  label: string;
  hint?: string;
  emoji?: string;
}

const PAYMENT_METHODS: MethodOption[] = [
  { id: 'apple_pay', label: 'Apple Pay', hint: 'One-tap', emoji: '' },
  { id: 'card', label: 'Card', hint: 'Stripe via Ping' },
  { id: 'bitcoin', label: 'Bitcoin', hint: 'via Ping' },
  { id: 'usdc', label: 'USDC', hint: 'Solana on-chain' },
  { id: 'transfer', label: 'Transfer $GRID', hint: 'From another wallet' },
];

export default function TopUpScreen() {
  const theme = useTheme();
  const [selectedChip, setSelectedChip] = useState<number>(QUICK_AMOUNTS[1].grid);
  const [customAmount, setCustomAmount] = useState<string>('');
  const [method, setMethod] = useState<PaymentMethod>('apple_pay');

  // Stub bind state — Track 2 will populate from secure storage
  const walletProvider: 'phantom' | 'ping' | null = null;
  const currentBalance: number | null = null;

  // ── Ping approve return-bounce handler (Refs #629) ─────────────
  // iogrid://vpn/activated?ok=1&signature=<sig>  → success
  // iogrid://vpn/activated?ok=0&reason=cancel    → soft cancel (retry OK)
  useEffect(() => {
    const unsubscribe = onVpnApproveReturn((result) => {
      if (result.ok) {
        // Best-effort confirmation pending Ping's C-8 decision; we don't
        // block the success UX on it (the bounce ok=1 is authoritative
        // enough for v1 — chain confirmation runs in the background).
        void verifyApprovalBestEffort(result.signature);
        Alert.alert('Top up approved', 'Your $GRID approval is being confirmed.');
        router.back();
        return;
      }
      if (result.cancelled) {
        // C-10: soft back-out — user can re-prompt. No scary error.
        Alert.alert('Top up cancelled', 'No charge was made. You can try again.');
        return;
      }
      // Hard reject — surface the reason verbatim.
      Alert.alert('Top up failed', `Ping reported: ${result.reason}`);
    });
    return unsubscribe;
  }, []);

  const effectiveAmount = (() => {
    if (customAmount) {
      const parsed = parseInt(customAmount, 10);
      if (!isNaN(parsed) && parsed > 0) return parsed;
    }
    return selectedChip;
  })();

  const onChipPress = (grid: number) => {
    setSelectedChip(grid);
    setCustomAmount('');
  };

  const onContinue = async () => {
    if (!walletProvider) {
      Alert.alert(
        'Connect a wallet first',
        'Add Phantom or Ping in Settings → Wallet to start topping up.',
        [
          { text: 'Cancel', style: 'cancel' },
          {
            text: 'Connect wallet',
            onPress: () => router.replace('/(onboarding)/connect-wallet' as any),
          },
        ],
      );
      return;
    }

    if (effectiveAmount <= 0) {
      Alert.alert('Enter an amount', 'Pick a preset or type a custom $GRID amount.');
      return;
    }

    // Ping PAYMENT surface (Refs #629): launch the canonical SPL-Approve
    // Universal Link `https://ping.cash/approve?…` (NOT a custom scheme —
    // custom schemes trigger the iOS "Open in Ping?" interstitial Ping
    // designed around). The wallet-BIND flow stays in wallets/ping.ts.
    if (!vpnVault()) {
      // The delegate vault is env-indirected and may be unset in CI /
      // until the real vault address lands. Guard rather than launch a
      // delegate-less approve.
      Alert.alert(
        'Top up unavailable',
        'The $GRID payment vault is not yet configured. Please try again later.',
      );
      return;
    }

    let url: string;
    try {
      url = buildVpnApproveUrl({
        grid: effectiveAmount,
        region: DEFAULT_VPN_REGION,
        days: DEFAULT_VPN_DAYS,
      });
    } catch (e) {
      Alert.alert('Could not build payment', e instanceof Error ? e.message : String(e));
      return;
    }

    try {
      // Universal Links resolve via Safari/the OS when Ping is installed;
      // canOpenURL on an https:// link is always true, so we openURL
      // directly and let iOS route to the Ping app (falling back to web).
      await Linking.openURL(url);
    } catch (e) {
      Alert.alert('Could not open Ping', e instanceof Error ? e.message : String(e));
    }
  };

  return (
    <ThemedView style={styles.root}>
      <SafeAreaView style={styles.safe} edges={['top', 'left', 'right']}>
        {/* ── Nav header ───────────────────────────────────────── */}
        <View style={styles.navHeader}>
          <Pressable
            testID="topup-close"
            onPress={() => router.back()}
            hitSlop={12}
            accessibilityLabel="Close top up"
            accessibilityRole="button"
          >
            <ThemedText style={[styles.navLeading, { color: theme.textSecondary }]}>
              ✕ Close
            </ThemedText>
          </Pressable>
          <ThemedText style={[styles.navTitle, { color: theme.text }]}>
            Top up
          </ThemedText>
          <View style={styles.navTrailing} />
        </View>

        <ScrollView contentContainerStyle={styles.scroll} showsVerticalScrollIndicator={false}>
          {/* ── Hero block ───────────────────────────────────────── */}
          <View style={styles.hero}>
            <ThemedText style={[styles.heroTitle, { color: theme.text }]}>
              Add $GRID to your wallet
            </ThemedText>
            <ThemedText style={[styles.balanceLabel, { color: theme.textTertiary }]}>
              CURRENT BALANCE
            </ThemedText>
            <ThemedText style={[styles.balanceValue, { color: theme.text }]}>
              {currentBalance != null
                ? `${currentBalance} $GRID  ≈  $${(currentBalance / 100).toFixed(2)}`
                : '0 $GRID'}
            </ThemedText>
          </View>

          {/* ── Quick amount chips ───────────────────────────────── */}
          <ThemedText style={[styles.sectionHeader, { color: theme.textTertiary }]}>
            QUICK AMOUNTS
          </ThemedText>
          <View style={styles.chipRow}>
            {QUICK_AMOUNTS.map((chip) => {
              const active = !customAmount && selectedChip === chip.grid;
              return (
                <Pressable
                  key={chip.grid}
                  testID={`topup-chip-${chip.grid}`}
                  onPress={() => onChipPress(chip.grid)}
                  accessibilityLabel={`${chip.grid} GRID, $${chip.usd}`}
                  accessibilityRole="button"
                  accessibilityState={{ selected: active }}
                  style={({ pressed }) => [
                    styles.chip,
                    {
                      backgroundColor: active ? theme.text : theme.backgroundCard,
                      borderColor: active ? theme.text : theme.border,
                    },
                    pressed ? { opacity: 0.7 } : null,
                  ]}
                >
                  <ThemedText
                    style={[styles.chipGrid, { color: active ? theme.textInverse : theme.text }]}
                  >
                    +{chip.grid.toLocaleString()}
                  </ThemedText>
                  <ThemedText
                    style={[
                      styles.chipUsd,
                      { color: active ? theme.textInverse : theme.textSecondary },
                    ]}
                  >
                    ${chip.usd}
                  </ThemedText>
                </Pressable>
              );
            })}
          </View>

          {/* ── Custom amount ────────────────────────────────────── */}
          <View
            style={[
              styles.customRow,
              { backgroundColor: theme.backgroundCard, borderColor: theme.border },
            ]}
          >
            <ThemedText style={[styles.customLabel, { color: theme.text }]}>Custom</ThemedText>
            <TextInput
              testID="topup-custom-input"
              style={[styles.customInput, { color: theme.text }]}
              placeholder="amount"
              placeholderTextColor={theme.textTertiary}
              keyboardType="number-pad"
              returnKeyType="done"
              value={customAmount}
              onChangeText={(v) => setCustomAmount(v.replace(/[^0-9]/g, ''))}
            />
            <ThemedText style={[styles.customSuffix, { color: theme.textSecondary }]}>
              $GRID
            </ThemedText>
          </View>

          {/* ── Payment methods ──────────────────────────────────── */}
          <ThemedText style={[styles.sectionHeader, { color: theme.textTertiary }]}>
            PAY WITH
          </ThemedText>
          <View
            style={[
              styles.methodGroup,
              { backgroundColor: theme.backgroundCard, borderColor: theme.border },
            ]}
          >
            {PAYMENT_METHODS.map((m, i) => {
              const active = method === m.id;
              const last = i === PAYMENT_METHODS.length - 1;
              return (
                <Pressable
                  key={m.id}
                  testID={`topup-method-${m.id}`}
                  onPress={() => setMethod(m.id)}
                  accessibilityLabel={m.label}
                  accessibilityRole="radio"
                  accessibilityState={{ selected: active }}
                  style={({ pressed }) => [
                    styles.methodRow,
                    {
                      borderBottomColor: theme.border,
                      borderBottomWidth: last ? 0 : StyleSheet.hairlineWidth,
                    },
                    pressed ? { opacity: 0.7 } : null,
                  ]}
                >
                  <View
                    style={[
                      styles.radio,
                      {
                        borderColor: active ? theme.text : theme.borderStrong,
                      },
                    ]}
                  >
                    {active ? (
                      <View style={[styles.radioDot, { backgroundColor: theme.text }]} />
                    ) : null}
                  </View>
                  <View style={styles.methodText}>
                    <ThemedText style={[styles.methodLabel, { color: theme.text }]}>
                      {m.label}
                    </ThemedText>
                    {m.hint ? (
                      <ThemedText style={[styles.methodHint, { color: theme.textSecondary }]}>
                        {m.hint}
                      </ThemedText>
                    ) : null}
                  </View>
                </Pressable>
              );
            })}
          </View>

          {/* ── Continue ─────────────────────────────────────────── */}
          <Pressable
            testID="topup-continue"
            onPress={onContinue}
            style={({ pressed }) => [
              styles.cta,
              { backgroundColor: theme.text },
              pressed ? { opacity: 0.85 } : null,
            ]}
            accessibilityLabel={`Continue topping up ${effectiveAmount} GRID`}
            accessibilityRole="button"
          >
            <ThemedText style={[styles.ctaLabel, { color: theme.textInverse }]}>
              Continue
            </ThemedText>
          </Pressable>

          <ThemedText style={[styles.poweredBy, { color: theme.textTertiary }]}>
            Powered by Ping
          </ThemedText>
        </ScrollView>
      </SafeAreaView>
    </ThemedView>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  safe: { flex: 1 },
  navHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: Spacing.lg,
    paddingVertical: Spacing.md,
  },
  navLeading: {
    ...TypeScale.bodyM,
    fontWeight: '500',
    minWidth: 80,
  },
  navTitle: {
    ...TypeScale.bodyL,
    fontWeight: '600',
  },
  navTrailing: {
    minWidth: 80,
  },
  scroll: {
    paddingBottom: Spacing.xxxl,
    paddingHorizontal: Spacing.lg,
  },
  hero: {
    paddingVertical: Spacing.xl,
    gap: Spacing.xs,
  },
  heroTitle: {
    ...TypeScale.displayS,
    marginBottom: Spacing.lg,
  },
  balanceLabel: {
    ...TypeScale.captionStrong,
    letterSpacing: 1.5,
  },
  balanceValue: {
    ...TypeScale.displayS,
    fontVariant: ['tabular-nums'],
  },
  sectionHeader: {
    ...TypeScale.captionStrong,
    letterSpacing: 1.5,
    paddingTop: Spacing.xl,
    paddingBottom: Spacing.sm,
  },
  chipRow: {
    flexDirection: 'row',
    gap: Spacing.sm,
  },
  chip: {
    flex: 1,
    paddingVertical: Spacing.md,
    paddingHorizontal: Spacing.sm,
    borderRadius: Radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: 'center',
    gap: 2,
  },
  chipGrid: {
    ...TypeScale.bodyL,
    fontWeight: '600',
    fontVariant: ['tabular-nums'],
  },
  chipUsd: {
    ...TypeScale.bodyS,
  },
  customRow: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: Card.padding,
    paddingVertical: Spacing.md,
    borderRadius: Radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    marginTop: Spacing.sm,
    gap: Spacing.md,
  },
  customLabel: {
    ...TypeScale.bodyL,
    fontWeight: '500',
  },
  customInput: {
    flex: 1,
    ...TypeScale.bodyL,
    paddingVertical: Spacing.xs,
  },
  customSuffix: {
    ...TypeScale.bodyM,
  },
  methodGroup: {
    borderRadius: Radii.lg,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: 'hidden',
  },
  methodRow: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: Card.padding,
    paddingVertical: Spacing.md,
    gap: Spacing.md,
    minHeight: 56,
  },
  radio: {
    width: 22,
    height: 22,
    borderRadius: 11,
    borderWidth: 2,
    alignItems: 'center',
    justifyContent: 'center',
  },
  radioDot: {
    width: 10,
    height: 10,
    borderRadius: 5,
  },
  methodText: {
    flex: 1,
  },
  methodLabel: {
    ...TypeScale.bodyL,
    fontWeight: '500',
  },
  methodHint: {
    ...TypeScale.bodyS,
    marginTop: 2,
  },
  cta: {
    marginTop: Spacing.xxl,
    paddingVertical: Spacing.lg,
    borderRadius: Radii.md,
    alignItems: 'center',
    minHeight: 56,
    justifyContent: 'center',
  },
  ctaLabel: {
    ...TypeScale.button,
  },
  poweredBy: {
    ...TypeScale.bodyS,
    textAlign: 'center',
    marginTop: Spacing.lg,
  },
});
