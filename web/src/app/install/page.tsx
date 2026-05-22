import Link from "next/link";
import { PortalShell } from "@/components/layout/portal-shell";
import { MarketingShell } from "@/components/marketing/marketing-shell";

/**
 * /install — the "grandma button" landing page.
 *
 * Pre-detected platform-appropriate installer link, plus the
 * `curl | sh` snippet for power users, plus links to every platform's
 * signed package.
 *
 * The page is a Server Component so we can read the User-Agent and pick
 * the default tab, but it falls back gracefully when JS is disabled.
 *
 * Wrapped in PortalShell so anyone landing here from the marketing
 * site (or from the legacy /vpn redirect, see #306) keeps the standard
 * Provide / Customer / VPN / Account top nav. The VPN tab is marked
 * active because /vpn redirects here and the daemon is the VPN client.
 */
export const metadata = {
  title: "Install iogrid",
  description:
    "Grandma-proof installers for Mac, Windows, and Linux. Plus curl-pipe-sh for developers.",
};

// Order matters: shown left-to-right as tabs / left column.
const PLATFORMS = [
  {
    id: "mac",
    label: "macOS",
    sub: "12.0+ (Apple Silicon + Intel)",
    curl: "curl -fsSL https://iogrid.org/install/mac | sh",
    pkgs: [
      { arch: "Apple Silicon", url: "https://releases.iogrid.org/latest/iogrid-darwin-arm64.pkg" },
      { arch: "Intel",         url: "https://releases.iogrid.org/latest/iogrid-darwin-amd64.pkg" },
    ],
  },
  {
    id: "win",
    label: "Windows",
    sub: "Windows 10 / 11",
    curl: "iwr -useb https://iogrid.org/install/win | iex",
    pkgs: [
      { arch: "x64",   url: "https://releases.iogrid.org/latest/iogrid-windows-x64.msi" },
      { arch: "ARM64", url: "https://releases.iogrid.org/latest/iogrid-windows-arm64.msi" },
    ],
  },
  {
    id: "linux",
    label: "Linux",
    sub: ".deb / .rpm / .apk + curl-pipe-sh",
    curl: "curl -fsSL https://iogrid.org/install/linux | sudo sh",
    pkgs: [
      { arch: ".deb (amd64)",  url: "https://releases.iogrid.org/latest/iogrid-linux-amd64.deb" },
      { arch: ".deb (arm64)",  url: "https://releases.iogrid.org/latest/iogrid-linux-arm64.deb" },
      { arch: ".rpm (x86_64)", url: "https://releases.iogrid.org/latest/iogrid-linux-x86_64.rpm" },
      { arch: ".rpm (arm64)",  url: "https://releases.iogrid.org/latest/iogrid-linux-aarch64.rpm" },
      { arch: ".apk (x86_64)", url: "https://releases.iogrid.org/latest/iogrid-linux-x86_64.apk" },
    ],
  },
] as const;

export default function InstallPage() {
  return (
    <MarketingShell>
    <PortalShell
      badge="VPN"
      title="Install iogrid"
      subtitle="Pick your platform. The installer drops the daemon, registers it to auto-start, and opens your browser to finish setup. Total time on a 100 Mbit connection: under 2 minutes. The same daemon powers both provider workloads and the iogrid VPN client."
      nav={[]}
      activeHref="/vpn"
    >
      <div className="space-y-8">
        {PLATFORMS.map((p) => (
          <section
            key={p.id}
            id={p.id}
            className="rounded-lg border border-border p-6"
            aria-labelledby={`install-${p.id}`}
          >
            <h2
              id={`install-${p.id}`}
              className="text-xl font-semibold tracking-tight"
            >
              {p.label}
              <span className="ml-2 text-sm font-normal text-muted-foreground">
                {p.sub}
              </span>
            </h2>

            <div className="mt-4 grid gap-3 sm:grid-cols-2">
              {p.pkgs.map((pkg) => (
                <a
                  key={pkg.url}
                  href={pkg.url}
                  className="flex items-center justify-between rounded-md border border-border px-3 py-2 text-sm hover:bg-muted"
                >
                  <span>{pkg.arch}</span>
                  <span className="text-xs text-muted-foreground">Download ↓</span>
                </a>
              ))}
            </div>

            <details className="mt-4 text-sm">
              <summary className="cursor-pointer text-foreground">
                Prefer the terminal?
              </summary>
              <pre className="mt-2 overflow-x-auto rounded-md bg-foreground px-3 py-2 font-mono text-xs text-background">
                {p.curl}
              </pre>
            </details>
          </section>
        ))}
      </div>

      <section className="mt-12 rounded-lg bg-muted p-6">
        <h2 className="text-lg font-semibold">After install</h2>
        <ol className="mt-2 list-decimal space-y-1 pl-6 text-sm text-foreground">
          <li>A browser tab opens with a 6-character pairing code in the URL.</li>
          <li>Sign in with Google or email (we send a one-tap magic link).</li>
          <li>Pick three sensible defaults — bandwidth cap, categories, payout.</li>
          <li>Your machine starts contributing when it&apos;s idle.</li>
        </ol>
      </section>

      <section className="mt-8 flex flex-wrap items-center gap-4 text-sm text-muted-foreground">
        <p>
          Already have a pairing code?{" "}
          <Link href="/onboard" className="underline">
            Enter it manually
          </Link>
          .
        </p>
        <p>
          Want a paid VPN tier?{" "}
          <Link href="/vpn/upgrade" className="underline">
            Compare plans
          </Link>
          .
        </p>
      </section>
    </PortalShell>
    </MarketingShell>
  );
}
