// Settings screen — surfaces the Mullvad-style anonymous account
// number (#569). The number is the user's ONLY recovery key — no
// email, no password — so we display it prominently with a "copy to
// clipboard" affordance so users can save it to their password
// manager.

import { useEffect, useState } from 'react';
import { Alert, Pressable, StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import * as Clipboard from 'expo-clipboard';

import { ThemedText } from '@/components/themed-text';
import { ThemedView } from '@/components/themed-view';
import { Spacing } from '@/constants/theme';
import { loadOrCreateIdentity, type Identity } from '@/lib/account';

export default function SettingsScreen() {
  const [identity, setIdentity] = useState<Identity | null>(null);

  useEffect(() => {
    loadOrCreateIdentity().then(setIdentity).catch((e) => {
      // If Keychain access fails at startup, fall back to a sentinel
      // so the screen still renders SOMETHING. The next launch will
      // retry. We do NOT show an alert here because Keychain failures
      // are rare + non-actionable for the user.
      console.warn('loadOrCreateIdentity failed', e);
      setIdentity({
        accountNumberRaw: '0000000000000000',
        accountNumberDisplay: '0000 0000 0000 0000',
        customerId: '00000000-0000-4000-8000-000000000000',
      });
    });
  }, []);

  const onCopy = async () => {
    if (!identity) return;
    await Clipboard.setStringAsync(identity.accountNumberRaw);
    Alert.alert('Copied', 'Account number copied to clipboard.');
  };

  return (
    <ThemedView style={styles.container}>
      <SafeAreaView style={styles.safe}>
        <ThemedText type="title">Settings</ThemedText>

        <View style={styles.section}>
          <ThemedText type="default">Account number</ThemedText>
          <Pressable onPress={onCopy} accessibilityRole="button">
            <ThemedText type="default" selectable style={styles.accountNumber}>
              {identity?.accountNumberDisplay ?? 'Loading…'}
            </ThemedText>
            <ThemedText type="small">Tap to copy</ThemedText>
          </Pressable>
        </View>

        <ThemedText type="small">
          Your account number is the only identifier iogrid has for you.
          No email. No password. Save it somewhere safe — it&apos;s how
          you recover access on a new device.
        </ThemedText>
      </SafeAreaView>
    </ThemedView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1 },
  safe: { flex: 1, paddingHorizontal: Spacing.three, gap: 16 },
  section: {
    paddingVertical: 16,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(127, 127, 127, 0.2)',
    gap: 8,
  },
  accountNumber: {
    fontVariant: ['tabular-nums'],
    fontSize: 22,
    letterSpacing: 2,
    fontWeight: '500',
    paddingVertical: 4,
  },
});
