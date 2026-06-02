// Onboarding 1 of 2 — "A VPN powered by people, not data centers".
//
// Wireframe ref: mobile/ios/docs/ux-wireframes-v2.md Screen 2.
//
// Page dots top-center, Skip top-right. Continue button at the bottom.

import { Pressable, StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { useRouter } from 'expo-router';

import { Button } from '@/components/button';
import { ThemedText } from '@/components/themed-text';
import { ThemedView } from '@/components/themed-view';
import { Spacing } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';

export default function WelcomeScreen() {
  const router = useRouter();
  const theme = useTheme();

  const onSkip = () => router.replace('/(onboarding)/privacy');
  const onContinue = () => router.push('/(onboarding)/privacy');

  return (
    <ThemedView style={styles.container}>
      <SafeAreaView style={styles.safe}>
        <View style={styles.topBar}>
          <View style={styles.dots}>
            <View style={[styles.dot, { backgroundColor: theme.text }]} />
            <View style={[styles.dot, { backgroundColor: theme.border }]} />
          </View>
          <Pressable testID="onboarding-skip" onPress={onSkip} hitSlop={12}>
            <ThemedText type="body-s" color={theme.textSecondary}>
              Skip
            </ThemedText>
          </Pressable>
        </View>

        <View style={styles.heroSpacer} />

        <View style={styles.body}>
          <ThemedText type="display-l" style={styles.headline}>
            A VPN powered by people, not data centers
          </ThemedText>
          <ThemedText
            type="body-m"
            color={theme.textSecondary}
            style={styles.paragraph}
          >
            iogrid routes traffic through real homes from real people who
            rent their idle bandwidth. Pay only for what you use, in
            $GRID tokens.
          </ThemedText>
        </View>

        <View style={styles.cta}>
          <Button
            label="Continue"
            variant="primary"
            size="lg"
            fullWidth
            onPress={onContinue}
            testID="onboarding-welcome-continue"
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
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingVertical: Spacing.lg,
  },
  dots: { flexDirection: 'row', gap: Spacing.sm },
  dot: { width: 6, height: 6, borderRadius: 3 },
  heroSpacer: { flex: 1 },
  body: { gap: Spacing.lg, paddingBottom: Spacing.xxl },
  headline: { lineHeight: 48 },
  paragraph: { lineHeight: 24 },
  cta: { paddingBottom: Spacing.xl },
});
