// Tests for the Main-screen tunnel-lifecycle state machine (Refs #684).
//
// These two transforms drive what the user SEES while a VPN connect
// attempt is in flight, and — more importantly — what they see when it
// fails. Before #684 pass 5, a failed attempt left the step list frozen
// on a perpetual spinner and then yanked it out from under a failure
// alert, so the user never learned which stage broke ("Resolving peer?
// Establishing tunnel? Verifying egress?"). `failActiveConnectingStep`
// is the fix: it turns the single in-progress step red so the failure is
// honest and located. A regression here silently returns the app to the
// "mystery spinner" failure mode — invisible in a green CI run, obvious
// and infuriating to a real user on a flaky network.
//
// Both functions are pure (no React, no native modules), so we drive
// them directly — the same approach src/lib/grid_balance + wallets tests
// use to stay out of the react-native / expo runtime. The `ConnectionStep`
// import is type-only, so this file pulls in zero native code.
//
// Matrix:
//   tunnelToConnectState
//     1. CONNECTED      → 'connected'
//     2. CONNECTING     → 'connecting'
//     3. DISCONNECTING  → 'connecting'  (button is busy either way)
//     4. OFF            → 'off'
//     5. exhaustiveness — every TunnelState maps to a valid ConnectState
//   failActiveConnectingStep
//     6. the real default scenario (active, pending, pending) → first fails
//     7. only the single `active` step flips; pending + done untouched
//     8. no active step (all pending)  → no-op
//     9. no active step (all done)     → no-op
//    10. immutability — input array + its members are never mutated
//    11. a later-stage failure leaves earlier `done` steps intact
//    12. an already-`failed` step is left as-is (no double-fail, no un-fail)

import type { ConnectionStep } from '@/components/connection-status';
import {
  failActiveConnectingStep,
  tunnelToConnectState,
  type TunnelState,
} from '@/lib/connection-steps';

describe('tunnelToConnectState', () => {
  it('maps CONNECTED → connected', () => {
    expect(tunnelToConnectState('CONNECTED')).toBe('connected');
  });

  it('maps CONNECTING → connecting', () => {
    expect(tunnelToConnectState('CONNECTING')).toBe('connecting');
  });

  it('maps DISCONNECTING → connecting (the button stays busy while tearing down)', () => {
    expect(tunnelToConnectState('DISCONNECTING')).toBe('connecting');
  });

  it('maps OFF → off', () => {
    expect(tunnelToConnectState('OFF')).toBe('off');
  });

  it('maps every TunnelState to a valid 3-state visual (exhaustive)', () => {
    const all: TunnelState[] = ['OFF', 'CONNECTING', 'CONNECTED', 'DISCONNECTING'];
    const valid = new Set(['off', 'connecting', 'connected']);
    for (const s of all) {
      expect(valid.has(tunnelToConnectState(s))).toBe(true);
    }
  });
});

describe('failActiveConnectingStep', () => {
  // The canonical CONNECTING set the screen starts from: first step
  // active, the rest pending (kept in sync with DEFAULT_CONNECTING_STEPS
  // in connection-status.tsx — duplicated here as a literal so the test
  // stays free of the component's native-svg import chain).
  const defaultSteps = (): ConnectionStep[] => [
    { id: 'resolve-peer', label: 'Resolving peer', state: 'active' },
    { id: 'establish-tunnel', label: 'Establishing tunnel', state: 'pending' },
    { id: 'verify-egress', label: 'Verifying egress IP', state: 'pending' },
  ];

  it('fails the in-progress step in the real default scenario', () => {
    const out = failActiveConnectingStep(defaultSteps());
    expect(out.map((s) => s.state)).toEqual(['failed', 'pending', 'pending']);
    // labels + ids are preserved — only `state` changes
    expect(out[0].id).toBe('resolve-peer');
    expect(out[0].label).toBe('Resolving peer');
  });

  it('flips ONLY the active step; pending and done are untouched', () => {
    const steps: ConnectionStep[] = [
      { id: 'a', label: 'A', state: 'done' },
      { id: 'b', label: 'B', state: 'active' },
      { id: 'c', label: 'C', state: 'pending' },
    ];
    expect(failActiveConnectingStep(steps).map((s) => s.state)).toEqual([
      'done',
      'failed',
      'pending',
    ]);
  });

  it('is a no-op when nothing is active (all pending)', () => {
    const steps: ConnectionStep[] = [
      { id: 'a', label: 'A', state: 'pending' },
      { id: 'b', label: 'B', state: 'pending' },
    ];
    expect(failActiveConnectingStep(steps).map((s) => s.state)).toEqual([
      'pending',
      'pending',
    ]);
  });

  it('is a no-op when nothing is active (all done)', () => {
    const steps: ConnectionStep[] = [
      { id: 'a', label: 'A', state: 'done' },
      { id: 'b', label: 'B', state: 'done' },
    ];
    expect(failActiveConnectingStep(steps).map((s) => s.state)).toEqual([
      'done',
      'done',
    ]);
  });

  it('never mutates the input array or its members', () => {
    const steps = defaultSteps();
    const snapshot = JSON.parse(JSON.stringify(steps));
    const out = failActiveConnectingStep(steps);
    // input is untouched...
    expect(steps).toEqual(snapshot);
    // ...and a NEW array of NEW objects is returned (referential honesty
    // so React's setState sees a changed reference and re-renders).
    expect(out).not.toBe(steps);
    expect(out[0]).not.toBe(steps[0]);
  });

  it('leaves an earlier completed step intact when a later stage fails', () => {
    // Realistic mid-handshake failure: peer resolved (done), tunnel
    // establishment is the active step that dies, egress never started.
    const steps: ConnectionStep[] = [
      { id: 'resolve-peer', label: 'Resolving peer', state: 'done' },
      { id: 'establish-tunnel', label: 'Establishing tunnel', state: 'active' },
      { id: 'verify-egress', label: 'Verifying egress IP', state: 'pending' },
    ];
    expect(failActiveConnectingStep(steps).map((s) => s.state)).toEqual([
      'done',
      'failed',
      'pending',
    ]);
  });

  it('leaves an already-failed step as-is (no double-fail, no un-fail)', () => {
    // Defensive: if failActiveStep somehow runs twice (two failure paths
    // firing), the second pass must be idempotent — no `active` left to
    // flip, and the existing `failed` is preserved.
    const onceFailed = failActiveConnectingStep(defaultSteps());
    const twiceFailed = failActiveConnectingStep(onceFailed);
    expect(twiceFailed.map((s) => s.state)).toEqual(['failed', 'pending', 'pending']);
  });
});
