// $GRID balance mock — feeds the wallet card with a deterministic
// balance the Maestro flows can assert against.

export interface MockBalanceSnapshot {
  balanceGrid: number;
  balanceUsd: number;
  burnRateGridPerMin: number;
  estimatedDaysAtUsage: number;
}

export const MOCK_BALANCE: MockBalanceSnapshot = {
  balanceGrid: 432,
  balanceUsd: 4.32,
  burnRateGridPerMin: 0.002,
  estimatedDaysAtUsage: 12,
};

export async function mockFetchBalance(): Promise<MockBalanceSnapshot> {
  await new Promise((r) => setTimeout(r, 80));
  return MOCK_BALANCE;
}
