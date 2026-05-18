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
};

export default nextConfig;
