/**
 * ConnectionStatus — step-list shown during CONNECTING state.
 *
 * Mullvad pattern: while the tunnel is establishing, the user sees
 * a vertical list of micro-steps that fill in as the WireGuard
 * handshake progresses. iogrid maps this to three coordinator
 * events: peer resolution (vpn-svc returns peer config), tunnel
 * establishment (WireGuardAdapter.start completion), and egress
 * verification (first packet round-trips through the peer).
 *
 * Refs #580, #591.
 *
 * Each step is one of:
 *   - 'pending'    — not yet started (greyscale dot)
 *   - 'active'     — currently in progress (spinning arc)
 *   - 'failed'     — the step the connect attempt died on (#684 pass 5:
 *                    previously the list froze mid-spinner and vanished
 *                    under the failure alert with no state honesty)
 *   - 'done'       — completed (filled accent checkmark)
 *
 * The component is purely visual; the parent screen drives the
 * `steps` prop from its `TunnelControl.onStatusChange` +
 * `TunnelControl.onStatsUpdate` subscribers.
 */

import { StyleSheet, View } from 'react-native';

import { ThemedText } from '@/components/themed-text';
import { Spinner } from '@/components/spinner';
import { Spacing, TypeScale } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';

export type ConnectionStepState = 'pending' | 'active' | 'done' | 'failed';

export interface ConnectionStep {
  /** Stable id (used as React key + testID suffix). */
  id: string;
  /** Visible label. */
  label: string;
  state: ConnectionStepState;
}

interface Props {
  steps: ConnectionStep[];
  /** Optional override; defaults to standard iogrid step set. */
  testIDPrefix?: string;
}

/**
 * Default step set used by the Main screen's CONNECTING render. Lets
 * callers import the canonical labels rather than re-defining strings.
 */
export const DEFAULT_CONNECTING_STEPS: ConnectionStep[] = [
  { id: 'resolve-peer', label: 'Resolving peer', state: 'active' },
  { id: 'establish-tunnel', label: 'Establishing tunnel', state: 'pending' },
  { id: 'verify-egress', label: 'Verifying egress IP', state: 'pending' },
];

export function ConnectionStatus({ steps, testIDPrefix = 'connection-step' }: Props) {
  const theme = useTheme();

  return (
    <View
      testID="connection-status"
      accessibilityLabel="Tunnel establishment progress"
      accessibilityRole="summary"
      style={styles.container}
    >
      {steps.map((step) => (
        <View
          key={step.id}
          testID={`${testIDPrefix}-${step.id}`}
          style={styles.row}
          accessibilityLabel={`${step.label}: ${step.state}`}
        >
          <View style={styles.indicator}>
            {step.state === 'pending' ? (
              <View
                style={[
                  styles.dot,
                  { backgroundColor: 'transparent', borderColor: theme.borderStrong },
                ]}
              />
            ) : step.state === 'active' ? (
              <Spinner size={14} color={theme.text} rotationDuration={900} />
            ) : step.state === 'failed' ? (
              <View
                style={[
                  styles.dot,
                  styles.dotFilled,
                  { backgroundColor: theme.error, borderColor: theme.error },
                ]}
              >
                <ThemedText style={[styles.checkmark, { color: theme.textInverse }]}>
                  ✕
                </ThemedText>
              </View>
            ) : (
              <View
                style={[
                  styles.dot,
                  styles.dotFilled,
                  { backgroundColor: theme.accent, borderColor: theme.accent },
                ]}
              >
                <ThemedText style={[styles.checkmark, { color: theme.textInverse }]}>
                  ✓
                </ThemedText>
              </View>
            )}
          </View>
          <ThemedText
            style={[
              styles.label,
              {
                color:
                  step.state === 'pending'
                    ? theme.textTertiary
                    : step.state === 'active'
                      ? theme.text
                      : step.state === 'failed'
                        ? theme.error
                        : theme.textSecondary,
              },
              step.state === 'done' ? styles.labelDone : null,
            ]}
          >
            {step.label}
          </ThemedText>
        </View>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    gap: Spacing.md,
    paddingVertical: Spacing.lg,
    paddingHorizontal: Spacing.lg,
    alignSelf: 'stretch',
  },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: Spacing.md,
  },
  indicator: {
    width: 18,
    height: 18,
    alignItems: 'center',
    justifyContent: 'center',
  },
  dot: {
    width: 14,
    height: 14,
    borderRadius: 7,
    borderWidth: 2,
    alignItems: 'center',
    justifyContent: 'center',
  },
  dotFilled: {
    borderWidth: 0,
  },
  checkmark: {
    fontSize: 10,
    fontWeight: '700',
    lineHeight: 12,
  },
  label: {
    ...TypeScale.bodyM,
    flex: 1,
  },
  labelDone: {
    textDecorationLine: 'line-through',
    textDecorationStyle: 'solid',
  },
});
