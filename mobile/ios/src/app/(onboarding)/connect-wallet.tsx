// STUB — Track 2 owns the wallet connect screen.
//
// Track 4 ships this placeholder so onboarding doesn't dead-end while
// Track 2 lands. Skipping wallet binding is OK — the main screen
// renders with balance=0 and prompts on first connect.

import { StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { useRouter } from 'expo-router';

import { Button } from '@/components/button';
import { ThemedText } from '@/components/themed-text';
import { ThemedView } from '@/components/themed-view';
import { Spacing } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';

export default function ConnectWalletStub() {
  const router = useRouter();
  const theme = useTheme();
  const finish = () => router.replace('/');
  return (
    <ThemedView style={styles.container}>
      <SafeAreaView style={styles.safe}>
        <ThemedText type="display-m">Connect a wallet</ThemedText>
        <ThemedText type="body-m" color={theme.textSecondary}>
          Bind a $GRID wallet now or skip and do it later from Settings.
          (Track 2 lands the real Phantom / Ping deeplink picker here.)
        </ThemedText>
        <View style={styles.spacer} />
        <Button
          label="Skip for now"
          variant="secondary"
          fullWidth
          onPress={finish}
          testID="connect-wallet-skip"
          style={styles.skip}
        />
        <Button
          label="Continue"
          variant="primary"
          fullWidth
          onPress={finish}
          testID="connect-wallet-continue"
        />
      </SafeAreaView>
    </ThemedView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1 },
  safe: { flex: 1, paddingHorizontal: Spacing.xl, paddingVertical: Spacing.xl, gap: Spacing.lg },
  spacer: { flex: 1 },
  skip: { marginBottom: Spacing.md },
});
