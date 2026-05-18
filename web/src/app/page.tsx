import Link from "next/link";

export default function HomePage() {
  return (
    <main className="mx-auto max-w-5xl px-6 py-16">
      <nav className="mb-12 flex items-center justify-between">
        <Link href="/" className="text-xl font-bold tracking-tight">
          iogrid
        </Link>
        <ul className="flex gap-6 text-sm">
          <li>
            <Link href="/provide" className="hover:underline">
              Provide
            </Link>
          </li>
          <li>
            <Link href="/customer" className="hover:underline">
              Customer
            </Link>
          </li>
          <li>
            <Link href="/vpn" className="hover:underline">
              VPN
            </Link>
          </li>
          <li>
            <Link href="/account" className="hover:underline">
              Account
            </Link>
          </li>
        </ul>
      </nav>

      <section>
        <h1 className="text-5xl font-bold tracking-tight">
          iogrid — Distributed compute mesh
        </h1>
        <p className="mt-6 max-w-2xl text-lg text-zinc-600 dark:text-zinc-400">
          Pool idle CPUs, GPUs, and edge boxes into a single schedulable fleet.
          Providers earn for the spare cycles they contribute; customers run
          workloads on a network that is cheaper, more resilient, and closer to
          the data than centralised clouds.
        </p>
        <div className="mt-8 flex gap-4">
          <Link
            href="/provide"
            className="rounded-md bg-zinc-900 px-5 py-3 text-sm font-medium text-white hover:bg-zinc-700"
          >
            Become a provider
          </Link>
          <Link
            href="/customer"
            className="rounded-md border border-zinc-300 px-5 py-3 text-sm font-medium hover:bg-zinc-50"
          >
            Run workloads
          </Link>
        </div>
      </section>
    </main>
  );
}
