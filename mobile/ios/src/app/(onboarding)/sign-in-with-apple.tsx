/**
 * Onboarding Screen 4 — Sign in with Apple (stub).
 *
 * The real Apple sign-in flow lives at this route in Track 1's
 * PR #601 worktree; the cherry-pick to main brought the backend
 * (apple_signin.go + auth.ts) but not this file. This stub
 * exists so:
 *   - Maestro flow 02-sign-in can assert + tap `connect-wallet-skip`
 *   - The router doesn't 404 when privacy.tsx pushes to /(onboarding)/sign-in-with-apple
 *   - The screen presents the Apple-Authentication button shape
 *
 * Track 1 follow-up will replace this with the real
 * expo-apple-authentication flow.
 *
 * Refs #582, #590.
 */

import { Pressable, StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { router } from 'expo-router';

import { ThemedText } from '@/components/themed-text';
import { ThemedView } from '@/components/themed-view';
import { Spacing, TypeScale } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';

export default function SignInWithAppleScreen() {
  const theme = useTheme();

  const onSkipForNow = () => {
    // Stub: route to wallet-connect onboarding screen (also a stub).
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
        </View>

        <View style={styles.ctaWrap}>
          <Pressable
            testID="sign-in-with-apple-button"
            onPress={onSkipForNow}
            style={({ pressed }) => [
              styles.cta,
              { backgroundColor: theme.text },
              pressed ? { opacity: 0.85 } : null,
            ]}
            accessibilityLabel="Sign in with Apple"
            accessibilityRole="button"
          >
            <ThemedText style={[styles.appleLogo, { color: theme.textInverse }]}>

            </ThemedText>
            <ThemedText style={[styles.ctaLabel, { color: theme.textInverse }]}>
              Sign in with Apple
            </ThemedText>
          </Pressable>

          {/* Skip button — Maestro flow 02-sign-in expects this for
              the stub path until the real Apple sign-in lands. */}
          <Pressable
            testID="connect-wallet-skip"
            onPress={onSkipForNow}
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
