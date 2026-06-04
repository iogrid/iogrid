/**
 * Spinner — animated arc primitive for connecting / loading states.
 *
 * Reuses the same SVG sweep-arc pattern as ConnectButton but at any
 * size, with configurable color. The arc is 25% of the circumference,
 * rotates 360° in 1s linear by default.
 *
 * Refs #580, #589.
 */

import { useEffect, useRef } from 'react';
import { Animated, Easing, StyleSheet, View, type ViewStyle } from 'react-native';
import Svg, { Circle } from 'react-native-svg';

import { Motion } from '@/constants/theme';
import { useReduceMotion } from '@/hooks/use-reduce-motion';
import { useTheme } from '@/hooks/use-theme';

interface Props {
  /** Diameter in points. Default 24. */
  size?: number;
  /** Stroke width. Default size/12. */
  strokeWidth?: number;
  /** Color override; defaults to theme.textSecondary. */
  color?: string;
  /** Rotation duration (ms). Default Motion.connectingArcRotation = 1000. */
  rotationDuration?: number;
  style?: ViewStyle;
}

const AnimatedCircle = Animated.createAnimatedComponent(Circle);

export function Spinner({
  size = 24,
  strokeWidth,
  color,
  rotationDuration = Motion.connectingArcRotation,
  style,
}: Props) {
  const theme = useTheme();
  const resolvedColor = color ?? theme.textSecondary;
  const sw = strokeWidth ?? Math.max(2, Math.round(size / 12));
  const radius = (size - sw) / 2;
  const center = size / 2;
  const circumference = 2 * Math.PI * radius;
  const arcDash = circumference * 0.25;
  const arcGap = circumference * 0.75;

  const rotation = useRef(new Animated.Value(0)).current;
  const reduceMotion = useReduceMotion();

  useEffect(() => {
    // Honor the OS Reduce Motion setting (#684 pass 5): hold the arc at
    // a fixed angle — the visible partial arc + progressbar role still
    // read as "in progress" without the perpetual rotation. Also stops
    // the infinite loop that forces Maestro's 4-5s settle-waits.
    if (reduceMotion) {
      rotation.setValue(0.125); // 45° — clearly "mid-progress", not idle
      return;
    }
    const loop = Animated.loop(
      Animated.timing(rotation, {
        toValue: 1,
        duration: rotationDuration,
        easing: Easing.linear,
        useNativeDriver: true,
      }),
    );
    loop.start();
    return () => {
      loop.stop();
    };
  }, [rotation, rotationDuration, reduceMotion]);

  const rotate = rotation.interpolate({
    inputRange: [0, 1],
    outputRange: ['0deg', '360deg'],
  });

  return (
    <View
      style={[styles.container, { width: size, height: size }, style]}
      accessibilityRole="progressbar"
      accessibilityLabel="Loading"
    >
      <Animated.View style={[StyleSheet.absoluteFill, { transform: [{ rotate }] }]}>
        <Svg width={size} height={size}>
          <AnimatedCircle
            cx={center}
            cy={center}
            r={radius}
            stroke={resolvedColor}
            strokeWidth={sw}
            strokeLinecap="round"
            strokeDasharray={[arcDash, arcGap]}
            fill="transparent"
          />
        </Svg>
      </Animated.View>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    alignItems: 'center',
    justifyContent: 'center',
  },
});
