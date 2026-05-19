"use client";

/**
 * SolanaWalletProvider — mounts the wallet-adapter stack once at the
 * root of the App Router tree so every client component can call
 * `useWallet()` / `useConnection()`.
 *
 * The whole stack is **client-only**:
 *   - `@solana/wallet-adapter-react` uses browser APIs (window.solana,
 *     window.crypto.subtle.verify) and reads `localStorage` for the
 *     auto-connect cookie.
 *   - `@solana/wallet-adapter-react-ui` ships CSS that we mount via
 *     dynamic side-effect import below.
 *
 * The provider sits inside a `"use client"` boundary; pages mounted on
 * the server are unaffected and continue to SSR normally (the wallet
 * subtree hydrates after navigation).
 */

import * as React from "react";
import {
  ConnectionProvider,
  WalletProvider,
} from "@solana/wallet-adapter-react";
import { WalletModalProvider } from "@solana/wallet-adapter-react-ui";
import type { Adapter } from "@solana/wallet-adapter-base";
// Adapters are imported from their individual packages rather than
// from the `@solana/wallet-adapter-wallets` umbrella because the
// umbrella pulls every adapter — including Ledger's `usb@2.x` native
// addon — which fails the Docker `node:22-alpine` build (no Python /
// build-essential available). Backpack, Glow, and other Wallet
// Standard wallets still register automatically via the standard.
import { PhantomWalletAdapter } from "@solana/wallet-adapter-phantom";
import { SolflareWalletAdapter } from "@solana/wallet-adapter-solflare";
import { TrustWalletAdapter } from "@solana/wallet-adapter-trust";

import { SOLANA_RPC_URL } from "./config";

// Wallet-adapter-react-ui ships its own stylesheet for the modal +
// connect button. Import once at module scope so it's bundled into the
// client chunk.
import "@solana/wallet-adapter-react-ui/styles.css";

export interface SolanaWalletProviderProps {
  children: React.ReactNode;
  /** Override the RPC endpoint (tests / Storybook). */
  endpoint?: string;
  /**
   * Inject custom adapters (tests). Defaults to Phantom / Solflare /
   * Trust — Backpack is intentionally omitted as its standalone
   * adapter package was deprecated in favour of the Solana wallet
   * standard (Backpack auto-registers via the standard, no explicit
   * adapter needed; users will still see it in the modal).
   */
  adapters?: Adapter[];
  /** Auto-reconnect to the last-used wallet on mount. */
  autoConnect?: boolean;
}

export function SolanaWalletProvider({
  children,
  endpoint,
  adapters,
  autoConnect = true,
}: SolanaWalletProviderProps) {
  const resolvedEndpoint = endpoint ?? SOLANA_RPC_URL;
  const wallets = React.useMemo<Adapter[]>(
    () =>
      adapters ?? [
        new PhantomWalletAdapter(),
        new SolflareWalletAdapter(),
        new TrustWalletAdapter(),
      ],
    [adapters],
  );

  return (
    <ConnectionProvider endpoint={resolvedEndpoint}>
      <WalletProvider wallets={wallets} autoConnect={autoConnect}>
        <WalletModalProvider>{children}</WalletModalProvider>
      </WalletProvider>
    </ConnectionProvider>
  );
}
