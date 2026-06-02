// Coordinator mock — region list + session lifecycle without a live
// vpn-svc. Used by Maestro flows so the region picker renders rows
// deterministically and the toggle connects to a fake session.

import type { RegionRow, SessionState } from '@/lib/coordinator';

export const MOCK_REGIONS: RegionRow[] = [
  { region: 'eu-central-1', healthyProviders: 12, totalProviders: 14 },
  { region: 'eu-west-1', healthyProviders: 8, totalProviders: 9 },
  { region: 'us-east-1', healthyProviders: 22, totalProviders: 25 },
  { region: 'us-west-2', healthyProviders: 11, totalProviders: 12 },
  { region: 'ap-northeast-1', healthyProviders: 6, totalProviders: 6 },
];

export async function mockListRegions(): Promise<RegionRow[]> {
  await new Promise((r) => setTimeout(r, 100));
  return MOCK_REGIONS;
}

export async function mockRequestSession(region: string): Promise<SessionState> {
  await new Promise((r) => setTimeout(r, 200));
  return {
    sessionId: 'mock-session-id',
    state: 'CONNECTED',
    region,
    quotaState: 'QUOTA_STATE_OK',
    bytesIn: 0,
    bytesOut: 0,
  };
}
