// /onboarding/connect-wallet — wireframes-v2 Screen 3 + 4 successor.
// After Sign in with Apple lands the user picks ONE wallet to hold
// their $GRID balance. The screen offers Phantom (Solana power-user)
// and Ping (openova-group consumer app) side-by-side.
//
// Flow on tap:
//   1. Build a fresh bind challenge (BindChallenge from lib/wallets).
//   2. Call wallet.connectAndSign(challenge) — opens the wallet via
//      deeplink, user approves, wallet returns with (address, signature).
//   3. POST /v1/identity/wallet/bind to identity-svc with the
//      signature; on 200 the row in customer_wallet_bindings is saved.
//   4. Navigate back to the main screen so the wallet card renders.
//
// Errors render an inline "Try again" surface; cancel from the wallet
// app yields the same. Ping-not-installed renders a "Get Ping" link
// to the App Store; ditto Phantom.

import { useCallback, useState } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { router } from 'expo-router';
import * as Linking from 'expo-linking';

import { ThemedText } from '@/components/themed-text';
import { ThemedView } from '@/components/themed-view';
import { Spacing } from '@/constants/theme';
import {
  buildBindChallenge,
  walletFor,
  type WalletProvider,
} from '@/lib/wallets';
import { bindWalletToCustomer } from '@/lib/coordinator';

type Status =
  | { kind: 'idle' }
  | { kind: 'connecting'; provider: WalletProvider }
  | { kind: 'error'; provider: WalletProvider; message: string };

export default function ConnectWalletScreen() {
  const [status, setStatus] = useState<Status>({ kind: 'idle' });

  const handleConnect = useCallback(async (provider: WalletProvider) => {
    setStatus({ kind: 'connecting', provider });
    const wallet = walletFor(provider);
    try {
      const installed = await wallet.isInstalled();
      if (!installed) {
        // Open the App Store rather than show a half-broken error —
        // user comes back and taps again after install.
        await Linking.openURL(wallet.appStoreURL());
        setStatus({
          kind: 'error',
          provider,
          message: `Install ${provider} from the App Store, then try again.`,
        });
        return;
      }
      const challenge = await buildBindChallenge();
      const result = await wallet.connectAndSign(challenge);
      await bindWalletToCustomer({
        walletAddress: result.address,
        walletProvider: result.provider,
        challenge: result.challenge.message,
        signature: result.signatureBase58,
      });
      // Success — go to main screen.
      router.replace('/');
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      setStatus({ kind: 'error', provider, message });
    }
  }, []);

  return (
    <ThemedView style={styles.container}>
      <SafeAreaView style={styles.safe}>
        <View style={styles.header}>
          <ThemedText type="title">Connect wallet</ThemedText>
          <ThemedText type="default" themeColor="textSecondary">
            Pick one wallet to hold your $GRID balance. You can switch
            later from Settings.
          </ThemedText>
        </View>

        <View style={styles.options}>
          <WalletButton
            label="Connect Phantom"
            subtitle="Solana · self-custody"
            testID="connect-phantom"
            disabled={status.kind === 'connecting'}
            busy={status.kind === 'connecting' && status.provider === 'phantom'}
            onPress={() => handleConnect('phantom')}
          />
          <WalletButton
            label="Connect Ping"
            subtitle="ping.cash · easy top-up"
            testID="connect-ping"
            disabled={status.kind === 'connecting'}
            busy={status.kind === 'connecting' && status.provider === 'ping'}
            onPress={() => handleConnect('ping')}
          />
        </View>

        {status.kind === 'error' ? (
          <View testID="connect-error" style={styles.errorBox}>
            <ThemedText type="smallBold">
              {status.provider} connect failed
            </ThemedText>
            <ThemedText type="small" themeColor="textSecondary">
              {status.message}
            </ThemedText>
          </View>
        ) : null}

        <ThemedText type="small" themeColor="textSecondary" style={styles.footer}>
          iogrid signs a one-time challenge to prove you control the
          wallet. We never see your private key.
        </ThemedText>
      </SafeAreaView>
    </ThemedView>
  );
}

function WalletButton({
  label,
  subtitle,
  testID,
  disabled,
  busy,
  onPress,
}: {
  label: string;
  subtitle: string;
  testID: string;
  disabled?: boolean;
  busy?: boolean;
  onPress: () => void;
}) {
  return (
    <Pressable
      testID={testID}
      onPress={onPress}
      disabled={disabled}
      style={[styles.walletButton, disabled && styles.walletButtonDisabled]}
      accessibilityRole="button"
      accessibilityLabel={label}
      accessibilityState={{ disabled, busy }}
    >
      <View>
        <ThemedText type="default">{busy ? `${label} …` : label}</ThemedText>
        <ThemedText type="small" themeColor="textSecondary">
          {subtitle}
        </ThemedText>
      </View>
      <ThemedText type="default">›</ThemedText>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1 },
  safe: { flex: 1, paddingHorizontal: Spacing.three, gap: Spacing.four },
  header: { paddingTop: Spacing.three, gap: Spacing.two },
  options: { gap: Spacing.two },
  walletButton: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingVertical: Spacing.three,
    paddingHorizontal: Spacing.three,
    borderRadius: 12,
    backgroundColor: 'rgba(127, 127, 127, 0.12)',
  },
  walletButtonDisabled: { opacity: 0.5 },
  errorBox: {
    padding: Spacing.two,
    borderRadius: 8,
    backgroundColor: 'rgba(255, 64, 64, 0.12)',
    gap: Spacing.half,
  },
  footer: { marginTop: 'auto', paddingBottom: Spacing.three },
});
