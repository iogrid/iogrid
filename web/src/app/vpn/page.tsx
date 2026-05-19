import Link from "next/link";
import { Downloads } from "./downloads";

export const metadata = { title: "iogrid VPN — Install" };

/**
 * /vpn — consumer-facing landing page for the daemon download +
 * iogrid VPN. Pure marketing chrome (no auth required) and a single
 * client island that fetches the per-platform config artefact.
 */
export default function VpnPage() {
  return (
    <main className="mx-auto max-w-3xl px-6 py-12">
      <Link href="/" className="text-sm text-zinc-500 hover:underline">
        ← Home
      </Link>
      <h1 className="mt-6 text-3xl font-bold">Install the iogrid daemon</h1>
      <p className="mt-2 text-sm text-zinc-600 dark:text-zinc-400">
        The iogrid daemon joins your machine to the mesh over an authenticated
        VPN tunnel and runs metered workloads inside isolated micro-VMs. No
        inbound ports required.
      </p>

      <Downloads />

      <p className="mt-8 text-xs text-zinc-500">
        Builds are signed and reproducible. SHA-256 checksums are listed on the
        release page; GPG signatures are available alongside each artifact.
      </p>

      <section className="mt-12 rounded-md border border-zinc-200 bg-white p-5 dark:border-zinc-800 dark:bg-zinc-900">
        <h2 className="text-lg font-semibold">Consumer VPN</h2>
        <p className="mt-1 text-sm text-zinc-600 dark:text-zinc-400">
          Already running the daemon? You can use the same identity to route
          your personal browsing through any iogrid provider in the mesh.
        </p>
        <Link
          href="/vpn/upgrade"
          className="mt-3 inline-block rounded-md bg-zinc-900 px-4 py-2 text-sm font-medium text-white hover:bg-zinc-700 dark:bg-zinc-100 dark:text-zinc-900"
        >
          Upgrade to Plus / Pro
        </Link>
      </section>
    </main>
  );
}
