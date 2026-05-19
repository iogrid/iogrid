import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  reactStrictMode: true,
  experimental: {
    typedRoutes: true,
  },
  // i18n is configured at the routing layer via the App Router middleware
  // (Next.js 15 App Router uses route segments, not the legacy `i18n` config).
  // Supported locales: en, es, pt, de, fr, it, tr.
  // See: src/middleware.ts and src/i18n/config.ts.
  output: "standalone",
  // The Solana wallet-adapter family ships ESM-only modules that
  // depend on each other through bare `*.mjs` entrypoints. Next.js 15
  // transpiles them when listed here, otherwise the standalone build
  // fails with "Cannot use import statement outside a module".
  transpilePackages: [
    "@solana/wallet-adapter-base",
    "@solana/wallet-adapter-phantom",
    "@solana/wallet-adapter-react",
    "@solana/wallet-adapter-react-ui",
    "@solana/wallet-adapter-solflare",
    "@solana/wallet-adapter-trust",
  ],
};

export default nextConfig;
