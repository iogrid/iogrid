/**
 * Onboarding Screen 5 — Connect wallet (stub).
 *
 * The real wallet-connect flow (Phantom + Ping) lives in Track 2's
 * PR #602 worktree; the cherry-pick to main brought the backend
 * (wallet_bind.go) + the wallet libs (src/lib/wallets/) but not
 * this screen. This stub exists so:
 *   - Maestro flow 03-wallet-connect can tap `connect-wallet-continue`
 *   - The router doesn't 404 when sign-in-with-apple.tsx pushes here
 *
 * Track 2 follow-up will replace this with the real Phantom/Ping
 * deeplink picker.
 *
 * Refs #583, #584, #585.
 */

import { Pressable, StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { router } from 'expo-router';

import { ThemedText } from '@/components/themed-text';
import { ThemedView } from '@/components/themed-view';
import { Spacing, TypeScale } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';

export default function ConnectWalletScreen() {
  const theme = useTheme();

  const onContinue = () => {
    // Stub: route to main app screen.
    router.replace('/' as any);
  };

  return (
    <ThemedView style={styles.root}>
      <SafeAreaView style={styles.safe} edges={['top', 'bottom', 'left', 'right']}>
        <View style={styles.copy}>
          <ThemedText style={[styles.headline, { color: theme.text }]}>
            Connect a wallet
          </ThemedText>
          <ThemedText style={[styles.body, { color: theme.textSecondary }]}>
            iogrid pays providers in $GRID tokens. Connect Phantom or Ping
            to top up your balance, or skip and connect later from Settings.
          </ThemedText>
        </View>

        <View style={styles.ctaWrap}>
          <Pressable
            testID="connect-wallet-continue"
            onPress={onContinue}
            style={({ pressed }) => [
              styles.cta,
              { backgroundColor: theme.text },
              pressed ? { opacity: 0.85 } : null,
            ]}
            accessibilityLabel="Continue to main"
            accessibilityRole="button"
          >
            <ThemedText style={[styles.ctaLabel, { color: theme.textInverse }]}>
              Continue
            </ThemedText>
          </Pressable>
        </View>
      </SafeAreaView>
    </ThemedView>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  safe: { flex: 1, justifyContent: 'space-between' },
  copy: {
    paddingHorizontal: Spacing.xl,
    paddingTop: Spacing.xxxl,
    gap: Spacing.md,
  },
  headline: { ...TypeScale.displayM },
  body: { ...TypeScale.bodyM },
  ctaWrap: {
    paddingHorizontal: Spacing.lg,
    paddingBottom: Spacing.lg,
  },
  cta: {
    alignItems: 'center',
    justifyContent: 'center',
    paddingVertical: Spacing.lg,
    borderRadius: 12,
    minHeight: 56,
  },
  ctaLabel: { ...TypeScale.button },
});
