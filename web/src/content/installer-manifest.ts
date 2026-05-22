// Installer manifest — single source of truth for marketing-site download
// links. The data itself lives in `installer-manifest.json` so the
// build-time `scripts/gen-install-manifest.mjs` script can read the same
// catalogue without spawning a TypeScript loader; this file wraps it
// in typed accessors for the React side.
//
// The JSON shape mirrors `installer/auto-update/manifest.schema.json` so
// a follow-up controller can drop the hardcoded list in favour of a
// live signed manifest fetched from the daemon-update channel without
// having to change the UI.

import raw from "./installer-manifest.json";

/**
 * Rustc target triple. Matches the enum in
 * installer/auto-update/manifest.schema.json.
 */
export type TargetTriple =
  | "aarch64-apple-darwin"
  | "x86_64-apple-darwin"
  | "x86_64-unknown-linux-gnu"
  | "aarch64-unknown-linux-gnu"
  | "x86_64-pc-windows-msvc"
  | "aarch64-pc-windows-msvc";

/** High-level platform family used by the OS-detection UX. */
export type Platform = "mac" | "win" | "linux";

/** Architecture, normalised to amd64 / arm64. */
export type Arch = "amd64" | "arm64";

/** Linux package format. Undefined for non-Linux artefacts. */
export type LinuxFormat = "deb" | "rpm" | "apk";

export interface Artifact {
  /** Stable per-platform slug used in route paths and manifest keys. */
  id: string;
  /** Human-readable label, e.g. "macOS (Apple Silicon)". */
  label: string;
  /** Secondary label / artefact filename, e.g. "iogrid-0.1.0-arm64.pkg". */
  sublabel: string;
  platform: Platform;
  arch: Arch;
  /** Linux package format. Omitted for macOS / Windows. */
  linuxFormat?: LinuxFormat;
  /** Rustc target triple — links the UX to the auto-update manifest schema. */
  target: TargetTriple;
  /** Absolute download URL on the public CDN host. */
  url: string;
  /** Hex-encoded SHA-256 of the artefact, 64 chars. */
  sha256: string;
  /** Artefact size in bytes for the all-downloads table. */
  sizeBytes: number;
}

interface RawArtifact {
  id: string;
  label: string;
  filename: string;
  platform: Platform;
  arch: Arch;
  linuxFormat: LinuxFormat | null;
  target: TargetTriple;
  sha256: string;
  sizeBytes: number;
}

interface RawManifest {
  version: string;
  channel: string;
  cdn: string;
  artifacts: RawArtifact[];
}

const data = raw as RawManifest;

/**
 * Current released version. Bumped at release time alongside the daemon
 * Cargo.toml + nfpm IOGRID_VERSION + the Phase-1 live manifest service.
 */
export const INSTALLER_VERSION: string = data.version;

/**
 * Full artefact catalogue. Order matches the JSON source and is the
 * display order in the "Other platforms" disclosure on the landing
 * page and the all-downloads table at /install.
 */
export const INSTALLER_ARTIFACTS: readonly Artifact[] = data.artifacts.map(
  (a): Artifact => ({
    id: a.id,
    label: a.label,
    sublabel: a.filename,
    platform: a.platform,
    arch: a.arch,
    linuxFormat: a.linuxFormat ?? undefined,
    target: a.target,
    url: `${data.cdn}/${data.version}/${a.filename}`,
    sha256: a.sha256,
    sizeBytes: a.sizeBytes,
  }),
);

/** Lookup an artefact by stable id. */
export function getArtifactById(id: string): Artifact | undefined {
  return INSTALLER_ARTIFACTS.find((a) => a.id === id);
}

/**
 * Pick the best artefact for a (platform, arch, linuxFormat) tuple, with
 * the platform-specific defaults documented in the README:
 *   - mac default arch: arm64 (the dominant Mac silicon shipped since 2020).
 *   - win default arch: amd64 (no arm64 daemon yet).
 *   - linux default format: .deb (Debian/Ubuntu dominate the consumer Linux
 *     install base and our `installer/install.sh` defaults to .deb).
 *   - linux default arch: amd64.
 *
 * Falls back to the first matching platform artefact if no exact match.
 */
export function pickArtifact(
  platform: Platform,
  arch?: Arch,
  linuxFormat?: LinuxFormat,
): Artifact {
  const candidates = INSTALLER_ARTIFACTS.filter((a) => a.platform === platform);
  if (candidates.length === 0) {
    throw new Error(`no artefacts published for platform=${platform}`);
  }
  let best = candidates[0]!;
  if (platform === "mac") {
    const want = arch ?? "arm64";
    best = candidates.find((a) => a.arch === want) ?? best;
  } else if (platform === "win") {
    const want = arch ?? "amd64";
    best = candidates.find((a) => a.arch === want) ?? best;
  } else {
    const wantArch = arch ?? "amd64";
    const wantFormat = linuxFormat ?? "deb";
    best =
      candidates.find(
        (a) => a.arch === wantArch && a.linuxFormat === wantFormat,
      ) ??
      candidates.find((a) => a.linuxFormat === wantFormat) ??
      best;
  }
  return best;
}
