// Wallet interface — Track 2 owns the implementations of phantom-iogrid
// and ping-iogrid wallets. Track 4 (this PR) declares the contract +
// stubs so the UI renders.
//
// Track 2's wallets/*.ts file replaces this module's connect /
// getBalance / buildTopupURL with the real deeplink + balance calls.

import AsyncStorage from '@react-native-async-storage/async-storage';

import {
  isMaestroMode,
  mockConnectWallet,
  mockFetchBalance,
  mockTopupDeeplink,
  type WalletProvider,
} from '@/lib/mocks';

export const WALLET_PROVIDER_KEY = 'iogrid.wallet.provider';
export const WALLET_ADDRESS_KEY = 'iogrid.wallet.address';

export interface WalletBalance {
  balanceGrid: number;
  balanceUsd: number;
  burnRateGridPerMin?: number;
  estimatedDaysAtUsage?: number;
}

export interface WalletState {
  provider: WalletProvider;
  address: string;
}

export interface WalletApi {
  connect(provider: WalletProvider): Promise<WalletState>;
  disconnect(): Promise<void>;
  getStored(): Promise<WalletState | null>;
  getBalance(): Promise<WalletBalance>;
  buildTopupURL(amountGrid: number, method: string): Promise<string>;
}

async function persist(state: WalletState): Promise<void> {
  await Promise.all([
    AsyncStorage.setItem(WALLET_PROVIDER_KEY, state.provider),
    AsyncStorage.setItem(WALLET_ADDRESS_KEY, state.address),
  ]);
}

export const wallet: WalletApi = {
  async connect(provider: WalletProvider): Promise<WalletState> {
    if (isMaestroMode()) {
      const mock = await mockConnectWallet(provider);
      const state: WalletState = { provider: mock.provider, address: mock.address };
      await persist(state);
      return state;
    }
    const state: WalletState = {
      provider,
      address: `pending-track-2-${provider}`,
    };
    await persist(state);
    return state;
  },

  async disconnect(): Promise<void> {
    await Promise.all([
      AsyncStorage.removeItem(WALLET_PROVIDER_KEY),
      AsyncStorage.removeItem(WALLET_ADDRESS_KEY),
    ]);
  },

  async getStored(): Promise<WalletState | null> {
    const [provider, address] = await Promise.all([
      AsyncStorage.getItem(WALLET_PROVIDER_KEY),
      AsyncStorage.getItem(WALLET_ADDRESS_KEY),
    ]);
    if (!provider || !address) return null;
    return { provider: provider as WalletProvider, address };
  },

  async getBalance(): Promise<WalletBalance> {
    if (isMaestroMode()) {
      const m = await mockFetchBalance();
      return {
        balanceGrid: m.balanceGrid,
        balanceUsd: m.balanceUsd,
        burnRateGridPerMin: m.burnRateGridPerMin,
        estimatedDaysAtUsage: m.estimatedDaysAtUsage,
      };
    }
    return {
      balanceGrid: 0,
      balanceUsd: 0,
    };
  },

  async buildTopupURL(amountGrid: number, method: string): Promise<string> {
    if (isMaestroMode()) {
      return mockTopupDeeplink(amountGrid);
    }
    return `ping://topup?amount=${amountGrid}&method=${encodeURIComponent(method)}`;
  },
};

export type { WalletProvider };
