// WalletCard — primitive that renders the bound wallet + $GRID balance.
// Used on the main screen (DISCONNECTED / CONNECTED states from
// wireframes-v2 Screen 5 + 7) and pumped into Settings → Wallet by
// Track 4. Self-contained: takes the wallet binding as props, polls
// the balance internally via {@link useGridBalance}, surfaces the
// "Top up" CTA + manual refresh button.

import { useCallback } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { ThemedText } from '@/components/themed-text';
import { Spacing } from '@/constants/theme';
import { formatGridBalance } from '@/lib/grid_balance';
import {
  truncateAddress,
  walletFor,
  type WalletProvider,
} from '@/lib/wallets';
import { useGridBalance } from '@/hooks/use_grid_balance';

export interface WalletBindingPropsShape {
  walletAddress: string;
  walletProvider: WalletProvider;
}

export interface WalletCardProps {
  binding: WalletBindingPropsShape | null;
  /** Called when the user taps "Top up". Deeplinks back to the bound
   *  wallet app (handled by the caller — usually opens the wallet's
   *  receive/buy screen via its scheme). */
  onTopUp?: () => void;
  /** Called when the user taps "Connect wallet" because no binding
   *  has been set up yet. Track 4 wires this to navigation to the
   *  /(onboarding)/connect-wallet screen. */
  onConnect?: () => void;
}

export function WalletCard({ binding, onTopUp, onConnect }: WalletCardProps) {
  const { balance, isFetching, refetch, isLow } = useGridBalance({
    walletAddress: binding?.walletAddress ?? null,
  });

  const openTopUp = useCallback(() => {
    if (!binding) return;
    if (onTopUp) {
      onTopUp();
      return;
    }
    // Default behaviour: deeplink back to the bound wallet.
    void walletFor(binding.walletProvider);
  }, [binding, onTopUp]);

  if (!binding) {
    return (
      <View testID="wallet-card-empty" style={styles.card}>
        <ThemedText type="small" themeColor="textSecondary">
          Wallet
        </ThemedText>
        <ThemedText type="default">No wallet connected</ThemedText>
        <Pressable
          testID="wallet-connect-cta"
          style={styles.topUpButton}
          onPress={onConnect}
          accessibilityRole="button"
          accessibilityLabel="Connect wallet"
        >
          <ThemedText type="smallBold">Connect wallet</ThemedText>
        </Pressable>
      </View>
    );
  }

  return (
    <View testID="wallet-card" style={styles.card}>
      <View style={styles.headerRow}>
        <ThemedText type="small" themeColor="textSecondary">
          Wallet · {binding.walletProvider}
        </ThemedText>
        <Pressable
          testID="wallet-refresh"
          onPress={refetch}
          accessibilityRole="button"
          accessibilityLabel="Refresh balance"
          hitSlop={12}
        >
          <ThemedText type="small" themeColor="textSecondary">
            {isFetching ? '…' : '↻'}
          </ThemedText>
        </Pressable>
      </View>
      <ThemedText type="default" testID="wallet-balance">
        {formatGridBalance(balance)}
      </ThemedText>
      <ThemedText type="small" themeColor="textSecondary" testID="wallet-address">
        {truncateAddress(binding.walletAddress)}
      </ThemedText>
      {isLow ? (
        <View testID="wallet-low-banner" style={styles.lowBanner}>
          <ThemedText type="smallBold">Top up to connect</ThemedText>
          <ThemedText type="small" themeColor="textSecondary">
            Balance below 0.001 $GRID — one minute of VPN.
          </ThemedText>
        </View>
      ) : null}
      <Pressable
        testID="wallet-topup"
        style={styles.topUpButton}
        onPress={openTopUp}
        accessibilityRole="button"
        accessibilityLabel="Top up wallet"
      >
        <ThemedText type="smallBold">Top up ›</ThemedText>
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  card: {
    padding: Spacing.three,
    borderRadius: 12,
    backgroundColor: 'rgba(127, 127, 127, 0.1)',
    gap: Spacing.two,
    marginVertical: Spacing.two,
  },
  headerRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  topUpButton: {
    marginTop: Spacing.two,
    paddingVertical: Spacing.two,
    paddingHorizontal: Spacing.three,
    borderRadius: 8,
    backgroundColor: 'rgba(127, 127, 127, 0.15)',
    alignSelf: 'flex-start',
  },
  lowBanner: {
    padding: Spacing.two,
    borderRadius: 8,
    backgroundColor: 'rgba(255, 184, 0, 0.15)',
    gap: Spacing.half,
  },
});
