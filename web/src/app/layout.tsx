import type { Metadata } from "next";
import { Inter } from "next/font/google";
import { Toaster } from "sonner";
import { SolanaWalletProvider } from "@/lib/solana/provider";
import { ThemeProvider } from "@/components/theme-provider";
import "./globals.css";

/**
 * Single sans typeface for the entire surface — Inter, self-hosted by
 * `next/font` so we ship zero third-party font CDN calls in production.
 * The variable form gives us the full 100-900 weight range over the
 * 12-64px scale defined in `design-tokens.css` without loading multiple
 * static cuts. Exposed as the `--font-inter` CSS variable so the L1
 * `--font-sans` token (defined in design-tokens.css) can prefer Inter
 * when it is loaded and gracefully fall back to system-ui otherwise.
 */
const inter = Inter({
  subsets: ["latin"],
  display: "swap",
  variable: "--font-inter",
});

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
    // `suppressHydrationWarning` is required by next-themes — the
    // provider mutates `<html class="...">` and `style.colorScheme`
    // before React hydrates so the first paint already matches the
    // resolved theme (system preference or persisted choice).
    <html lang="en" className={inter.variable} suppressHydrationWarning>
      <body className="min-h-screen antialiased">
        {/* ThemeProvider thinly wraps next-themes so we can centralise
            its config (class-based strategy, `system` default,
            enableSystem). The Toaster and Solana provider sit inside
            so toast surfaces + wallet modals pick up the current
            theme automatically. */}
        <ThemeProvider>
          {/* SolanaWalletProvider mounts ConnectionProvider +
              WalletProvider + WalletModalProvider so any client
              component — wallet-bind flow, balance widget, staking UI —
              can call `useWallet()` / `useConnection()` without
              re-wiring. Server Components inside still SSR; the
              wallet subtree only hydrates on the client. */}
          <SolanaWalletProvider>
            {children}
            {/* Sonner toast container — every mutation (block
                category, save schedule, create API key, ...) routes
                through `toast.*` so we can swap implementations
                without touching call sites. `theme="system"` lets
                Sonner follow the same `prefers-color-scheme` signal
                as the rest of the UI. */}
            <Toaster
              richColors
              closeButton
              position="top-right"
              theme="system"
            />
          </SolanaWalletProvider>
        </ThemeProvider>
      </body>
    </html>
  );
}
