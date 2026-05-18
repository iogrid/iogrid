import Link from "next/link";

export default function VpnDownloadPage() {
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

      <section className="mt-10 grid grid-cols-1 gap-4 md:grid-cols-3">
        <DownloadCard
          os="macOS"
          arch="Apple silicon"
          filename="iogrid-darwin-arm64.pkg"
        />
        <DownloadCard
          os="Linux"
          arch="x86_64 / arm64"
          filename="iogrid-linux.tar.gz"
        />
        <DownloadCard
          os="Windows"
          arch="x86_64"
          filename="iogrid-windows.msi"
        />
      </section>

      <p className="mt-8 text-xs text-zinc-500">
        Builds are signed and reproducible. SHA-256 checksums are listed on the
        release page; GPG signatures are available alongside each artifact.
      </p>
    </main>
  );
}

function DownloadCard({
  os,
  arch,
  filename,
}: {
  os: string;
  arch: string;
  filename: string;
}) {
  return (
    <div className="rounded-lg border border-zinc-200 p-5">
      <h2 className="font-semibold">{os}</h2>
      <p className="text-xs text-zinc-500">{arch}</p>
      <p className="mt-4 text-xs font-mono text-zinc-600">{filename}</p>
      <button
        type="button"
        className="mt-4 w-full rounded-md bg-zinc-900 px-3 py-2 text-sm font-medium text-white hover:bg-zinc-700"
      >
        Download
      </button>
    </div>
  );
}
