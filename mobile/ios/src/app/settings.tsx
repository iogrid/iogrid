// Settings screen — SectionList with ACCOUNT / CONNECTION / ABOUT (#593).
//
// Wireframe ref: mobile/ios/docs/ux-wireframes-v2.md Screen 9.
//
// Sign-out clears Keychain → navigate /onboarding/welcome.

import { useEffect, useState } from 'react';
import {
  Alert,
  Linking,
  ScrollView,
  StyleSheet,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import AsyncStorage from '@react-native-async-storage/async-storage';
import Constants from 'expo-constants';
import { useRouter } from 'expo-router';

import { SettingsRow } from '@/components/settings-row';
import { ThemedText } from '@/components/themed-text';
import { ThemedView } from '@/components/themed-view';
import { Spacing } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';
import { auth, type AppleSession } from '@/lib/auth';
import { wallet, type WalletBalance, type WalletState } from '@/lib/wallet';

const AUTOCONNECT_KEY = 'iogrid.settings.autoConnect';
const KILLSWITCH_KEY = 'iogrid.settings.killSwitch';
const DNSLEAK_KEY = 'iogrid.settings.dnsLeak';

export default function SettingsScreen() {
  const router = useRouter();
  const theme = useTheme();
  const [session, setSession] = useState<AppleSession | null>(null);
  const [walletState, setWalletState] = useState<WalletState | null>(null);
  const [balance, setBalance] = useState<WalletBalance>({ balanceGrid: 0, balanceUsd: 0 });
  const [autoConnect, setAutoConnect] = useState(false);
  const [killSwitch, setKillSwitch] = useState(true);
  const [dnsLeak, setDnsLeak] = useState(true);

  useEffect(() => {
    auth.getStoredSession().then(setSession).catch(() => undefined);
    wallet.getStored().then(setWalletState).catch(() => undefined);
    wallet.getBalance().then(setBalance).catch(() => undefined);
    AsyncStorage.getItem(AUTOCONNECT_KEY).then((v) => v === '1' && setAutoConnect(true));
    AsyncStorage.getItem(KILLSWITCH_KEY).then((v) => v !== null && setKillSwitch(v === '1'));
    AsyncStorage.getItem(DNSLEAK_KEY).then((v) => v !== null && setDnsLeak(v === '1'));
  }, []);

  const setToggle =
    (key: string, set: (v: boolean) => void) => (v: boolean) => {
      set(v);
      AsyncStorage.setItem(key, v ? '1' : '0').catch(() => undefined);
    };

  const onSignOut = () => {
    Alert.alert('Sign out', 'You will need to sign in again to use iogrid.', [
      { text: 'Cancel', style: 'cancel' },
      {
        text: 'Sign out',
        style: 'destructive',
        onPress: async () => {
          await auth.signOut();
          await wallet.disconnect();
          router.replace('/(onboarding)/welcome');
        },
      },
    ]);
  };

  const version = Constants.expoConfig?.version ?? '0.0.0';

  return (
    <ThemedView style={styles.container}>
      <SafeAreaView style={styles.safe} edges={['bottom']}>
        <ScrollView contentContainerStyle={styles.scroll}>
          <SectionHeader>ACCOUNT</SectionHeader>
          <SettingsRow
            testID="settings-row-apple"
            label="Signed in as"
            sublabel={session?.email ?? 'Not signed in'}
            navigable
            onPress={() => undefined}
          />
          <SettingsRow
            testID="settings-row-wallet"
            label={walletState ? `Wallet (${walletState.provider})` : 'Wallet'}
            sublabel={
              walletState
                ? `${balance.balanceGrid.toLocaleString()} $GRID ≈ $${balance.balanceUsd.toFixed(2)}`
                : 'Not connected'
            }
            navigable
            onPress={() => router.push('/(onboarding)/connect-wallet')}
          />

          <SectionHeader>CONNECTION</SectionHeader>
          <SettingsRow
            testID="settings-row-autoconnect"
            label="Auto-connect"
            sublabel="Connect on app launch"
            toggle={{
              value: autoConnect,
              onChange: setToggle(AUTOCONNECT_KEY, setAutoConnect),
            }}
          />
          <SettingsRow
            testID="settings-row-killswitch"
            label="Kill switch"
            sublabel="Block traffic if the tunnel drops"
            toggle={{
              value: killSwitch,
              onChange: setToggle(KILLSWITCH_KEY, setKillSwitch),
            }}
          />
          <SettingsRow
            testID="settings-row-dnsleak"
            label="DNS-leak protection"
            sublabel="Route DNS through the tunnel"
            toggle={{
              value: dnsLeak,
              onChange: setToggle(DNSLEAK_KEY, setDnsLeak),
            }}
          />

          <SectionHeader>ABOUT</SectionHeader>
          <SettingsRow
            testID="settings-row-privacy"
            label="Privacy Policy"
            navigable
            onPress={() => Linking.openURL('https://iogrid.org/privacy').catch(() => undefined)}
          />
          <SettingsRow
            testID="settings-row-terms"
            label="Terms of Service"
            navigable
            onPress={() => Linking.openURL('https://iogrid.org/terms').catch(() => undefined)}
          />
          <SettingsRow testID="settings-row-version" label="Version" value={version} />
          <SettingsRow
            testID="settings-row-signout"
            label="Sign out"
            destructive
            onPress={onSignOut}
          />

          <View style={styles.footer}>
            <ThemedText type="caption" color={theme.textTertiary}>
              iogrid mobile {version}
            </ThemedText>
          </View>
        </ScrollView>
      </SafeAreaView>
    </ThemedView>
  );
}

function SectionHeader({ children }: { children: string }) {
  const theme = useTheme();
  return (
    <ThemedText
      type="caption"
      color={theme.textSecondary}
      style={styles.sectionHeader}
    >
      {children}
    </ThemedText>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1 },
  safe: { flex: 1 },
  scroll: {
    paddingHorizontal: Spacing.lg,
    paddingVertical: Spacing.md,
    gap: Spacing.sm,
  },
  sectionHeader: {
    paddingTop: Spacing.xl,
    paddingBottom: Spacing.sm,
    textTransform: 'uppercase',
    letterSpacing: 1.5,
  },
  footer: { paddingTop: Spacing.xxl, alignItems: 'center' },
});
