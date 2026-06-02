// Spinner — animated arc per the #589 DoD.
//
// 60° filled arc on a transparent circle, rotating once per second.
// react-native-svg for the arc geometry + react-native-reanimated for
// the rotation driver (no JS-bridge ticks, runs on UI thread).

import { useEffect } from 'react';
import { StyleSheet, View } from 'react-native';
import Animated, {
  Easing,
  useAnimatedStyle,
  useSharedValue,
  withRepeat,
  withTiming,
  cancelAnimation,
} from 'react-native-reanimated';
import Svg, { Path } from 'react-native-svg';

import { useTheme } from '@/hooks/use-theme';

export interface SpinnerProps {
  size?: number;
  thickness?: number;
  /** Arc colour. Defaults to the theme's accent. */
  color?: string;
  /** Override rotation period in ms. Default 1000ms = 1 rotation/s. */
  durationMs?: number;
  testID?: string;
}

export function Spinner({
  size = 36,
  thickness = 3,
  color,
  durationMs = 1000,
  testID,
}: SpinnerProps) {
  const theme = useTheme();
  const arcColor = color ?? theme.accent;
  const rotation = useSharedValue(0);

  useEffect(() => {
    rotation.value = withRepeat(
      withTiming(360, { duration: durationMs, easing: Easing.linear }),
      -1,
      false,
    );
    return () => {
      cancelAnimation(rotation);
    };
  }, [rotation, durationMs]);

  const animatedStyle = useAnimatedStyle(() => ({
    transform: [{ rotate: `${rotation.value}deg` }],
  }));

  // 60° arc on a centered circle. Path drawn as an SVG arc command:
  // M start of arc → A radius radius 0 0 1 end of arc.
  const r = (size - thickness) / 2;
  const cx = size / 2;
  const cy = size / 2;
  const startAngle = -90; // start at 12 o'clock
  const endAngle = startAngle + 60;
  const toRad = (deg: number) => (deg * Math.PI) / 180;
  const start = {
    x: cx + r * Math.cos(toRad(startAngle)),
    y: cy + r * Math.sin(toRad(startAngle)),
  };
  const end = {
    x: cx + r * Math.cos(toRad(endAngle)),
    y: cy + r * Math.sin(toRad(endAngle)),
  };
  const arcPath = `M ${start.x} ${start.y} A ${r} ${r} 0 0 1 ${end.x} ${end.y}`;

  return (
    <View testID={testID} style={{ width: size, height: size }}>
      <Animated.View style={[styles.fill, animatedStyle]}>
        <Svg width={size} height={size} viewBox={`0 0 ${size} ${size}`}>
          <Path
            d={arcPath}
            stroke={arcColor}
            strokeWidth={thickness}
            strokeLinecap="round"
            fill="none"
          />
        </Svg>
      </Animated.View>
    </View>
  );
}

const styles = StyleSheet.create({
  fill: { ...StyleSheet.absoluteFill },
});
