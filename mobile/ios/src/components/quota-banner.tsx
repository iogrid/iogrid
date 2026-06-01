// Quota banner — renders the server-returned `quota_state` enum as a
// dismissable banner on the toggle screen. Pairs with #573's
// server-side enum on POST /v1/vpn/sessions + GET /v1/vpn/sessions/{id}
// + heartbeat responses.
//
// State of truth is the server — this component is a pure renderer.
// The QuotaState type matches the enum string from the proto:
// QUOTA_STATE_OK | QUOTA_STATE_THROTTLED | QUOTA_STATE_EXHAUSTED.

import { Pressable, StyleSheet, View } from 'react-native';

import { ThemedText } from '@/components/themed-text';

export type QuotaState =
  | 'QUOTA_STATE_OK'
  | 'QUOTA_STATE_THROTTLED'
  | 'QUOTA_STATE_EXHAUSTED'
  | 'QUOTA_STATE_UNSPECIFIED';

interface Props {
  state: QuotaState;
  onUpgrade?: () => void;
}

export function QuotaBanner({ state, onUpgrade }: Props) {
  // OK + UNSPECIFIED render nothing — banner is only for actionable
  // states.
  if (state === 'QUOTA_STATE_OK' || state === 'QUOTA_STATE_UNSPECIFIED') {
    return null;
  }

  const config =
    state === 'QUOTA_STATE_THROTTLED'
      ? {
          title: 'Connection slowed',
          body: "You've used 80% of this month's free 2 GiB. Upgrade for full speed.",
          tint: '#bf8700',
          testID: 'quota-banner-throttled',
        }
      : {
          title: 'Free tier exhausted',
          body: 'Upgrade to keep your tunnel up. Free tier resets on the 1st.',
          tint: '#cf222e',
          testID: 'quota-banner-exhausted',
        };

  return (
    <View
      style={[styles.banner, { borderColor: config.tint }]}
      testID={config.testID}
    >
      <View style={styles.text}>
        <ThemedText type="default" style={{ color: config.tint, fontWeight: '600' }}>
          {config.title}
        </ThemedText>
        <ThemedText type="small">{config.body}</ThemedText>
      </View>
      <Pressable
        testID="quota-banner-upgrade"
        onPress={onUpgrade}
        style={[styles.button, { backgroundColor: config.tint }]}
      >
        <ThemedText type="default" style={styles.buttonText}>
          Upgrade
        </ThemedText>
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  banner: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    padding: 14,
    borderRadius: 12,
    borderWidth: 1,
    marginBottom: 12,
  },
  text: { flex: 1, gap: 2 },
  button: {
    paddingHorizontal: 14,
    paddingVertical: 8,
    borderRadius: 8,
  },
  buttonText: { color: 'white', fontWeight: '600' },
});
