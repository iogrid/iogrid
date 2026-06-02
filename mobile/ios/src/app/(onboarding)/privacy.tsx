/**
 * Onboarding Screen 3 — "Privacy by default" + Sign in with Apple CTA.
 *
 * Per mobile/ios/docs/ux-wireframes-v2.md Screen 3. Establishes the
 * iogrid privacy promise (no logs, Apple-only identity, no tracking,
 * $GRID-or-Apple-Pay) then routes to the Apple sign-in sheet.
 *
 * The 'Sign in with Apple' button defers to Track 1's
 * src/app/(onboarding)/sign-in-with-apple.tsx (PR #601) which owns
 * the actual `expo-apple-authentication` integration.
 *
 * Refs #580, #590.
 */

import { Pressable, StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { router } from 'expo-router';

import { ThemedText } from '@/components/themed-text';
import { ThemedView } from '@/components/themed-view';
import { Spacing, TypeScale } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';

const PROMISES = [
  'No logs',
  'Apple-only ID',
  'No tracking',
  'Pay with $GRID or Apple Pay',
] as const;

export default function PrivacyScreen() {
  const theme = useTheme();

  return (
    <ThemedView style={styles.root}>
      <SafeAreaView style={styles.safe} edges={['top', 'bottom', 'left', 'right']}>
        {/* ── Page indicator + back ────────────────────────────── */}
        <View style={styles.topBar}>
          <Pressable
            testID="onboarding-back"
            onPress={() => router.back()}
            hitSlop={12}
            accessibilityLabel="Go back"
            accessibilityRole="button"
          >
            <ThemedText style={[styles.back, { color: theme.textSecondary }]}>
              ‹ Back
            </ThemedText>
          </Pressable>
          <View style={styles.dots}>
            <View style={[styles.dot, { backgroundColor: theme.border }]} />
            <View
              style={[styles.dot, styles.dotActive, { backgroundColor: theme.text }]}
            />
          </View>
        </View>

        {/* ── Promise checklist card ───────────────────────────── */}
        <View style={styles.checklistWrap}>
          <View
            style={[
              styles.checklist,
              { backgroundColor: theme.backgroundElement, borderColor: theme.border },
            ]}
          >
            {PROMISES.map((promise, i) => (
              <View key={i} style={styles.promiseRow}>
                <View
                  style={[
                    styles.checkBox,
                    { borderColor: theme.text, backgroundColor: theme.text },
                  ]}
                >
                  <ThemedText style={[styles.checkMark, { color: theme.textInverse }]}>
                    ✓
                  </ThemedText>
                </View>
                <ThemedText style={[styles.promiseText, { color: theme.text }]}>
                  {promise}
                </ThemedText>
              </View>
            ))}
          </View>
        </View>

        {/* ── Copy ─────────────────────────────────────────────── */}
        <View style={styles.copy}>
          <ThemedText style={[styles.headline, { color: theme.text }]}>
            Privacy by default.
          </ThemedText>
          <ThemedText style={[styles.body, { color: theme.textSecondary }]}>
            iogrid never stores traffic logs. Apple knows your ID; iogrid only
            sees a salted hash. We can't link your IP back to your account.
          </ThemedText>
        </View>

        {/* ── Sign in with Apple CTA ───────────────────────────── */}
        <View style={styles.ctaWrap}>
          <Pressable
            testID="onboarding-sign-in-apple"
            onPress={() => router.push('/(onboarding)/sign-in-with-apple' as any)}
            style={({ pressed }) => [
              styles.cta,
              { backgroundColor: theme.text },
              pressed ? styles.ctaPressed : null,
            ]}
            accessibilityLabel="Continue to Sign in with Apple"
            accessibilityRole="button"
          >
            <ThemedText style={[styles.appleLogo, { color: theme.textInverse }]}>

            </ThemedText>
            <ThemedText style={[styles.ctaLabel, { color: theme.textInverse }]}>
              Sign in with Apple
            </ThemedText>
          </Pressable>
        </View>
      </SafeAreaView>
    </ThemedView>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  safe: { flex: 1 },
  topBar: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: Spacing.lg,
    paddingTop: Spacing.md,
  },
  back: {
    ...TypeScale.bodyM,
    fontWeight: '500',
  },
  dots: {
    flexDirection: 'row',
    gap: Spacing.sm,
  },
  dot: {
    width: 8,
    height: 8,
    borderRadius: 4,
  },
  dotActive: {
    width: 24,
  },
  checklistWrap: {
    flex: 1,
    paddingHorizontal: Spacing.xl,
    alignItems: 'center',
    justifyContent: 'center',
  },
  checklist: {
    width: '100%',
    maxWidth: 320,
    padding: Spacing.xl,
    borderRadius: 16,
    borderWidth: StyleSheet.hairlineWidth,
    gap: Spacing.md,
  },
  promiseRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: Spacing.md,
  },
  checkBox: {
    width: 22,
    height: 22,
    borderRadius: 4,
    borderWidth: 1,
    alignItems: 'center',
    justifyContent: 'center',
  },
  checkMark: {
    ...TypeScale.captionStrong,
    fontWeight: '700',
  },
  promiseText: {
    ...TypeScale.bodyL,
    flex: 1,
  },
  copy: {
    paddingHorizontal: Spacing.xl,
    gap: Spacing.md,
    marginBottom: Spacing.xl,
  },
  headline: {
    ...TypeScale.displayM,
  },
  body: {
    ...TypeScale.bodyM,
  },
  ctaWrap: {
    paddingHorizontal: Spacing.lg,
    paddingBottom: Spacing.lg,
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
  ctaPressed: {
    opacity: 0.85,
  },
  appleLogo: {
    fontSize: 18,
    fontWeight: '500',
  },
  ctaLabel: {
    ...TypeScale.button,
  },
});
