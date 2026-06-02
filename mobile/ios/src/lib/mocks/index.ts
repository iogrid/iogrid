// Maestro mocks — activated only when `process.env.MAESTRO_MODE === '1'`.
//
// The mocks stand in for any surface that would otherwise depend on a
// system sheet (Apple sign-in), a third-party app (Phantom / Ping
// deeplinks), or a live backend (coordinator).
//
// Activation: call-sites import `isMaestroMode()` and short-circuit to
// the mock. Production builds NEVER include MAESTRO_MODE, so the bundle
// still ships the real code path.

export const MAESTRO_MODE_ENV = 'MAESTRO_MODE';

export function isMaestroMode(): boolean {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const env = (process as any)?.env;
  return env?.[MAESTRO_MODE_ENV] === '1';
}

export * from './apple-mock';
export * from './wallet-mock';
export * from './coordinator-mock';
export * from './balance-mock';
