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

import { useCallback, useEffect, useRef, useState } from 'react';
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
import { requestMobileSession } from '@/lib/coordinator';
import {
  failActiveConnectingStep,
  tunnelToConnectState,
} from '@/lib/connection-steps';
import {
  evaluateGate,
  HANDSHAKE_TIMEOUT_MS,
  type NativeTunnelStatus,
  type TunnelState,
} from '@/lib/connection-gate';
import { formatBytes } from '@/lib/format-bytes';
import * as Clipboard from 'expo-clipboard';
import { TunnelControl, type TunnelStats } from 'iogrid-tunnel-control';

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

  // ── Status machine sync — NE status + handshake gate (#701) ──────
  //
  // The OS reports `connected` the instant the PacketTunnelProvider's
  // tunnel INTERFACE comes up — BEFORE any WireGuard handshake. A
  // black-hole tunnel (wrong/dead peer) therefore sits in OS-`connected`
  // forever while no traffic flows. We refuse to show the green
  // "Connected / Protected" affordance on OS-`connected` alone: it is
  // promoted to the user-facing CONNECTED state ONLY once a stats tick
  // proves a real handshake (recent `handshakeAge`, `received > 0`, or a
  // real `latency` probe sample). Until then we hold at VERIFYING; if no
  // evidence arrives within HANDSHAKE_TIMEOUT_MS we fail the tunnel
  // honestly. All gating lives in the pure `evaluateGate` (src/lib —
  // unit-tested); this effect just feeds it live inputs + applies the
  // verdict. The happy path is unchanged: a real handshake still reaches
  // CONNECTED (now via a stats tick rather than the bare OS status).
  const nativeStatusRef = useRef<NativeTunnelStatus>('disconnected');
  const latestStatsRef = useRef<TunnelStats | null>(null);
  const nativeConnectedAtRef = useRef<number | null>(null);
  const blackHoleTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    const clearBlackHoleTimer = () => {
      if (blackHoleTimerRef.current != null) {
        clearTimeout(blackHoleTimerRef.current);
        blackHoleTimerRef.current = null;
      }
    };

    // Adjudicate the current native status + latest evidence and apply the
    // gate verdict to the visible state. Called on every status change and
    // every stats tick, plus once when the black-hole timer fires.
    const recompute = () => {
      const nativeStatus = nativeStatusRef.current;

      // Track when the OS FIRST reported connected for this bring-up so the
      // gate can measure the black-hole timeout window.
      if (nativeStatus === 'connected') {
        if (nativeConnectedAtRef.current == null) {
          nativeConnectedAtRef.current = Date.now();
          // Arm a one-shot fallback so a tunnel that never handshakes is
          // re-evaluated (and failed) even if no further stats tick fires.
          clearBlackHoleTimer();
          blackHoleTimerRef.current = setTimeout(recompute, HANDSHAKE_TIMEOUT_MS + 250);
        }
      } else {
        nativeConnectedAtRef.current = null;
        latestStatsRef.current = null;
        clearBlackHoleTimer();
      }

      const verdict = evaluateGate({
        nativeStatus,
        latestStats: latestStatsRef.current,
        msSinceNativeConnected:
          nativeConnectedAtRef.current != null
            ? Date.now() - nativeConnectedAtRef.current
            : 0,
      });

      if (verdict.state === 'FAILED') {
        // Black-hole: OS says up but no handshake within the window. Tear
        // the dead tunnel down + tell the user honestly instead of leaving
        // a fake "Protected" (or an endless verifying spinner) on screen.
        clearBlackHoleTimer();
        nativeConnectedAtRef.current = null;
        setState('OFF');
        TunnelControl.stopTunnel().catch((e) =>
          console.warn('stopTunnel after handshake timeout failed', e),
        );
        Alert.alert(
          'Could not connect',
          'The VPN tunnel came up but never completed a secure handshake, so your traffic is not protected. Please try again.',
        );
        return;
      }

      setState(verdict.state);
    };

    const statusSub = TunnelControl.onStatusChange(({ status }) => {
      nativeStatusRef.current = status as NativeTunnelStatus;
      recompute();
    });

    // Stats ticks (#587) carry the handshake evidence the gate needs:
    // handshakeAge / received bytes / probe latency. Each tick re-runs the
    // gate so VERIFYING promotes to CONNECTED the moment evidence lands.
    const statsSub = TunnelControl.onStatsUpdate((s: TunnelStats) => {
      latestStatsRef.current = s;
      recompute();
    });

    return () => {
      statusSub.remove();
      statsSub.remove();
      clearBlackHoleTimer();
    };
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
      // #701: register the device's REAL WireGuard public key with
      // vpn-svc. The keypair is generated+persisted natively (App Group);
      // its private half is injected into the tunnel by TunnelControl, so
      // the provider can accept this device as a peer and the WG handshake
      // can complete. (Was a stub string — the provider then had no key to
      // route return traffic to, and startTunnel got an empty peer.)
      const devicePublicKey = await TunnelControl.ensureDeviceKeypair();
      const mobile = await requestMobileSession({
        apiKey: identity.accountNumberRaw,
        customerId: identity.customerId,
        region: persistedRegion,
        clientPublicKey: devicePublicKey,
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

      // #701: the mobile endpoint (#588/#605) is the canonical single-shot
      // path — it returns the fully-resolved WG peer config in one
      // round-trip: peerPublicKey + peerEndpoint (the provider's REAL
      // public IP:port after the pickEndpoint fix #702) + the assigned
      // inner IP. Start the tunnel directly with it. A 201 with an empty
      // peer_public_key means vpn-svc had no resolvable peer (e.g. the
      // phantom-provider state) — surface an honest error instead of
      // starting a tunnel with an empty peer, which the extension rejects
      // with missingField("peerPublicKey").
      if (!mobile.peerPublicKey || !mobile.peerEndpoint) {
        failActiveStep();
        await holdConnectingVisible();
        setState('OFF');
        Alert.alert(
          'Could not connect',
          'No VPN peer is available right now. Try again in a moment.',
        );
        return;
      }
      await TunnelControl.startTunnel({
        peerPublicKey: mobile.peerPublicKey,
        peerEndpoint: mobile.peerEndpoint,
        customerInnerCIDR: mobile.innerIP || '10.66.0.2/32',
        // #701 client browsing fix: full-tunnel must capture BOTH families.
        // IPv4-only (`0.0.0.0/0`) leaves the device's IPv6 traffic on the
        // raw cellular/Wi-Fi interface — on dual-stack or IPv6-only carriers
        // (most US/EU mobile networks are IPv6-NAT64) browsers prefer IPv6
        // (Happy Eyeballs), so pages either leak past the tunnel or hang
        // when the v6 path is firewalled. Adding `::/0` makes iOS install a
        // default IPv6 route on the utun too; the v4-only WG peer has no v6
        // route so those packets are dropped at the tunnel (no leak) and the
        // app re-tries over the captured IPv4 path. This matches the official
        // WireGuard-iOS "block-all / full-tunnel" allowedIPs.
        allowedIPs: '0.0.0.0/0,::/0',
        region: mobile.region,
        sessionId: mobile.sessionId,
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
      // #701: surface the REAL failing leg, not a generic sentence. The
      // founder's device reproducibly lands here while the server side is
      // verified healthy (session created, key registered, peer bound) —
      // so the only way to find the failing leg (NEVPN SAVE_FAILED /
      // RELOAD_FAILED / START_FAILED vs an HTTP/parse error) is for the
      // alert itself to say which. Same honesty pattern as #684/#690.
      const errCode = (e as { code?: string } | null)?.code;
      const errMsg = e instanceof Error ? e.message : String(e);
      console.warn(
        `[iogrid/vpn] start failed code=${errCode ?? 'none'} msg=${errMsg}`,
      );
      failActiveStep();
      await holdConnectingVisible();
      setState('OFF');
      Alert.alert(
        'Could not connect',
        `Something went wrong starting the session. Check your connection and try again.\n\nDiagnostic: ${errCode ? `[${errCode}] ` : ''}${errMsg}`,
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

  // Copy the egress IP to the clipboard with transient confirmation —
  // replaces the prior no-op stub (a Pressable labelled "Copy egress IP"
  // that did nothing was misleading-affordance UX). Matches the
  // established stats-card.tsx pattern (Clipboard.setStringAsync + a
  // 1.5s reset). Activates once Track 3 wires real stats.egressIP.
  const [ipCopied, setIpCopied] = useState(false);
  const copyEgressIP = useCallback(async () => {
    if (!stats.egressIP) return;
    try {
      await Clipboard.setStringAsync(stats.egressIP);
      setIpCopied(true);
      setTimeout(() => setIpCopied(false), 1500);
    } catch {
      // copy is a convenience, not critical — swallow
    }
  }, [stats.egressIP]);

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
                  onPress={copyEgressIP}
                  hitSlop={8}
                  accessibilityLabel={
                    ipCopied ? 'Egress IP copied' : `Copy egress IP ${stats.egressIP}`
                  }
                  accessibilityRole="button"
                >
                  <ThemedText
                    style={[
                      styles.egressIP,
                      { color: ipCopied ? theme.accent : theme.textSecondary },
                    ]}
                  >
                    {ipCopied ? '✓ Copied' : stats.egressIP}
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
