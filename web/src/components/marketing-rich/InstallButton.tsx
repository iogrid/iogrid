"use client";

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { detectOS, type DetectedOS } from "@/lib/detect-os";
import {
  INSTALLER_ARTIFACTS,
  pickArtifact,
  type Artifact,
} from "@/content/installer-manifest";

/**
 * OS-detecting install CTA used in the landing-page hero + the
 * "Install in two minutes" section. SSR-safe: the first render emits
 * the platform-neutral "Choose your platform" state so the static
 * export checksum-matches across pages, then hydration sniffs the
 * client `navigator` and swaps in a concrete CTA.
 *
 * Behaviour:
 *   - Detected desktop platform → primary button reads "Install for
 *     macOS (Apple Silicon)" (or the matching family) and links to the
 *     latest signed artefact for that target.
 *   - Mobile / unknown → primary button links to /install/ (all
 *     downloads).
 *   - "Other platforms" disclosure always shows every artefact with
 *     explicit OS+arch labels.
 */
export function InstallButton({
  variant = "primary",
}: {
  variant?: "primary" | "secondary";
}) {
  const [detected, setDetected] = useState<DetectedOS | null>(null);
  const [open, setOpen] = useState(false);

  // SSR: render the neutral state; on mount, sniff the live navigator.
  useEffect(() => {
    if (typeof navigator === "undefined") return;
    setDetected(detectOS(navigator));
  }, []);

  const primary = useMemo<{
    href: string;
    label: string;
    artifact: Artifact | null;
  }>(() => {
    if (!detected || detected.platform === "other") {
      return { href: "/install", label: "Choose your platform", artifact: null };
    }
    const artifact = pickArtifact(
      detected.platform,
      detected.arch,
      detected.linuxFormat,
    );
    return {
      href: artifact.url,
      label: `Install for ${detected.display}`,
      artifact,
    };
  }, [detected]);

  const btnClass = variant === "primary" ? "btn-primary" : "btn-secondary";

  return (
    <div className="flex flex-col items-stretch gap-4">
      <Link
        href={primary.href}
        className={`${btnClass} justify-center text-base`}
        aria-label={
          primary.artifact
            ? `Download ${primary.artifact.label} installer (${primary.artifact.sublabel})`
            : "Choose an installer for your platform"
        }
        data-platform={detected?.platform ?? "ssr"}
        data-artifact-id={primary.artifact?.id ?? ""}
        // Open downloads in the same tab so the browser's default download
        // handler kicks in; .pkg / .msi / .deb / .rpm / .apk are all
        // non-renderable mime types and will trigger a Save dialog rather
        // than navigating away.
      >
        {primary.label}
      </Link>

      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="text-center text-sm font-medium text-primary-700 hover:underline"
        aria-expanded={open}
        aria-controls="install-other-platforms"
      >
        {open ? "Hide other platforms" : "Other platforms"}
      </button>

      {open && (
        <ul
          id="install-other-platforms"
          className="grid gap-2 rounded-xl border border-neutral-200 bg-white p-4 sm:grid-cols-2"
        >
          {INSTALLER_ARTIFACTS.map((a) => (
            <li key={a.id}>
              <a
                href={a.url}
                className="flex flex-col rounded-lg border border-neutral-200 px-3 py-2 text-sm hover:border-primary-500 hover:bg-primary-50"
                title={`SHA-256: ${a.sha256}`}
                data-artifact-id={a.id}
              >
                <span className="font-medium text-neutral-900">{a.label}</span>
                <span className="text-xs font-mono text-neutral-500">
                  {a.sublabel}
                </span>
              </a>
            </li>
          ))}
          <li className="sm:col-span-2">
            <Link
              href="/install"
              className="block rounded-lg px-3 py-2 text-center text-xs text-neutral-500 hover:text-primary-700 hover:underline"
            >
              View full downloads page with checksums →
            </Link>
          </li>
        </ul>
      )}
    </div>
  );
}
