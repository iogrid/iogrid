import type { Metadata } from "next";
import { notFound } from "next/navigation";
import Link from "next/link";
import {
  pickArtifact,
  type Arch,
  type Artifact,
  type LinuxFormat,
  type Platform,
} from "@/content/installer-manifest";

// Catch-all route under /install. Resolves `slug` of the form
// `[os]` or `[os]/[arch-or-format]` to a concrete artefact URL and
// renders a self-redirecting HTML page that:
//   1. Sets `<meta http-equiv="refresh">` so browsers + curl alike see
//      the canonical artefact URL.
//   2. Falls back to a JS redirect for browsers that disable
//      meta-refresh.
//   3. Renders a visible "If you aren't redirected, click here" link
//      so the no-JS / curl-no-redirect-follow case still has a working
//      affordance.
//
// `next.config.ts` sets `output: "export"`, so this dynamic segment
// MUST enumerate every supported slug at build time via
// generateStaticParams + dynamicParams=false. The set is small (12
// permutations) and matches the InstallButtons component link table.

export const dynamicParams = false;

interface SlugResolution {
  artifact: Artifact;
  /** Canonical slug joined back with "/" for display. */
  canonical: string;
}

/**
 * Set of every URL slug the redirect catch-all supports, alongside
 * the artefact each one targets. Kept verbose-but-explicit so a future
 * audit can grep for any path that needs to keep working.
 */
const SLUG_TABLE: ReadonlyArray<{
  slug: string[];
  platform: Platform;
  arch?: Arch;
  linuxFormat?: LinuxFormat;
}> = [
  // macOS — bare `/install/mac` picks the default (arm64); explicit
  // `/install/mac/arm64` + `/install/mac/amd64` allow direct linking.
  { slug: ["mac"], platform: "mac" },
  { slug: ["mac", "arm64"], platform: "mac", arch: "arm64" },
  { slug: ["mac", "amd64"], platform: "mac", arch: "amd64" },
  { slug: ["mac", "intel"], platform: "mac", arch: "amd64" },
  { slug: ["macos"], platform: "mac" },
  { slug: ["darwin"], platform: "mac" },

  // Windows — single arch shipped today; both `/install/win` and the
  // historical `/install/windows` resolve to the .msi.
  { slug: ["win"], platform: "win" },
  { slug: ["win", "amd64"], platform: "win", arch: "amd64" },
  { slug: ["win", "x64"], platform: "win", arch: "amd64" },
  { slug: ["windows"], platform: "win" },

  // Linux — `/install/linux` defaults to .deb amd64 (matches what
  // installer/install.sh defaults to). Per-format overrides for the
  // installer the user actually wants.
  { slug: ["linux"], platform: "linux" },
  { slug: ["linux", "deb"], platform: "linux", linuxFormat: "deb" },
  {
    slug: ["linux", "deb", "amd64"],
    platform: "linux",
    arch: "amd64",
    linuxFormat: "deb",
  },
  {
    slug: ["linux", "deb", "arm64"],
    platform: "linux",
    arch: "arm64",
    linuxFormat: "deb",
  },
  { slug: ["linux", "rpm"], platform: "linux", linuxFormat: "rpm" },
  { slug: ["linux", "apk"], platform: "linux", linuxFormat: "apk" },
  { slug: ["linux", "amd64"], platform: "linux", arch: "amd64" },
  { slug: ["linux", "arm64"], platform: "linux", arch: "arm64" },
];

export function generateStaticParams(): Array<{ slug: string[] }> {
  return SLUG_TABLE.map((row) => ({ slug: row.slug }));
}

function resolveSlug(slug: readonly string[]): SlugResolution | null {
  const lookup = SLUG_TABLE.find(
    (row) =>
      row.slug.length === slug.length &&
      row.slug.every((piece, i) => piece === slug[i]),
  );
  if (!lookup) return null;
  const artifact = pickArtifact(lookup.platform, lookup.arch, lookup.linuxFormat);
  return { artifact, canonical: lookup.slug.join("/") };
}

interface PageProps {
  params: Promise<{ slug: string[] }>;
}

export async function generateMetadata(
  { params }: PageProps,
): Promise<Metadata> {
  const { slug } = await params;
  const r = resolveSlug(slug);
  if (!r) return { title: "Download — not found" };
  return {
    title: `Download iogrid daemon — ${r.artifact.label}`,
    description: `Direct download for ${r.artifact.label}: ${r.artifact.sublabel}.`,
    alternates: { canonical: `/install/${r.canonical}` },
    // No-index the redirect pages: search engines should rank /install,
    // not the throwaway redirect endpoints.
    robots: { index: false, follow: true },
  };
}

export default async function InstallRedirectPage({ params }: PageProps) {
  const { slug } = await params;
  const r = resolveSlug(slug);
  if (!r) notFound();

  const { artifact, canonical } = r;
  // Build the inline JS that runs on document ready. The Next static
  // export pre-renders this page to HTML; we can't use client-side
  // hooks here because <head>'s meta-refresh needs to be present on
  // first byte for the curl / no-JS path.
  const redirectScript = `window.location.replace(${JSON.stringify(artifact.url)})`;

  return (
    <>
      <head>
        <meta httpEquiv="refresh" content={`0; url=${artifact.url}`} />
        <link rel="canonical" href={`/install/${canonical}`} />
      </head>
      <section className="container-page py-24">
        <div className="mx-auto max-w-xl text-center">
          <h1 className="h-section text-neutral-900">
            Downloading {artifact.label}…
          </h1>
          <p className="mt-4 text-lead">
            Your download for{" "}
            <code className="rounded bg-neutral-100 px-1.5 py-0.5 font-mono text-sm">
              {artifact.sublabel}
            </code>{" "}
            should start automatically.
          </p>
          <p className="mt-8">
            <a
              href={artifact.url}
              className="btn-primary"
              data-artifact-id={artifact.id}
            >
              Click here if it doesn&rsquo;t
            </a>
          </p>
          <p className="mt-6 text-sm text-neutral-500">
            SHA-256:{" "}
            <code className="font-mono text-xs">{artifact.sha256}</code>
            <br />
            <Link href="/install" className="text-primary-700 hover:underline">
              See all platforms →
            </Link>
          </p>
        </div>
        {/* JS fallback for browsers where the user disabled
            meta-refresh. `dangerouslySetInnerHTML` is the standard
            React idiom for inline <script>. */}
        <script
          // eslint-disable-next-line react/no-danger
          dangerouslySetInnerHTML={{ __html: redirectScript }}
        />
      </section>
    </>
  );
}
