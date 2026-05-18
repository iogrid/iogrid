import Link from "next/link";

export default function ProvideDashboardPage() {
  return (
    <main className="mx-auto max-w-6xl px-6 py-12">
      <Link href="/" className="text-sm text-zinc-500 hover:underline">
        ← Home
      </Link>
      <header className="mt-4 flex items-end justify-between">
        <div>
          <h1 className="text-3xl font-bold">Provider dashboard</h1>
          <p className="mt-1 text-sm text-zinc-600 dark:text-zinc-400">
            Manage your contributed machines, payouts, and reliability score.
          </p>
        </div>
        <Link
          href="/vpn"
          className="rounded-md bg-zinc-900 px-4 py-2 text-sm font-medium text-white hover:bg-zinc-700"
        >
          Install the daemon
        </Link>
      </header>

      <section className="mt-10 grid grid-cols-1 gap-4 md:grid-cols-3">
        <div className="rounded-lg border border-zinc-200 p-5">
          <h2 className="text-sm font-medium text-zinc-500">Active nodes</h2>
          <p className="mt-2 text-3xl font-semibold">—</p>
        </div>
        <div className="rounded-lg border border-zinc-200 p-5">
          <h2 className="text-sm font-medium text-zinc-500">
            Earnings this month
          </h2>
          <p className="mt-2 text-3xl font-semibold">$—</p>
        </div>
        <div className="rounded-lg border border-zinc-200 p-5">
          <h2 className="text-sm font-medium text-zinc-500">Reliability</h2>
          <p className="mt-2 text-3xl font-semibold">—</p>
        </div>
      </section>
    </main>
  );
}
