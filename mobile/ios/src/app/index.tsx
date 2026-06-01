// VPN toggle screen — primary surface of the iogrid mobile app.
//
// Scope of #567 (bootstrap): render the toggle + region row + settings
// entry point with the testIDs the Maestro smoke flow asserts on. The
// toggle's actual data plane (PacketTunnelProvider, WG handshake,
// coordinator session POST) lands in #568 + #569 + #570 — for now
// toggling locally drives the OFF → CONNECTING state transition on
// the JS side only, so the smoke flow asserts state changes without
// a live provider.

import { useEffect, useState } from 'react';
import { Pressable, StyleSheet, Switch, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { router, useFocusEffect } from 'expo-router';
import { useCallback } from 'react';
import AsyncStorage from '@react-native-async-storage/async-storage';

import { ThemedText } from '@/components/themed-text';
import { ThemedView } from '@/components/themed-view';
import { QuotaBanner, type QuotaState } from '@/components/quota-banner';
import { Spacing } from '@/constants/theme';
import { AUTO_REGION_SENTINEL } from '@/app/regions';
import { loadOrCreateIdentity } from '@/lib/account';
import { requestSession } from '@/lib/coordinator';
import { TunnelControl } from '../../modules/TunnelControl/src';

type TunnelState = 'OFF' | 'CONNECTING' | 'CONNECTED' | 'DISCONNECTING';

const SELECTED_REGION_KEY = 'iogrid.region.selected';

export default function VPNToggleScreen() {
  const [state, setState] = useState<TunnelState>('OFF');
  const [region, setRegion] = useState<string>('Best (auto)');
  const [quotaState, setQuotaState] = useState<QuotaState>('QUOTA_STATE_UNSPECIFIED');

  // Re-read the persisted region whenever the toggle screen comes
  // back into focus (typically: user just tapped a row on the
  // regions screen + the router popped back). useFocusEffect runs
  // on every focus, not just mount, so the change reflects without
  // a manual prop drill or pub/sub.
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

  const onToggle = async (value: boolean) => {
    if (value) {
      setState('CONNECTING');
      try {
        const identity = await loadOrCreateIdentity();
        const persistedRegion = (await AsyncStorage.getItem(SELECTED_REGION_KEY)) ?? AUTO_REGION_SENTINEL;
        // Mullvad-style: the account number IS the API key. vpn-svc
        // accepts unknown customer_ids + tier-defaults to FREE on
        // first sight (#569 contract).
        const session = await requestSession(
          identity.accountNumberRaw,
          identity.customerId,
          persistedRegion,
        );
        setQuotaState(session.quotaState);

        // Now actually start the tunnel via NETunnelProviderManager.
        // The native module triggers iOS's "Allow VPN configuration"
        // sheet on first run; subsequent runs reuse the saved
        // configuration. The PTP extension (#568/#572) takes over
        // from here, receives the providerConfiguration, runs the
        // WG handshake + NWPathMonitor for roaming.
        if (session.sessionId) {
          await TunnelControl.startTunnel({
            peerPublicKey: '',  // coordinator returns this on the followup peer-info call (#570)
            peerEndpoint: 'discover',
            customerInnerCIDR: '10.66.0.2/16',
            allowedIPs: '0.0.0.0/0',
            region: session.region,
            sessionId: session.sessionId,
          });
        }
      } catch (e) {
        console.warn('vpn start failed', e);
        setState('OFF');
      }
    } else {
      setState('DISCONNECTING');
      try {
        await TunnelControl.stopTunnel();
      } catch (e) {
        console.warn('vpn stop failed', e);
      }
      setState('OFF');
    }
  };

  // Subscribe to native VPN status updates so the toggle reflects
  // the real OS state. Without this the JS-side state diverges from
  // what iOS thinks (e.g. user disconnects via Settings → VPN
  // toggle, our UI would still show ON).
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

  const isOn = state === 'CONNECTING' || state === 'CONNECTED';

  return (
    <ThemedView style={styles.container}>
      <SafeAreaView style={styles.safe}>
        <View style={styles.header}>
          <ThemedText type="title">iogrid</ThemedText>
          <Pressable
            testID="settings-button"
            onPress={() => router.push('/settings')}
            hitSlop={12}
          >
            <ThemedText type="default">⚙</ThemedText>
          </Pressable>
        </View>

        <QuotaBanner state={quotaState} onUpgrade={() => router.push('/settings')} />

        <View style={styles.toggleBlock}>
          <Switch
            testID="vpn-toggle"
            value={isOn}
            onValueChange={onToggle}
            style={styles.bigSwitch}
          />
          <ThemedText type="subtitle" style={styles.state}>
            {state}
          </ThemedText>
        </View>

        <Pressable
          testID="region-picker-row"
          style={styles.regionRow}
          onPress={() => router.push('/regions')}
        >
          <View>
            <ThemedText type="small">Region</ThemedText>
            <ThemedText type="default">{region}</ThemedText>
          </View>
          <ThemedText type="default">›</ThemedText>
        </Pressable>
      </SafeAreaView>
    </ThemedView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1 },
  safe: { flex: 1, paddingHorizontal: Spacing.three },
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingVertical: 16,
  },
  toggleBlock: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    gap: 24,
  },
  bigSwitch: { transform: [{ scaleX: 2 }, { scaleY: 2 }] },
  state: { letterSpacing: 2, fontWeight: '600' },
  regionRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingVertical: 16,
    paddingHorizontal: 16,
    borderRadius: 12,
    backgroundColor: 'rgba(127, 127, 127, 0.1)',
    marginBottom: 24,
  },
});
