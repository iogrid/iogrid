// astro.config.mjs — docs.iogrid.org
//
// Starlight (Astro) was chosen over Mintlify because:
//   - Mintlify ties to their hosted SaaS (search, analytics) — we want a fully
//     static deploy behind our own nginx, no third-party runtime dependency.
//   - Starlight ships pure static HTML + a built-in Pagefind search index
//     (client-side WASM, no backend), which matches how marketing/ already
//     deploys (multi-stage Dockerfile -> nginx:alpine).
//   - MIT-licensed, OSS, easy MDX, Astro's island architecture keeps JS tiny.
//
// The OpenAPI integration auto-renders the spec at ../proto/gen/openapi/iogrid.yaml
// when it exists; it falls back gracefully if the SDK agent hasn't shipped it yet.

import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";
import starlightOpenAPI, { openAPISidebarGroups } from "starlight-openapi";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

// Brand tokens — single source of truth in ../brand/tokens.
// Imported via static JSON to avoid runtime Tailwind dependency (Starlight has its
// own CSS pipeline; we only need the primary brand colours mapped).
const colorsTokensPath = path.resolve(__dirname, "../brand/tokens/colors.json");
const colors = JSON.parse(fs.readFileSync(colorsTokensPath, "utf8"));

const SITE_URL = process.env.SITE_URL ?? "https://docs.iogrid.org";
// Prefer the local public/openapi.yaml (copied by scripts/fetch-openapi.mjs)
// over the raw proto/gen path so a stub can substitute when the SDK pipeline
// hasn't shipped the real spec yet.
const LOCAL_OPENAPI = path.resolve(__dirname, "public", "openapi.yaml");
const PROTO_OPENAPI = path.resolve(__dirname, "..", "proto", "gen", "openapi", "iogrid.yaml");
const OPENAPI_SPEC = fs.existsSync(LOCAL_OPENAPI)
  ? LOCAL_OPENAPI
  : fs.existsSync(PROTO_OPENAPI)
    ? PROTO_OPENAPI
    : null;

if (!OPENAPI_SPEC) {
  console.warn(
    `[docs-site] No OpenAPI spec found. ` +
      `Looked at ${LOCAL_OPENAPI} and ${PROTO_OPENAPI}. ` +
      `API reference auto-generation will fall back to a placeholder sidebar entry. ` +
      `Run 'node scripts/fetch-openapi.mjs' to populate.`,
  );
}

const openAPIPlugins = OPENAPI_SPEC
  ? [
      starlightOpenAPI([
        {
          base: "api/reference",
          label: "API reference",
          schema: OPENAPI_SPEC,
        },
      ]),
    ]
  : [];

const apiSidebar = OPENAPI_SPEC
  ? openAPISidebarGroups
  : [
      {
        label: "Reference (coming soon)",
        items: [
          {
            label: "Spec not yet published",
            link: "/api/overview/",
          },
        ],
      },
    ];

export default defineConfig({
  site: SITE_URL,
  outDir: "./dist",
  trailingSlash: "always",
  integrations: [
    starlight({
      title: "iogrid docs",
      description:
        "Documentation for iogrid — the open mesh for compute. Provider install, customer API, workload guides, $GRID tokenomics, and the anti-Hola transparency dashboard.",
      logo: {
        src: "./src/assets/logo.svg",
        replacesTitle: false,
      },
      favicon: "/favicon.svg",
      head: [
        {
          tag: "meta",
          attrs: { name: "theme-color", content: colors.primary["500"].value },
        },
        {
          tag: "link",
          attrs: { rel: "canonical", href: SITE_URL },
        },
        {
          tag: "meta",
          attrs: {
            property: "og:image",
            content: `${SITE_URL}/og-default.png`,
          },
        },
        {
          tag: "meta",
          attrs: { name: "twitter:card", content: "summary_large_image" },
        },
        // JSON-LD organization schema (issue #113 — SEO baseline).
        {
          tag: "script",
          attrs: { type: "application/ld+json" },
          content: JSON.stringify({
            "@context": "https://schema.org",
            "@type": "Organization",
            name: "iogrid",
            url: "https://iogrid.org",
            logo: "https://iogrid.org/logo.svg",
            sameAs: [
              "https://github.com/iogrid/iogrid",
              "https://twitter.com/iogrid",
            ],
          }),
        },
      ],
      social: [
        {
          icon: "github",
          label: "GitHub",
          href: "https://github.com/iogrid/iogrid",
        },
      ],
      editLink: {
        baseUrl:
          "https://github.com/iogrid/iogrid/edit/main/docs-site/src/content/docs/",
      },
      lastUpdated: true,
      pagination: true,
      customCss: ["./src/styles/brand.css"],
      plugins: [...openAPIPlugins],
      sidebar: [
        {
          label: "Welcome",
          items: [
            { label: "What is iogrid?", link: "/" },
            { label: "Quickstart (customers)", link: "/getting-started/quickstart/" },
            {
              label: "Quickstart (providers)",
              link: "/getting-started/provider-quickstart/",
            },
          ],
        },
        {
          label: "Concepts",
          items: [
            { label: "Mesh overview", link: "/concepts/overview/" },
            { label: "Workload types", link: "/concepts/workloads/" },
            { label: "Providers (the supply side)", link: "/concepts/providers/" },
            {
              label: "Transparency (anti-Hola)",
              link: "/concepts/transparency/",
            },
            { label: "$GRID tokenomics", link: "/concepts/tokenomics/" },
          ],
        },
        {
          label: "Workloads",
          items: [
            { label: "Bandwidth proxy", link: "/workloads/proxy/" },
            { label: "Docker compute", link: "/workloads/compute/" },
            { label: "GPU / AI inference", link: "/workloads/gpu/" },
            { label: "iOS build CI", link: "/workloads/ios-build/" },
          ],
        },
        {
          label: "API",
          items: [
            { label: "Overview", link: "/api/overview/" },
            { label: "Authentication", link: "/api/authentication/" },
            { label: "Rate limits", link: "/api/rate-limits/" },
            { label: "Errors", link: "/api/errors/" },
            ...apiSidebar,
          ],
        },
        {
          label: "SDKs",
          items: [
            { label: "TypeScript", link: "/sdks/typescript/" },
            { label: "Python", link: "/sdks/python/" },
            { label: "Go", link: "/sdks/go/" },
            { label: "Java", link: "/sdks/java/" },
          ],
        },
        {
          label: "Billing",
          items: [
            { label: "Plans & pricing", link: "/billing/plans/" },
            { label: "Provider payouts", link: "/billing/payouts/" },
          ],
        },
        {
          label: "Security",
          items: [
            { label: "Privacy posture", link: "/security/privacy/" },
            { label: "Anti-abuse (AUP)", link: "/security/anti-abuse/" },
          ],
        },
        {
          label: "Legal",
          items: [
            { label: "Terms of Service", link: "/legal/tos/" },
            { label: "Acceptable Use Policy", link: "/legal/aup/" },
            { label: "Privacy Policy", link: "/legal/privacy/" },
          ],
        },
        {
          label: "Resources",
          items: [
            { label: "Changelog", link: "/changelog/" },
            { label: "Blog", link: "/blog/" },
          ],
        },
      ],
    }),
  ],
});
