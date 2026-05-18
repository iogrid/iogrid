import Link from "next/link";

export default function CustomerDashboardPage() {
  return (
    <main className="mx-auto max-w-6xl px-6 py-12">
      <Link href="/" className="text-sm text-zinc-500 hover:underline">
        ← Home
      </Link>
      <header className="mt-4">
        <h1 className="text-3xl font-bold">Customer dashboard</h1>
        <p className="mt-1 text-sm text-zinc-600 dark:text-zinc-400">
          Submit workloads, monitor scheduling, and view per-workload billing.
        </p>
      </header>

      <section className="mt-10 grid grid-cols-1 gap-4 md:grid-cols-3">
        <div className="rounded-lg border border-zinc-200 p-5">
          <h2 className="text-sm font-medium text-zinc-500">
            Running workloads
          </h2>
          <p className="mt-2 text-3xl font-semibold">—</p>
        </div>
        <div className="rounded-lg border border-zinc-200 p-5">
          <h2 className="text-sm font-medium text-zinc-500">
            Spend this month
          </h2>
          <p className="mt-2 text-3xl font-semibold">$—</p>
        </div>
        <div className="rounded-lg border border-zinc-200 p-5">
          <h2 className="text-sm font-medium text-zinc-500">
            Avg. dispatch latency
          </h2>
          <p className="mt-2 text-3xl font-semibold">—</p>
        </div>
      </section>
    </main>
  );
}
