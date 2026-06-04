/**
 * Pure state-machine helpers for the Main screen's tunnel lifecycle.
 *
 * Extracted out of `src/app/index.tsx` so the logic can be unit-tested
 * without rendering the screen (which pulls in the native
 * `iogrid-tunnel-control` TurboModule + expo-secure-store + the
 * coordinator client — none of which load under `testEnvironment: node`).
 * This mirrors how `src/lib/grid_balance.ts` and `src/lib/wallets/*`
 * keep their pure logic out of the component tree.
 *
 * Both functions are the #684-pass-5 "connection state honesty" fix:
 * before it, a failed connect attempt froze the step list on a perpetual
 * spinner and then vanished under the failure alert, so the user never
 * learned WHICH stage broke. The transforms below make that observable
 * and — being pure — keep it regression-guarded.
 *
 * The type-only imports below are erased at compile time (ts-jest with
 * `import type`), so importing this module touches zero native code.
 *
 * Refs #580, #591, #684.
 */

import type { ConnectState } from '@/components/connect-button';
import type { ConnectionStep } from '@/components/connection-status';

/** The four tunnel states surfaced by the native TunnelControl module. */
export type TunnelState = 'OFF' | 'CONNECTING' | 'CONNECTED' | 'DISCONNECTING';

/**
 * Collapse the 4-state native tunnel lifecycle into the 3-state visual
 * the ConnectButton renders. DISCONNECTING shows the same in-progress
 * affordance as CONNECTING (the button is busy either way); everything
 * that isn't actively up or actively transitioning reads as `off`.
 */
export function tunnelToConnectState(state: TunnelState): ConnectState {
  if (state === 'CONNECTED') return 'connected';
  if (state === 'CONNECTING' || state === 'DISCONNECTING') return 'connecting';
  return 'off';
}

/**
 * Mark the in-progress step as failed when a connect attempt dies (#684
 * pass 5). Flips ONLY the single `active` step to `failed`; `pending` and
 * `done` steps are left untouched so the list reads as "we got this far,
 * then it broke" rather than freezing on a perpetual spinner. Pure +
 * immutable (returns a new array, never mutates the input) so React state
 * updates stay referentially honest and the transform is unit-testable in
 * isolation.
 */
export function failActiveConnectingStep(steps: ConnectionStep[]): ConnectionStep[] {
  return steps.map((st) =>
    st.state === 'active' ? { ...st, state: 'failed' as const } : st,
  );
}
