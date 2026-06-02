// Connection status — drives the text + step list on the CONNECTING
// state per #591 DoD. Tied to the parent's state machine; receives
// `state` + optional `step` from IPC.

import { StyleSheet, View } from 'react-native';

import { ThemedText } from '@/components/themed-text';
import { useTheme } from '@/hooks/use-theme';
import type { ConnectState } from '@/components/connect-button';

export type ConnectingStep = 'resolve' | 'tunnel' | 'egress';

const STEP_ORDER: ConnectingStep[] = ['resolve', 'tunnel', 'egress'];
const STEP_LABEL: Record<ConnectingStep, string> = {
  resolve: 'Resolving peer',
  tunnel: 'Establishing tunnel',
  egress: 'Verifying egress IP',
};

export interface ConnectionStatusProps {
  state: ConnectState;
  step?: ConnectingStep;
  testID?: string;
}

export function ConnectionStatus({
  state,
  step = 'resolve',
  testID = 'connection-status',
}: ConnectionStatusProps) {
  const theme = useTheme();

  if (state !== 'connecting') {
    return <View testID={testID} />;
  }

  const currentIdx = STEP_ORDER.indexOf(step);
  return (
    <View testID={testID} style={styles.container}>
      <View style={styles.steps}>
        {STEP_ORDER.map((s, i) => {
          const done = i < currentIdx;
          const active = i === currentIdx;
          const dotColor = done
            ? theme.accent
            : active
              ? theme.text
              : theme.textTertiary;
          const labelColor = done || active ? theme.text : theme.textTertiary;
          const symbol = done ? '✓' : active ? '●' : '○';
          return (
            <View key={s} style={styles.stepRow}>
              <ThemedText type="body-m" color={dotColor} style={styles.stepDot}>
                {symbol}
              </ThemedText>
              <ThemedText type="body-m" color={labelColor}>
                {STEP_LABEL[s]}
              </ThemedText>
            </View>
          );
        })}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  container: { alignItems: 'center', gap: 16 },
  steps: { gap: 8, alignSelf: 'center' },
  stepRow: { flexDirection: 'row', alignItems: 'center', gap: 12 },
  stepDot: { width: 18, textAlign: 'center' },
});
