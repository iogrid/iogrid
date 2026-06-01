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
import { Spacing } from '@/constants/theme';
import { AUTO_REGION_SENTINEL } from '@/app/regions';

type TunnelState = 'OFF' | 'CONNECTING' | 'CONNECTED' | 'DISCONNECTING';

const SELECTED_REGION_KEY = 'iogrid.region.selected';

export default function VPNToggleScreen() {
  const [state, setState] = useState<TunnelState>('OFF');
  const [region, setRegion] = useState<string>('Best (auto)');

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

  const onToggle = (value: boolean) => {
    if (value) {
      setState('CONNECTING');
      // TODO(#568): hand off to PacketTunnelProvider via
      // NETunnelProviderManager + transition to CONNECTED on tunnel up.
    } else {
      setState('DISCONNECTING');
      setTimeout(() => setState('OFF'), 250);
    }
  };

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
