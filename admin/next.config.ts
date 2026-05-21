import type { NextConfig } from "next";

/**
 * admin/ — independent Next.js 15 management console for admin.iogrid.org
 * (EPIC #422 Phase 1).
 *
 * No Solana wallet adapter transpile entries — the admin console does NOT
 * render the provider/customer wallet flows, those live in web/. Keeping
 * the bundle slim is part of the strict-separation invariant.
 */
const nextConfig: NextConfig = {
  reactStrictMode: true,
  experimental: {
    typedRoutes: true,
  },
  output: "standalone",
};

export default nextConfig;
