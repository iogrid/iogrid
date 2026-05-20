import type { Metadata } from "next";
import Link from "next/link";
import {
  INSTALLER_ARTIFACTS,
  INSTALLER_VERSION,
} from "@/content/installer-manifest";

export const metadata: Metadata = {
  title: "Download iogrid daemon — every platform",
  description:
    "Signed iogrid daemon installers for macOS, Windows, and Linux (deb/rpm/apk). Each download lists its SHA-256 and size; verify before you install.",
  alternates: { canonical: "/install" },
};

// Format bytes as a human-readable size with one decimal place. We
// duplicate this tiny helper here (instead of pulling in a 3rd-party
// `pretty-bytes`) because the static export bundles everything client-
// side and adding 1 kB for a single call-site is not worth it.
function formatSize(bytes: number): string {
  const mb = bytes / 1_048_576;
  if (mb >= 1) return `${mb.toFixed(1)} MB`;
  const kb = bytes / 1024;
  return `${kb.toFixed(0)} KB`;
}

export default function InstallIndexPage() {
  return (
    <section className="container-page py-16">
      <div className="mx-auto max-w-3xl">
        <h1 className="h-hero text-neutral-900">Download iogrid daemon</h1>
        <p className="mt-4 text-lead">
          One static binary per platform. Version{" "}
          <code className="rounded bg-neutral-100 px-1.5 py-0.5 font-mono text-sm text-neutral-700">
            {INSTALLER_VERSION}
          </code>
          . Every artefact is rebuilt on every push to{" "}
          <code className="font-mono">main</code>; release tags are signed with
          cosign + notarised on macOS + Authenticode on Windows.
        </p>

        <div className="mt-10 overflow-x-auto rounded-2xl border border-neutral-200 bg-white">
          <table className="w-full text-left text-sm">
            <thead className="border-b border-neutral-200 bg-neutral-50 text-xs uppercase tracking-wide text-neutral-500">
              <tr>
                <th className="px-4 py-3 font-medium">Platform</th>
                <th className="px-4 py-3 font-medium">Artefact</th>
                <th className="px-4 py-3 font-medium">Size</th>
                <th className="px-4 py-3 font-medium">SHA-256</th>
                <th className="px-4 py-3 font-medium">Download</th>
              </tr>
            </thead>
            <tbody>
              {INSTALLER_ARTIFACTS.map((a) => (
                <tr
                  key={a.id}
                  data-artifact-id={a.id}
                  className="border-b border-neutral-100 last:border-0"
                >
                  <td className="px-4 py-3 font-medium text-neutral-900">
                    {a.label}
                  </td>
                  <td className="px-4 py-3 font-mono text-xs text-neutral-700">
                    {a.sublabel}
                  </td>
                  <td className="px-4 py-3 text-neutral-700">
                    {formatSize(a.sizeBytes)}
                  </td>
                  <td className="px-4 py-3 font-mono text-[11px] text-neutral-500">
                    {/* Truncate to first 12 chars in the table; full value
                        sits in the title attribute for copy-paste. */}
                    <span title={a.sha256}>{a.sha256.slice(0, 12)}…</span>
                  </td>
                  <td className="px-4 py-3">
                    <a
                      href={a.url}
                      className="font-medium text-primary-700 hover:underline"
                      data-artifact-id={a.id}
                    >
                      Download
                    </a>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        <h2 className="h-section mt-16 text-neutral-900">Verify your download</h2>
        <p className="mt-4 text-lead">
          Every artefact ships with a SHA-256 published next to it. Verify on
          your machine before running the installer:
        </p>
        <pre className="mt-6 overflow-x-auto rounded-lg bg-neutral-900 p-4 text-xs text-neutral-100">
          <code>
            {`# macOS / Linux
shasum -a 256 iogrid-${INSTALLER_VERSION}-arm64.pkg
# compare against the value in the table above

# Or fetch the published checksum and diff:
curl -fsSL https://iogrid.org/install/mac/arm64.sha256 \\
  | cmp - <(shasum -a 256 iogrid-${INSTALLER_VERSION}-arm64.pkg | awk '{print $1}')`}
          </code>
        </pre>

        <h2 className="h-section mt-16 text-neutral-900">Terminal install</h2>
        <p className="mt-4 text-lead">
          Prefer the command line? On macOS or Linux:
        </p>
        <pre className="mt-6 overflow-x-auto rounded-lg bg-neutral-900 p-4 text-xs text-neutral-100">
          <code>{`curl -fsSL https://iogrid.org/install/sh | sh`}</code>
        </pre>
        <p className="mt-4 text-sm text-neutral-500">
          The installer script lives at{" "}
          <Link
            href="https://github.com/iogrid/iogrid/blob/main/installer/install.sh"
            className="font-medium text-primary-700 hover:underline"
          >
            installer/install.sh
          </Link>{" "}
          — review it before piping to a shell.
        </p>
      </div>
    </section>
  );
}
