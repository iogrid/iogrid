// Wallet adapter registry — the single import surface for screens
// that need to launch a wallet connect flow. Adding a new wallet means
// implementing the {@link Wallet} interface and adding one row here.

export * from './types';
export { phantomWallet } from './phantom';
export { pingWallet } from './ping';

import type { Wallet, WalletProvider } from './types';
import { phantomWallet } from './phantom';
import { pingWallet } from './ping';

/** Resolve a Wallet by its provider tag. */
export function walletFor(provider: WalletProvider): Wallet {
  switch (provider) {
    case 'phantom':
      return phantomWallet;
    case 'ping':
      return pingWallet;
  }
}
