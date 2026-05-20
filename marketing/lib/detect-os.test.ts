import { describe, expect, it } from "vitest";
import { detectOS, type NavigatorLike } from "./detect-os";

// Fixture user-agent strings sampled from real browsers. The point of
// these tests isn't to exhaustively cover every UA in the wild — it's
// to (a) pin down the resolution table for the platforms iogrid ships
// installers for and (b) regress the mobile + unknown fallbacks so a
// bad detectOS change doesn't silently show "Install for macOS" on an
// iPad.

function nav(partial: Partial<NavigatorLike>): NavigatorLike {
  return { userAgent: "", ...partial };
}

describe("detectOS — macOS", () => {
  it("Safari on Apple Silicon defaults to arm64", () => {
    // Safari 17 on M-series. navigator.platform is frozen at "MacIntel"
    // for fingerprinting reasons; UA carries "Intel Mac OS X" — but the
    // modern Safari `Version/17.x` tag is exclusive to Apple Silicon
    // Macs in practice, so we treat the absence of any other signal
    // as arm64.
    const r = detectOS(
      nav({
        userAgent:
          "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_4) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15",
        platform: "MacIntel",
      }),
    );
    expect(r.platform).toBe("mac");
    expect(r.arch).toBe("arm64");
    expect(r.display).toMatch(/Apple Silicon/);
  });

  it("Chrome on Apple Silicon defaults to arm64", () => {
    const r = detectOS(
      nav({
        userAgent:
          "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
        platform: "MacIntel",
      }),
    );
    expect(r.platform).toBe("mac");
    // Chrome currently builds universal binaries but reports "Intel
    // Mac OS X" on both archs. Detection defaults to arm64; the user
    // gets the explicit Intel link in the "Other platforms" panel.
    expect(r.arch).toBe("arm64");
  });

  it("Firefox on Intel Mac falls into the Mac branch", () => {
    const r = detectOS(
      nav({
        userAgent:
          "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:125.0) Gecko/20100101 Firefox/125.0",
        platform: "MacIntel",
      }),
    );
    expect(r.platform).toBe("mac");
  });
});

describe("detectOS — Windows", () => {
  it("Edge on Windows 11 x64 returns win/amd64", () => {
    const r = detectOS(
      nav({
        userAgent:
          "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36 Edg/124.0.0.0",
        platform: "Win32",
      }),
    );
    expect(r.platform).toBe("win");
    expect(r.arch).toBe("amd64");
    expect(r.display).toMatch(/Windows/);
  });

  it("Firefox on Windows", () => {
    const r = detectOS(
      nav({
        userAgent:
          "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:125.0) Gecko/20100101 Firefox/125.0",
        platform: "Win32",
      }),
    );
    expect(r.platform).toBe("win");
  });
});

describe("detectOS — Linux", () => {
  it("Chrome on Ubuntu x86_64 → linux/amd64/.deb", () => {
    const r = detectOS(
      nav({
        userAgent:
          "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
        platform: "Linux x86_64",
      }),
    );
    expect(r.platform).toBe("linux");
    expect(r.arch).toBe("amd64");
    expect(r.linuxFormat).toBe("deb");
  });

  it("Linux aarch64 → linux/arm64/.deb", () => {
    const r = detectOS(
      nav({
        userAgent:
          "Mozilla/5.0 (X11; Linux aarch64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
        platform: "Linux aarch64",
      }),
    );
    expect(r.platform).toBe("linux");
    expect(r.arch).toBe("arm64");
  });

  it("Fedora UA → linux/.rpm", () => {
    const r = detectOS(
      nav({
        userAgent:
          "Mozilla/5.0 (X11; Fedora; Linux x86_64; rv:125.0) Gecko/20100101 Firefox/125.0",
        platform: "Linux x86_64",
      }),
    );
    expect(r.linuxFormat).toBe("rpm");
  });

  it("Alpine UA → linux/.apk", () => {
    const r = detectOS(
      nav({
        userAgent:
          "Mozilla/5.0 (X11; Linux x86_64; alpine musl) AppleWebKit/537.36",
        platform: "Linux x86_64",
      }),
    );
    expect(r.linuxFormat).toBe("apk");
  });
});

describe("detectOS — mobile + unknown", () => {
  it("iPhone Safari → other (no install button)", () => {
    const r = detectOS(
      nav({
        userAgent:
          "Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1",
        platform: "iPhone",
      }),
    );
    expect(r.platform).toBe("other");
  });

  it("Android Chrome → other", () => {
    const r = detectOS(
      nav({
        userAgent:
          "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Mobile Safari/537.36",
        platform: "Linux armv8l",
      }),
    );
    expect(r.platform).toBe("other");
  });

  it("UA-CH mobile=true overrides desktop-looking UA", () => {
    const r = detectOS(
      nav({
        userAgent: "Mozilla/5.0 (compatible)",
        platform: "Linux x86_64",
        userAgentData: { platform: "Android", mobile: true },
      }),
    );
    expect(r.platform).toBe("other");
  });

  it("empty navigator → other", () => {
    const r = detectOS(nav({ userAgent: "" }));
    expect(r.platform).toBe("other");
  });
});
