import Link from "next/link";

export default function AdminPage() {
  return (
    <main className="mx-auto max-w-6xl px-6 py-12">
      <Link href="/" className="text-sm text-zinc-500 hover:underline">
        ← Home
      </Link>
      <h1 className="mt-6 text-3xl font-bold">Admin console</h1>
      <p className="mt-2 text-sm text-zinc-600 dark:text-zinc-400">
        Operator-only views: scheduling debug, fleet health, abuse review,
        feature flags. Gated by the <code>admin</code> role on the user
        identity.
      </p>

      <div className="mt-10 rounded-lg border border-amber-300 bg-amber-50 p-5 text-sm text-amber-900">
        Stub view — wired in a follow-up PR.
      </div>
    </main>
  );
}
