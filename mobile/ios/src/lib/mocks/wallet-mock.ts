// Wallet mock — for Maestro flows that exercise the top-up + wallet-
// connect screens. The real flow deeplinks to a third-party app;
// Maestro can't follow that, so the mock resolves in-process.

export type WalletProvider = 'phantom' | 'ping';

export interface MockWalletConnection {
  provider: WalletProvider;
  address: string;
  balanceGrid: number;
  balanceUsd: number;
}

export const MOCK_WALLET_CONNECTION: MockWalletConnection = {
  provider: 'ping',
  address: 'ping1maestrotestmocknotrealaddress00000000',
  balanceGrid: 432,
  balanceUsd: 4.32,
};

export async function mockConnectWallet(
  provider: WalletProvider = 'ping',
): Promise<MockWalletConnection> {
  await new Promise((r) => setTimeout(r, 150));
  return { ...MOCK_WALLET_CONNECTION, provider };
}

export async function mockTopupDeeplink(amountGrid: number): Promise<string> {
  // Real path: opens phantom://topup?amount=… or ping://topup?amount=…
  // and waits for the OS handoff. Mock just echoes the URL.
  return `ping://mock/topup?amount=${amountGrid}`;
}
