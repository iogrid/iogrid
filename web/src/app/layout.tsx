import type { Metadata } from "next";
import { Toaster } from "sonner";
import { SolanaWalletProvider } from "@/lib/solana/provider";
import "./globals.css";

export const metadata: Metadata = {
  title: "iogrid — Distributed compute mesh",
  description:
    "iogrid is a distributed compute mesh that turns idle machines into a shared, schedulable fleet.",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en">
      <body className="min-h-screen antialiased">
        {/* SolanaWalletProvider mounts ConnectionProvider +
            WalletProvider + WalletModalProvider so any client component
            — wallet-bind flow, balance widget, staking UI — can call
            `useWallet()` / `useConnection()` without re-wiring. Server
            Components inside still SSR; the wallet subtree only
            hydrates on the client. */}
        <SolanaWalletProvider>
          {children}
          {/* Sonner toast container — every mutation (block category, save
              schedule, create API key, ...) routes through `toast.*` so we
              can swap implementations without touching call sites. */}
          <Toaster richColors closeButton position="top-right" />
        </SolanaWalletProvider>
      </body>
    </html>
  );
}
