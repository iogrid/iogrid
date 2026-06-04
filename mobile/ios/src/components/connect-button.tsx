/**
 * ConnectButton — the giant circular tunnel toggle.
 *
 * This is the primary affordance of the entire app. Mullvad pattern:
 * a single oversized circular button is the home screen. Three visual
 * states (off / connecting / connected) communicated through ring
 * color + animation, label inside the ring, plus an external status
 * label below.
 *
 * Reference: mobile/ios/docs/ux-wireframes-v2.md Screens 5/6/7
 *
 * Sizing/animation tokens live in `@/constants/theme` (ConnectButton,
 * Motion). The arc animation for CONNECTING is a continuous 360°
 * rotation of a 90° accent stroke (≈ heartbeat rhythm).
 *
 * testIDs: `connect-button`, `connect-button-label`, `status-label`
 */

import { useEffect, useRef } from 'react';
import {
  Animated,
  Easing,
  Pressable,
  StyleSheet,
  View,
  type AccessibilityState,
  type AccessibilityRole,
} from 'react-native';
import Svg, { Circle } from 'react-native-svg';

import { ThemedText } from '@/components/themed-text';
import { ThemedView } from '@/components/themed-view';
import { useReduceMotion } from '@/hooks/use-reduce-motion';
import { useTheme } from '@/hooks/use-theme';
import {
  ConnectButton as ConnectButtonTokens,
  Motion,
  Spacing,
  TypeScale,
} from '@/constants/theme';

export type ConnectState = 'off' | 'connecting' | 'connected';

interface Props {
  state: ConnectState;
  onPress: () => void;
  /** Sub-label shown inside the ring (e.g. 'Tap to connect', 'Connected'). */
  innerLabel?: string;
  /** Disable the button — e.g. while waiting for backend ack. */
  disabled?: boolean;
}

const AnimatedCircle = Animated.createAnimatedComponent(Circle);

