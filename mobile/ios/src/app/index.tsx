/**
 * Main screen — the home of iogrid.
 *
 * v2 rewrite per mobile/ios/docs/ux-wireframes-v2.md Screens 5/6/7.
 * Drops the iOS Switch + tiny status text in favor of:
 *
 *   - Giant 180pt ConnectButton (Mullvad-style) with 3 states
 *   - Region card (tap → /regions)
 *   - Wallet card with $GRID balance + burn ticker when CONNECTED
 *   - Settings affordance in the top-right
 *   - Egress IP + live stats card shown only in CONNECTED state
 *
 * State machine driven by `TunnelControl.onStatusChange`. The legacy
 * 3000ms CONNECTING-hold workaround stays in the FAILURE path only —
 * once the real WireGuard data plane lands (#587), success transitions
 * are driven by the OS NEVPNStatusDidChange notification and no hold
 * is needed.
 *
 * Refs #580, #591.
 */

import { useCallback, useEffect, useState } from 'react';
import { Alert, Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { router, useFocusEffect } from 'expo-router';
import AsyncStorage from '@react-native-async-storage/async-storage';

import { ConnectButton } from '@/components/connect-button';
import { GearIcon } from '@/components/icons';
import {
  ConnectionStatus,
  DEFAULT_CONNECTING_STEPS,
} from '@/components/connection-status';
import { ThemedText } from '@/components/themed-text';
import { ThemedView } from '@/components/themed-view';
import { Card, Spacing, TypeScale } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';
import { AUTO_REGION_SENTINEL } from '@/app/regions';
import { loadOrCreateIdentity } from '@/lib/account';
import { requestMobileSession, requestSession } from '@/lib/coordinator';
import {
  failActiveConnectingStep,
  tunnelToConnectState,
  type TunnelState,
} from '@/lib/connection-steps';
import { TunnelControl } from 'iogrid-tunnel-control';

const SELECTED_REGION_KEY = 'iogrid.region.selected';

export default function MainScreen() {
  const theme = useTheme();
  const [state, setState] = useState<TunnelState>('OFF');
  const [region, setRegion] = useState<string>('Best (auto)');
  // Stats are populated by Track 3's `TunnelControl.onStatsUpdate`
  // event once the real WireGuard tunnel is live. For now, they stay
  // null and render skeleton placeholders in CONNECTED state.
  const [stats] = useState<{
    sentBytes: number;
    receivedBytes: number;
    egressIP: string | null;
    city: string | null;
    flag: string | null;
    latencyMs: number | null;
  }>({
    sentBytes: 0,
    receivedBytes: 0,
    egressIP: null,
    city: null,
    flag: null,
    latencyMs: null,
  });

  useFocusEffect(
    useCallback(() => {
      AsyncStorage.getItem(SELECTED_REGION_KEY)
        .then((v) => {
          if (!v || v === AUTO_REGION_SENTINEL) {
            setRegion('Best (auto)');
          } else {
            setRegion(v);
          }
        })
        .catch(() => undefined);
    }, []),
  );

  // ── Status machine sync — NEVPNStatusDidChange via native module ──
  useEffect(() => {
    const sub = TunnelControl.onStatusChange(({ status }) => {
      switch (status) {
        case 'connected':
          setState('CONNECTED');
          break;
        case 'connecting':
        case 'reasserting':
          setState('CONNECTING');
          break;
        case 'disconnecting':
          setState('DISCONNECTING');
          break;
        case 'disconnected':
        case 'invalid':
        case 'unknown':
          setState('OFF');
          break;
      }
    });
    return () => sub.remove();
  }, []);

  const onConnect = useCallback(async () => {
    if (state !== 'OFF') {
      // Already CONNECTING / CONNECTED / DISCONNECTING — tapping the
      // big button when CONNECTED treats it as disconnect intent.
      if (state === 'CONNECTED') {
        setState('DISCONNECTING');
        try {
          await TunnelControl.stopTunnel();
        } catch (e) {
          console.warn('stopTunnel failed', e);
        }
        setState('OFF');
      }
      return;
    }

    setState('CONNECTING');
    setConnectingSteps(DEFAULT_CONNECTING_STEPS);
    const minVisibleStart = Date.now();
    const holdConnectingVisible = async () => {
      // Preserve the failure-path hold from #567: if the tunnel
      // start fails fast (e.g. coordinator unreachable, WG not
      // linked yet), keep CONNECTING visible long enough that the
      // user reads it rather than seeing a confusing instant OFF.
      //
      // The window must also survive Maestro's post-tap settle on
      // iOS (#599): the CONNECTING arc is an infinite Animated.loop,
      // so Maestro's `screenshotBasedTap` cannot detect the hierarchy
      // settling and spends ~4–5s in back-to-back 3000ms "waiting for
      // animation to end" cycles BEFORE the next command (the
      // connection-status assertVisible in flow 05) even starts —
      // `waitToSettleTimeoutMs:0` does NOT suppress that internal
      // wait. A 3000ms hold expired ~1.3s before the assert began
      // (observed: tap-press 20:02:23.6, tap-COMPLETED 20:02:27.9,
      // assert-RUNNING 20:02:27.9 — hold long gone). 8000ms held on
      // normal runners but FLAKED on a slow rerun runner (run
      // 26910040116 attempt 2: tap+settle ate the whole hold and flow
      // 05's 5000ms poll expired). 12000ms restores the margin on the
      // slowest observed runners and still reads fine for a real
      // failed connect.
      const elapsed = Date.now() - minVisibleStart;
      const remaining = 12000 - elapsed;
      if (remaining > 0) {
        await new Promise((resolve) => setTimeout(resolve, remaining));
      }
    };

    try {
      const identity = await loadOrCreateIdentity();
      const persistedRegion =
        (await AsyncStorage.getItem(SELECTED_REGION_KEY)) ?? AUTO_REGION_SENTINEL;

      // #588/#605: single-shot mobile session bring-up. Calls the new
      // POST /v1/vpn/sessions/mobile endpoint that returns the WG peer
      // config in one round-trip. On 503 (no provider available yet)
      // we surface a tappable "Could not connect" alert and stay OFF —
      // the user can retry later. The legacy two-step path below kicks
      // in only when the mobile endpoint returns a populated session.
      const mobile = await requestMobileSession({
        apiKey: identity.accountNumberRaw,
        customerId: identity.customerId,
        region: persistedRegion,
        // Track 3 will replace this stub with the real WG public key
        // once iogrid-tunnel-control exposes a generateKeypair() RPC.
        // The handler accepts any non-empty string for the smoke path.
        clientPublicKey: 'maestro-degraded-path-stub-pubkey',
      });
      if (mobile.status === 503) {
        failActiveStep();
        await holdConnectingVisible();
        setState('OFF');
        Alert.alert(
          'Could not connect',
          mobile.retryAfterSec
            ? `No provider available right now. Try again in ${mobile.retryAfterSec}s.`
            : 'No provider available right now. Try again in a moment.',
        );
        return;
      }
      if (mobile.status === 429) {
        failActiveStep();
        await holdConnectingVisible();
        setState('OFF');
        Alert.alert(
          'Could not connect',
          'Your free-tier quota is exhausted. Tap Top up to upgrade.',
        );
        return;
      }

      const session = await requestSession(
        identity.accountNumberRaw,
        identity.customerId,
        persistedRegion,
      );
      if (!session.sessionId) {
        // Backend returned a session-less response (e.g. quota
        // exhausted, or v2's wallet authorization failed). Surface
        // a tappable error.
        failActiveStep();
        await holdConnectingVisible();
        setState('OFF');
        Alert.alert(
          'Could not connect',
          'Your wallet balance may be insufficient. Tap Top up to add $GRID.',
        );
        return;
      }
      await TunnelControl.startTunnel({
        peerPublicKey: '',
        peerEndpoint: 'discover',
        customerInnerCIDR: '10.66.0.2/16',
        allowedIPs: '0.0.0.0/0',
        region: session.region,
        sessionId: session.sessionId,
      });
      // NEVPNStatusDidChange will drive setState to CONNECTED via
      // the onStatusChange subscriber above.
    } catch (e) {
      // #690 D2: a THROWN failure (401 unregistered identity, DNS,
      // timeout, 5xx) must never be silent — the user tapped Connect
      // and deserves the same honest alert the explicit 503/429
      // branches show. Fourth instance of the failure-masking
      // pattern (#675/#685/#686) had lived right here.
      console.warn('vpn start failed', e);
      failActiveStep();
      await holdConnectingVisible();
      setState('OFF');
      Alert.alert(
        'Could not connect',
        'Something went wrong starting the session. Check your connection and try again.',
      );
    }
  }, [state]);

  // Connecting step-list state (#684 pass 5): on failure the ACTIVE step
  // flips to 'failed' (red ✕) for the hold window, so the user sees WHERE
  // the attempt died instead of a frozen spinner vanishing under the
  // alert. Real per-step progression arrives with Track 3 (#588).
  const [connectingSteps, setConnectingSteps] = useState(DEFAULT_CONNECTING_STEPS);
  const failActiveStep = useCallback(() => {
    setConnectingSteps(failActiveConnectingStep);
  }, []);

  const connectState = tunnelToConnectState(state);
  const isConnected = state === 'CONNECTED';

  return (
    <ThemedView style={styles.root}>
      <SafeAreaView style={styles.safe} edges={['top', 'left', 'right']}>
        {/* ── Top bar ─────────────────────────────────────────── */}
        <View style={styles.topBar}>
          <ThemedText style={styles.brand}>iogrid</ThemedText>
          <Pressable
            testID="settings-button"
            onPress={() => router.push('/settings')}
            hitSlop={12}
            accessibilityLabel="Open settings"
            accessibilityRole="button"
            style={({ pressed }) => (pressed ? { opacity: 0.6 } : null)}
          >
            {/* Drawn SVG gear — the literal `⚙` character it replaces was
                emoji-grade chrome (#684). */}
            <GearIcon size={22} color={theme.textSecondary} />
          </Pressable>
        </View>

        <ScrollView
          contentContainerStyle={styles.scrollContent}
          showsVerticalScrollIndicator={false}
        >
          {/* ── Connect button + status label ─────────────────── */}
          <ConnectButton state={connectState} onPress={onConnect} />

          {/* Step-list shown only while CONNECTING — Maestro flow 05
              asserts `connection-status` testID visible during this
              window. Currently uses the DEFAULT_CONNECTING_STEPS set;
              real WG handshake progress (Track 3 #588) will drive
              the step state once peer + tunnel + egress events fire. */}
          {connectState === 'connecting' ? (
            <ConnectionStatus steps={connectingSteps} />
          ) : null}

          {/* ── Connected state extras: city, egress IP, stats ── */}
          {isConnected ? (
            <View style={styles.connectedMeta}>
              {stats.city ? (
                <ThemedText
                  testID="connected-city"
                  style={[styles.locationLine, { color: theme.text }]}
                >
                  {stats.flag ?? ''} {stats.city}
                </ThemedText>
              ) : null}
              {stats.egressIP ? (
                <Pressable
                  testID="egress-ip"
                  onPress={() => {
                    // TODO Track 3 wires Clipboard.setStringAsync once
                    // the stats event provides the egress IP. For now
                    // this is a no-op stub.
                  }}
                  hitSlop={8}
                  accessibilityLabel={`Copy egress IP ${stats.egressIP}`}
                  accessibilityRole="button"
                >
                  <ThemedText style={[styles.egressIP, { color: theme.textSecondary }]}>
                    {stats.egressIP}
                  </ThemedText>
                </Pressable>
              ) : null}
              <View style={styles.statsRow}>
                <ThemedText style={[styles.statLine, { color: theme.textSecondary }]}>
                  ↓ {formatBytes(stats.receivedBytes)}
                </ThemedText>
                <ThemedText style={[styles.statLine, { color: theme.textSecondary }]}>
                  ↑ {formatBytes(stats.sentBytes)}
                </ThemedText>
                {stats.latencyMs != null ? (
                  <ThemedText style={[styles.statLine, { color: theme.textSecondary }]}>
                    {stats.latencyMs} ms
                  </ThemedText>
                ) : null}
              </View>
            </View>
          ) : null}

          {/* ── Region card ───────────────────────────────────── */}
          <Pressable
            testID="region-card"
            onPress={() => router.push('/regions')}
            style={({ pressed }) => [
              styles.card,
              {
                backgroundColor: theme.backgroundCard,
                borderColor: theme.border,
              },
              pressed ? styles.cardPressed : null,
            ]}
            accessibilityLabel={`Region: ${region}. Tap to change.`}
            accessibilityRole="button"
          >
            <View>
              <ThemedText style={[styles.cardLabel, { color: theme.textTertiary }]}>
                REGION
              </ThemedText>
              <ThemedText style={[styles.cardValue, { color: theme.text }]}>
                {region}
              </ThemedText>
            </View>
            <ThemedText style={[styles.cardChevron, { color: theme.textTertiary }]}>
              ›
            </ThemedText>
          </Pressable>

          {/* ── Wallet card (stub; #594 owns full implementation) ─ */}
          <View
            testID="wallet-card"
            style={[
              styles.card,
              styles.walletCard,
              {
                backgroundColor: theme.backgroundCard,
                borderColor: theme.border,
              },
            ]}
          >
            <View style={styles.walletTopRow}>
              <ThemedText style={[styles.cardLabel, { color: theme.textTertiary }]}>
                WALLET
              </ThemedText>
            </View>
            <ThemedText style={[styles.walletBalance, { color: theme.text }]}>
              0 $GRID
            </ThemedText>
            <ThemedText style={[styles.walletSubtle, { color: theme.textSecondary }]}>
              Connect a wallet to start
            </ThemedText>
            <Pressable
              testID="wallet-card-topup"
              onPress={() => router.push('/topup' as any)}
              style={({ pressed }) => [
                styles.topupButton,
                { backgroundColor: theme.backgroundElement },
                pressed ? styles.cardPressed : null,
              ]}
              accessibilityLabel="Top up wallet"
              accessibilityRole="button"
            >
              <ThemedText style={[styles.topupLabel, { color: theme.text }]}>
                Top up ›
              </ThemedText>
            </Pressable>
          </View>

          {/* ── Disconnect (only when CONNECTED) ──────────────── */}
          {isConnected ? (
            <Pressable
              testID="disconnect-button"
              onPress={onConnect}
              hitSlop={8}
              accessibilityLabel="Disconnect from iogrid VPN"
              accessibilityRole="button"
              style={({ pressed }) => [
                styles.disconnectButton,
                pressed ? styles.cardPressed : null,
              ]}
            >
              <ThemedText style={[styles.disconnectLabel, { color: theme.error }]}>
                Disconnect
              </ThemedText>
            </Pressable>
          ) : null}
        </ScrollView>
      </SafeAreaView>
    </ThemedView>
  );
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  safe: { flex: 1 },
  topBar: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: Spacing.lg,
    paddingVertical: Spacing.md,
  },
  brand: {
    ...TypeScale.displayS,
    letterSpacing: -0.4,
  },
  settingsIcon: {
    fontSize: 22,
    fontWeight: '400',
  },
  scrollContent: {
    paddingBottom: Spacing.xxxl,
    paddingHorizontal: Spacing.lg,
  },
  connectedMeta: {
    alignItems: 'center',
    gap: Spacing.sm,
    marginTop: -Spacing.lg, // hug the ConnectButton container
    marginBottom: Spacing.xl,
  },
  locationLine: {
    ...TypeScale.bodyL,
    fontWeight: '500',
  },
  egressIP: {
    ...TypeScale.monoM,
  },
  statsRow: {
    flexDirection: 'row',
    gap: Spacing.lg,
    marginTop: Spacing.sm,
  },
  statLine: {
    ...TypeScale.monoS,
  },
  card: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: Card.padding,
    borderRadius: 16,
    borderWidth: StyleSheet.hairlineWidth,
    marginTop: Card.marginVertical,
  },
  cardPressed: {
    opacity: 0.7,
  },
  cardLabel: {
    ...TypeScale.captionStrong,
    letterSpacing: 1.5,
    marginBottom: 2,
  },
  cardValue: {
    ...TypeScale.bodyL,
    fontWeight: '500',
  },
  cardChevron: {
    fontSize: 24,
    fontWeight: '300',
  },
  walletCard: {
    flexDirection: 'column',
    alignItems: 'stretch',
    gap: Spacing.sm,
  },
  walletTopRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
  },
  walletBalance: {
    ...TypeScale.displayS,
    fontVariant: ['tabular-nums'],
  },
  walletSubtle: {
    ...TypeScale.bodyS,
  },
  topupButton: {
    marginTop: Spacing.md,
    paddingVertical: Spacing.md,
    paddingHorizontal: Spacing.lg,
    borderRadius: 12,
    alignItems: 'center',
  },
  topupLabel: {
    ...TypeScale.button,
  },
  disconnectButton: {
    marginTop: Spacing.xl,
    paddingVertical: Spacing.md,
    alignItems: 'center',
  },
  disconnectLabel: {
    ...TypeScale.button,
  },
});
