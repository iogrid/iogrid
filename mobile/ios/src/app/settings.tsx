// Settings — stub for #567 bootstrap so the Maestro smoke flow's
// `settings-button` tap lands on a screen with "Account number" text.
// Full account-ID generator + Keychain wire + sign-in-to-recover lives
// in #569.

import { StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

import { ThemedText } from '@/components/themed-text';
import { ThemedView } from '@/components/themed-view';
import { Spacing } from '@/constants/theme';

export default function SettingsScreen() {
  // Placeholder account number — replaced by the Keychain-backed
  // generator in #569.
  const accountNumber = '0000 0000 0000 0000';

  return (
    <ThemedView style={styles.container}>
      <SafeAreaView style={styles.safe}>
        <ThemedText type="title">Settings</ThemedText>

        <View style={styles.row}>
          <ThemedText type="default">Account number</ThemedText>
          <ThemedText type="default" selectable>
            {accountNumber}
          </ThemedText>
        </View>

        <ThemedText type="small">
          Your account number is the only identifier iogrid has for you.
          No email. No password. Keep it safe — it&apos;s how you recover
          access on a new device. (Full Keychain wiring lands in #569.)
        </ThemedText>
      </SafeAreaView>
    </ThemedView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1 },
  safe: { flex: 1, paddingHorizontal: Spacing.three, gap: 16 },
  row: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    paddingVertical: 16,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(127, 127, 127, 0.2)',
  },
});
