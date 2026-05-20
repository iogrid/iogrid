import type { NextConfig } from "next";

/**
 * iogrid staff console — Next.js config.
 *
 * Mirrors `web/next.config.ts` shape (standalone output, strict mode,
 * typed routes). No Solana wallet transpile list — admin surfaces do
 * not embed customer-facing wallet flows.
 */
const nextConfig: NextConfig = {
  reactStrictMode: true,
  experimental: {
    typedRoutes: true,
  },
  output: "standalone",
};

export default nextConfig;