export function ConnectButton({ state, onPress, innerLabel, disabled }: Props) {
  const theme = useTheme();

  // ── Ring color resolution ───────────────────────────────────────
  const ringColor =
    state === 'connected'
      ? theme.ringConnected
      : state === 'connecting'
        ? theme.ringConnecting
        : theme.ringOff;

  // ── Connecting-arc rotation ─────────────────────────────────────
  // A single 90° stroke rotates 360° in Motion.connectingArcRotation
  // milliseconds. The arc is rendered via a strokeDasharray hack on
  // a full circle path: the dash equals 25% of the circumference,
  // the gap equals 75%, and rotating the whole circle gives the
  // sweeping-arc illusion.
  const rotation = useRef(new Animated.Value(0)).current;
  const reduceMotion = useReduceMotion();

  useEffect(() => {
    if (state !== 'connecting') {
      rotation.stopAnimation();
      rotation.setValue(0);
      return;
    }
    // Reduce Motion (#684 pass 5): hold the arc mid-sweep instead of
    // looping — same in-progress read, no perpetual animation (and no
    // Maestro settle-wait tax).
    if (reduceMotion) {
      rotation.setValue(0.125);
      return;
    }
    const loop = Animated.loop(
      Animated.timing(rotation, {
        toValue: 1,
        duration: Motion.connectingArcRotation,
        easing: Easing.linear,
        useNativeDriver: true,
      }),
    );
    loop.start();
    return () => {
      loop.stop();
    };
  }, [state, rotation, reduceMotion]);

  const rotateInterpolation = rotation.interpolate({
    inputRange: [0, 1],
    outputRange: ['0deg', '360deg'],
  });

  // ── Ring geometry ───────────────────────────────────────────────
  const size = ConnectButtonTokens.size;
  const strokeWidth = ConnectButtonTokens.ringStrokeWidth;
  const radius = (size - strokeWidth) / 2;
  const center = size / 2;
  const circumference = 2 * Math.PI * radius;
  // 25% sweep arc for the connecting state
  const arcDash = circumference * 0.25;
  const arcGap = circumference * 0.75;

  // ── Accessibility ───────────────────────────────────────────────
  const a11yLabel =
    state === 'off'
      ? 'Connect to iogrid VPN'
      : state === 'connecting'
        ? 'Connecting to iogrid VPN'
        : 'Disconnect from iogrid VPN';

  const a11yState: AccessibilityState = {
    busy: state === 'connecting',
    disabled: !!disabled,
    selected: state === 'connected',
  };

  const a11yRole: AccessibilityRole = 'button';

  return (
    <ThemedView style={styles.container}>
      <Pressable
        testID="connect-button"
        onPress={onPress}
        disabled={disabled || state === 'connecting'}
        hitSlop={ConnectButtonTokens.tapTargetExpansion}
        accessibilityLabel={a11yLabel}
        accessibilityRole={a11yRole}
        accessibilityState={a11yState}
        style={({ pressed }) => [
          styles.pressable,
          { width: size, height: size, borderRadius: size / 2 },
          // The disc: a filled body so the control reads as a BUTTON, not
          // a hollow outline (#684). Connected gets a green-tinted fill +
          // a soft glow — the Mullvad "secured" moment.
          {
            backgroundColor:
              state === 'connected' ? theme.connectFillConnected : theme.connectFill,
          },
          state === 'connected' ? [styles.glow, { shadowColor: theme.accent }] : styles.restShadow,
          pressed && !disabled && state !== 'connecting' ? styles.pressed : null,
        ]}
      >
        {/* Static ring (always full circle, color = current state) */}
        <Svg width={size} height={size} style={StyleSheet.absoluteFill}>
          <Circle
            cx={center}
            cy={center}
            r={radius}
            stroke={ringColor}
            strokeWidth={strokeWidth}
            fill="transparent"
            // Dim the static ring during CONNECTING so the sweeping arc
            // is the primary motion read; tone is the same hue, lower
            // opacity rather than a different color.
            opacity={state === 'connecting' ? 0.25 : 1}
          />
        </Svg>

        {/* CONNECTING sweep arc — only rendered in connecting state */}
        {state === 'connecting' ? (
          <Animated.View
            style={[
              StyleSheet.absoluteFill,
              { transform: [{ rotate: rotateInterpolation }] },
            ]}
          >
            <Svg width={size} height={size}>
              <AnimatedCircle
                cx={center}
                cy={center}
                r={radius}
                stroke={ringColor}
                strokeWidth={strokeWidth}
                strokeLinecap="round"
                strokeDasharray={[arcDash, arcGap]}
                fill="transparent"
              />
            </Svg>
          </Animated.View>
        ) : null}

        {/* Inner label */}
        <View style={styles.innerLabelWrap}>
          <ThemedText
            testID="connect-button-label"
            style={[
              styles.innerLabel,
              state === 'connected' ? { color: theme.accent } : null,
            ]}
          >
            {innerLabel ??
              (state === 'off'
                ? 'Tap to\nconnect'
                : state === 'connecting'
                  ? 'Connecting…'
                  : 'Connected')}
          </ThemedText>
        </View>
      </Pressable>

      {/* External status label — all-caps, letter-spaced, SEMANTICALLY
          colored (the Mullvad pattern the gray-only label was missing,
          #684): unsecured reads red, secured reads green. */}
      <ThemedText
        testID="status-label"
        style={[
          styles.statusLabel,
          {
            color:
              state === 'off'
                ? theme.error
                : state === 'connecting'
                  ? theme.textSecondary
                  : theme.accent,
          },
        ]}
      >
        {state === 'off'
          ? 'UNSECURED CONNECTION'
          : state === 'connecting'
            ? 'CREATING SECURE CONNECTION…'
            : 'SECURE CONNECTION'}
      </ThemedText>
    </ThemedView>
  );
}

const styles = StyleSheet.create({
  container: {
    alignItems: 'center',
    justifyContent: 'center',
    gap: Spacing.lg,
    paddingVertical: Spacing.xxl,
  },
  pressable: {
    alignItems: 'center',
    justifyContent: 'center',
  },
  pressed: {
    opacity: 0.9,
    transform: [{ scale: 0.96 }],
  },
  // Resting elevation — the disc floats slightly off the canvas.
  restShadow: {
    shadowColor: '#000000',
    shadowOpacity: 0.08,
    shadowRadius: 16,
    shadowOffset: { width: 0, height: 6 },
    elevation: 4,
  },
  // The secured-state glow: a soft halo in the accent hue.
  glow: {
    shadowOpacity: 0.45,
    shadowRadius: 24,
    shadowOffset: { width: 0, height: 0 },
    elevation: 10,
  },
  innerLabelWrap: {
    alignItems: 'center',
    justifyContent: 'center',
    paddingHorizontal: Spacing.lg,
  },
  innerLabel: {
    ...TypeScale.bodyL,
    textAlign: 'center',
    fontWeight: '500',
  },
  statusLabel: {
    ...TypeScale.statusLabel,
  },
});
