/**
 * useReduceMotion — live mirror of the OS "Reduce Motion" setting.
 *
 * #684 pass 5: the connecting arcs run infinite Animated.loops, which
 * (a) ignore a system-wide accessibility preference and (b) are the
 * documented reason Maestro burns 4–5s per interaction waiting for the
 * hierarchy to "settle" (see the 12s-hold comment in app/index.tsx).
 * Consumers render a static state when this returns true.
 */

import { useEffect, useState } from 'react';
import { AccessibilityInfo } from 'react-native';

export function useReduceMotion(): boolean {
  const [reduceMotion, setReduceMotion] = useState(false);

  useEffect(() => {
    let mounted = true;
    AccessibilityInfo.isReduceMotionEnabled()
      .then((v) => {
        if (mounted) setReduceMotion(v);
      })
      .catch(() => undefined);
    const sub = AccessibilityInfo.addEventListener('reduceMotionChanged', setReduceMotion);
    return () => {
      mounted = false;
      sub.remove();
    };
  }, []);

  return reduceMotion;
}
