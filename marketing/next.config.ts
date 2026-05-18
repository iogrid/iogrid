import type { NextConfig } from "next";

const config: NextConfig = {
  // Static export for any-host deploy (Vercel, Cloudflare Pages, S3+CloudFront, k8s nginx).
  output: "export",
  // App Router's dynamic blog routes need an explicit list of slugs at build time,
  // which the [slug] page provides via generateStaticParams.
  images: {
    unoptimized: true,
  },
  trailingSlash: true,
  reactStrictMode: true,
  poweredByHeader: false,
  experimental: {
    typedRoutes: false,
  },
};

export default config;
