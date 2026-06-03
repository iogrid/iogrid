/**
 * Onboarding Screen 4 — Sign in with Apple.
 *
 * Wires the real expo-apple-authentication flow (src/lib/auth.ts
 * `signInWithApple()`) into the UI. Previously a stub whose button just
 * routed forward without authenticating (#629 GAP 4 — UAT 2026-06-03);
 * the backend (apple_signin.go) + helper (auth.ts) were cherry-picked from
 * Track 1 (#601) but this screen was never connected.
 *
 * Flow: tap → native Apple sheet → POST identityToken to identity-svc →
 * persist session in Keychain → route to wallet-connect onboarding. Error
 * model maps the four AuthError codes: `apple_canceled` stays silent on
 * this screen; the rest show "Apple sign-in failed, try again".
 *
 * Refs #582, #590, #601, #629.
 */

import { useState } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { router } from 'expo-router';

import { StatusDot } from '@/components/icons';
import { ThemedText } from '@/components/themed-text';
import { ThemedView } from '@/components/themed-view';
import { Spacing, TypeScale } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';
import { AuthError, signInWithApple } from '@/lib/auth';

export default function SignInWithAppleScreen() {
  const theme = useTheme();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Real Sign in with Apple: drive the native sheet → identity-svc → Keychain.
  const onSignInWithApple = async () => {
    if (busy) return;
    setError(null);
    setBusy(true);
    try {
      await signInWithApple();
      // Success — session persisted. Continue onboarding to wallet connect.
      router.replace('/(onboarding)/connect-wallet' as any);
    } catch (err: unknown) {
      if (err instanceof AuthError && err.code === 'apple_canceled') {
        // User dismissed the sheet — no banner, just stay on this screen.
        return;
      }
      setError('Apple sign-in failed. Please try again.');
    } finally {
      setBusy(false);
    }
  };

  // Skip remains for the free/consume-only path (App Store: VPN works without
  // an account) + Maestro flow 02-sign-in which taps `connect-wallet-skip`.
  const onSkipForNow = () => {
    router.push('/(onboarding)/connect-wallet' as any);
  };

  return (
    <ThemedView style={styles.root}>
      <SafeAreaView style={styles.safe} edges={['top', 'bottom', 'left', 'right']}>
        <View style={styles.copy}>
          <ThemedText style={[styles.headline, { color: theme.text }]}>
            Sign in with Apple
          </ThemedText>
          <ThemedText style={[styles.body, { color: theme.textSecondary }]}>
            iogrid uses Apple sign-in for privacy. Tap the button below to
            continue.
          </ThemedText>
          {error ? (
            <ThemedText
              testID="sign-in-with-apple-error"
              style={[styles.error, { color: theme.error }]}
            >
              {error}
            </ThemedText>
          ) : null}
        </View>

        {/* Privacy assurances — fills the previously-dead middle with the
            quiet-card language the home screen established (UX pass 2,
            #684). Marketing-free statements of what the auth model
            actually does; monochrome + single accent per the locked
            design system. */}
        <View style={styles.assuranceWrap}>
          <View
            style={[
              styles.assuranceCard,
              { backgroundColor: theme.backgroundElement, borderColor: theme.border },
            ]}
          >
            {[
              'No activity logs — ever',
              'Apple hides your email from us',
              'The VPN works without an account',
            ].map((line) => (
              <View key={line} style={styles.assuranceRow}>
                <StatusDot size={6} color={theme.accent} />
                <ThemedText style={[styles.assuranceText, { color: theme.textSecondary }]}>
                  {line}
                </ThemedText>
              </View>
            ))}
          </View>
        </View>

        <View style={styles.ctaWrap}>
          <Pressable
            testID="sign-in-with-apple-button"
            onPress={onSignInWithApple}
            disabled={busy}
            style={({ pressed }) => [
              styles.cta,
              { backgroundColor: theme.text },
              pressed || busy ? { opacity: 0.85 } : null,
            ]}
            accessibilityLabel="Sign in with Apple"
            accessibilityRole="button"
            accessibilityState={{ disabled: busy, busy }}
          >
            {busy ? (
              <ActivityIndicator color={theme.textInverse} />
            ) : (
              <>
                <ThemedText style={[styles.appleLogo, { color: theme.textInverse }]}>
                  {''}
                </ThemedText>
                <ThemedText style={[styles.ctaLabel, { color: theme.textInverse }]}>
                  Sign in with Apple
                </ThemedText>
              </>
            )}
          </Pressable>

          {/* Skip — consume-only VPN works without an account (App Store
              policy); Maestro flow 02-sign-in taps this testID. */}
          <Pressable
            testID="connect-wallet-skip"
            onPress={onSkipForNow}
            disabled={busy}
            style={({ pressed }) => [
              styles.skipBtn,
              pressed ? { opacity: 0.7 } : null,
            ]}
            accessibilityLabel="Skip for now"
            accessibilityRole="button"
          >
            <ThemedText style={[styles.skipLabel, { color: theme.textSecondary }]}>
              Skip for now
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
  error: { ...TypeScale.bodyS, marginTop: Spacing.sm },
  assuranceWrap: {
    flex: 1,
    justifyContent: 'center',
    paddingHorizontal: Spacing.xl,
  },
  assuranceCard: {
    borderRadius: 16,
    borderWidth: StyleSheet.hairlineWidth,
    paddingVertical: Spacing.lg,
    paddingHorizontal: Spacing.lg,
    gap: Spacing.md,
  },
  assuranceRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: Spacing.md,
  },
  assuranceText: { ...TypeScale.bodyM },
  ctaWrap: {
    paddingHorizontal: Spacing.lg,
    paddingBottom: Spacing.lg,
    gap: Spacing.md,
  },
  cta: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: Spacing.sm,
    paddingVertical: Spacing.lg,
    borderRadius: 12,
    minHeight: 56,
  },
  appleLogo: { fontSize: 18, fontWeight: '500' },
  ctaLabel: { ...TypeScale.button },
  skipBtn: {
    paddingVertical: Spacing.md,
    alignItems: 'center',
  },
  skipLabel: {
    ...TypeScale.bodyM,
    fontWeight: '500',
  },
});
