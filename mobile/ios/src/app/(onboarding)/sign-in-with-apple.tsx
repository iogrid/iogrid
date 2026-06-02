// STUB — Track 1 owns the real Apple sign-in screen / interstitial.
//
// The default Apple flow goes through ASAuthorizationAppleIDProvider
// (the system sheet) directly. Track 1's PR will either delete this
// stub or replace it with a styled "Continue with Apple" interstitial.
// Keeping the route present prevents 404s if linked before Track 1
// lands.

import { StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { useRouter } from 'expo-router';

import { Button } from '@/components/button';
import { ThemedText } from '@/components/themed-text';
import { ThemedView } from '@/components/themed-view';
import { Spacing } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';

export default function SignInWithAppleStub() {
  const router = useRouter();
  const theme = useTheme();
  return (
    <ThemedView style={styles.container}>
      <SafeAreaView style={styles.safe}>
        <ThemedText type="display-m">Sign in with Apple</ThemedText>
        <ThemedText type="body-m" color={theme.textSecondary}>
          (Track 1 lands the real ASAuthorizationAppleIDProvider call here.)
        </ThemedText>
        <View style={styles.spacer} />
        <Button
          label="Continue"
          variant="primary"
          fullWidth
          onPress={() => router.replace('/(onboarding)/connect-wallet')}
          testID="sign-in-apple-continue"
        />
      </SafeAreaView>
    </ThemedView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1 },
  safe: { flex: 1, paddingHorizontal: Spacing.xl, paddingVertical: Spacing.xl, gap: Spacing.lg },
  spacer: { flex: 1 },
});
