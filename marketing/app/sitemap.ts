import type { MetadataRoute } from "next";
import { posts } from "@/content/posts";

const SITE_URL = process.env.NEXT_PUBLIC_SITE_URL ?? "https://iogrid.org";

// Required for `output: "export"` — sitemap.xml is generated at build time.
export const dynamic = "force-static";

export default function sitemap(): MetadataRoute.Sitemap {
  const now = new Date();
  const staticRoutes = [
    "",
    "/proxy",
    "/compute",
    "/gpu",
    "/ios-build",
    "/vpn",
    "/pricing",
    "/providers",
    "/token",
    "/blog",
    "/docs",
    "/about",
    "/legal/tos",
    "/legal/privacy",
    "/legal/aup",
  ].map((path) => ({
    url: `${SITE_URL}${path}`,
    lastModified: now,
    changeFrequency: "weekly" as const,
    priority: path === "" ? 1.0 : 0.7,
  }));

  const blogRoutes = posts.map((post) => ({
    url: `${SITE_URL}/blog/${post.slug}`,
    lastModified: post.date ? new Date(post.date) : now,
    changeFrequency: "monthly" as const,
    priority: 0.6,
  }));

  return [...staticRoutes, ...blogRoutes];
}
