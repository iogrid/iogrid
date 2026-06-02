// Onboarding 2 of 2 — Privacy promise + "Sign in with Apple" CTA.
//
// Wireframe ref: mobile/ios/docs/ux-wireframes-v2.md Screen 3.
//
// Apple-sign-in delegates to Track 1's auth.signInWithApple(),
// persists onboarded=true, navigates to /(onboarding)/connect-wallet.

import { StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { useRouter } from 'expo-router';
import AsyncStorage from '@react-native-async-storage/async-storage';

import { Button } from '@/components/button';
import { SectionCard } from '@/components/section-card';
import { ThemedText } from '@/components/themed-text';
import { ThemedView } from '@/components/themed-view';
import { Spacing } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';
import { auth, ONBOARDED_KEY } from '@/lib/auth';

const PROMISES: { label: string }[] = [
  { label: 'No logs' },
  { label: 'Apple-only identity' },
  { label: 'No tracking' },
  { label: 'Pay with $GRID or Apple Pay' },
];

export default function PrivacyScreen() {
  const router = useRouter();
  const theme = useTheme();

  const onSignIn = async () => {
    try {
      await auth.signInWithApple();
      await AsyncStorage.setItem(ONBOARDED_KEY, '1');
      router.replace('/(onboarding)/connect-wallet');
    } catch (e) {
      console.warn('Apple sign-in failed', e);
    }
  };

  return (
    <ThemedView style={styles.container}>
      <SafeAreaView style={styles.safe}>
        <View style={styles.topBar}>
          <View style={styles.dots}>
            <View style={[styles.dot, { backgroundColor: theme.border }]} />
            <View style={[styles.dot, { backgroundColor: theme.text }]} />
          </View>
        </View>

        <View style={styles.heroSpacer} />

        <SectionCard style={styles.promiseCard}>
          {PROMISES.map((p) => (
            <View key={p.label} style={styles.promiseRow}>
              <ThemedText type="body-l" color={theme.accent}>
                ✓
              </ThemedText>
              <ThemedText type="body-m">{p.label}</ThemedText>
            </View>
          ))}
        </SectionCard>

        <View style={styles.body}>
          <ThemedText type="display-m" style={styles.headline}>
            Privacy by default
          </ThemedText>
          <ThemedText
            type="body-m"
            color={theme.textSecondary}
            style={styles.paragraph}
          >
            iogrid never stores traffic logs. Apple knows your ID; iogrid
            only sees a salted hash. We can&apos;t link your IP back to
            your account.
          </ThemedText>
        </View>

        <View style={styles.cta}>
          <Button
            label="Sign in with Apple"
            variant="primary"
            size="lg"
            fullWidth
            onPress={onSignIn}
            testID="onboarding-sign-in-apple"
          />
        </View>
      </SafeAreaView>
    </ThemedView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1 },
  safe: { flex: 1, paddingHorizontal: Spacing.xl },
  topBar: {
    flexDirection: 'row',
    justifyContent: 'flex-start',
    alignItems: 'center',
    paddingVertical: Spacing.lg,
  },
  dots: { flexDirection: 'row', gap: Spacing.sm },
  dot: { width: 6, height: 6, borderRadius: 3 },
  heroSpacer: { height: Spacing.xxl },
  promiseCard: { gap: Spacing.md },
  promiseRow: { flexDirection: 'row', alignItems: 'center', gap: Spacing.md },
  body: { gap: Spacing.lg, marginTop: Spacing.xxl, flex: 1 },
  headline: {},
  paragraph: { lineHeight: 24 },
  cta: { paddingBottom: Spacing.xl },
});
