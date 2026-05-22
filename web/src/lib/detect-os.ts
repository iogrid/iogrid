// User-agent + platform sniffer used by the install-CTA on the landing
// page and the /install/[os] redirect pages. Kept pure (no DOM access)
// so it's directly testable from vitest.
//
// We deliberately do NOT use the Client Hints API
// (`navigator.userAgentData`) as the primary detection path: it's
// Chromium-only, Firefox + Safari never shipped it, and falling back to
// the legacy UA string is required regardless. Using the legacy string
// alone is simpler and works in every browser we care about.

import type { Arch, LinuxFormat, Platform } from "@/content/installer-manifest";

/** Result of sniffing a navigator-style input. */
export interface DetectedOS {
  /** Coarse platform family or "other" for mobile / unknown. */
  platform: Platform | "other";
  /** Architecture if it can be inferred; undefined otherwise. */
  arch?: Arch;
  /** For Linux only: best-guess package format from the UA. */
  linuxFormat?: LinuxFormat;
  /** Human-readable display string for the CTA, e.g. "macOS (Apple Silicon)". */
  display: string;
}

/** Subset of `navigator` we need — keeps tests free of jsdom. */
export interface NavigatorLike {
  readonly userAgent: string;
  /** Legacy field. Optional because Firefox dropped it on `navigator`. */
  readonly platform?: string;
  /**
   * `navigator.userAgentData` when present (Chromium 90+). Type kept
   * intentionally loose because the spec field is not in lib.dom.d.ts
   * on every TypeScript version we ship under.
   */
  readonly userAgentData?: {
    readonly platform?: string;
    readonly mobile?: boolean;
    readonly brands?: ReadonlyArray<{ brand: string; version: string }>;
  };
}

/**
 * Sniff platform + arch + Linux package format from a navigator object.
 *
 * The function never throws: a fully-unknown input returns
 * `{ platform: "other", display: "your device" }` so callers can render
 * a graceful "see all downloads" fallback.
 */
export function detectOS(nav: NavigatorLike): DetectedOS {
  const ua = (nav.userAgent ?? "").toLowerCase();
  const legacyPlatform = (nav.platform ?? "").toLowerCase();
  const hintsPlatform = (nav.userAgentData?.platform ?? "").toLowerCase();
  const isMobileHint = nav.userAgentData?.mobile === true;

  // Phones + tablets are consume-only (VPN); the install download UX is
  // for desktop providers. Detect and fall through to "other" so the
  // CTA points at the all-downloads page rather than mis-claiming
  // "Install for macOS" on an iPad.
  const isMobile =
    isMobileHint ||
    /android|iphone|ipad|ipod|mobile|webos|blackberry|iemobile|opera mini/.test(
      ua,
    );
  if (isMobile) {
    return { platform: "other", display: "your device" };
  }

  // ---- macOS ----------------------------------------------------------
  // navigator.platform on Safari is "MacIntel" for both Apple Silicon
  // and Intel Macs (Apple deliberately froze the value to avoid
  // fingerprinting). We disambiguate via two heuristics, in order:
  //   1. UA-CH hints platform field — Chromium reports "macOS" but
  //      the brands include `Apple` / arch isn't exposed; not enough.
  //   2. UA-string substring matches — Safari + Chrome on M-series
  //      Macs run under Rosetta when the browser is x86 and report
  //      "Intel Mac OS X" in the UA. There is no reliable browser-side
  //      way to detect Apple Silicon today. We default to arm64 because:
  //      (a) every Mac sold since 2020 ships M1/M2/M3/M4,
  //      (b) macOS arm64 installers run on Intel via Rosetta — broken UX
  //          but functional. The reverse (Intel installer on arm64) is
  //          common and "just works" too, but the user gets the
  //          Rosetta-translated daemon — slower.
  // Net: serve arm64 by default, leave the explicit "Other platforms"
  // disclosure as the escape hatch for Intel users.
  const isMac =
    legacyPlatform.startsWith("mac") ||
    hintsPlatform === "macos" ||
    /mac os x|macintosh|mac_powerpc/.test(ua);
  if (isMac) {
    // Browser-side architecture detection on macOS is unreliable: Safari +
    // Chrome both report "Intel Mac OS X" in their UA on Apple Silicon
    // (Apple froze the value to defeat fingerprinting). The only signal
    // that distinguishes the two is `navigator.userAgentData.brands` ARM
    // hint on Chromium 100+, and even there the "Mac" platform string
    // doesn't carry an arch.
    //
    // Policy: default to arm64 because (a) every Mac sold since late 2020
    // is M-series, (b) the arm64 daemon runs on Intel under Rosetta — a
    // degraded but functional fallback — while the Intel daemon will not
    // boot on Apple Silicon at all without Rosetta installed. Intel
    // users self-select via the explicit "Other platforms" disclosure.
    const arch: Arch = "arm64";
    return {
      platform: "mac",
      arch,
      display: "macOS (Apple Silicon)",
    };
  }

  // ---- Windows --------------------------------------------------------
  // "Win64" / "WOW64" / "x64" → amd64. "ARM64" → arm64 (no Windows
  // arm64 daemon yet; fall back to amd64 — runs under x86 emulation).
  const isWin =
    legacyPlatform.startsWith("win") ||
    hintsPlatform === "windows" ||
    /windows nt|win64|wow64/.test(ua);
  if (isWin) {
    const arch: Arch = "amd64";
    return { platform: "win", arch, display: "Windows (x64)" };
  }

  // ---- Linux ----------------------------------------------------------
  const isLinux =
    legacyPlatform.startsWith("linux") ||
    hintsPlatform === "linux" ||
    /linux|x11/.test(ua);
  if (isLinux) {
    // arch — UA exposes "aarch64" / "arm64" reliably on Chromium Linux.
    const arch: Arch = /aarch64|arm64/.test(ua) ? "arm64" : "amd64";
    // package format — distro hints are sparse in browser UA strings.
    // Firefox + Chromium leak nothing distro-specific; only the
    // `Distribution/...` token Ubuntu's apt-installed Firefox sometimes
    // carries is useful. We check a small allow-list and default to
    // .deb (which is what `installer/install.sh` also defaults to).
    let linuxFormat: LinuxFormat = "deb";
    if (/fedora|red hat|rhel|centos|rocky|alma|opensuse|suse/.test(ua)) {
      linuxFormat = "rpm";
    } else if (/alpine|musl/.test(ua)) {
      linuxFormat = "apk";
    }
    const archLabel = arch === "arm64" ? "arm64" : "x64";
    return {
      platform: "linux",
      arch,
      linuxFormat,
      display: `Linux (.${linuxFormat} · ${archLabel})`,
    };
  }

  return { platform: "other", display: "your device" };
}
