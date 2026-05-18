import Link from "next/link";

export default function AccountPage() {
  return (
    <main className="mx-auto max-w-md px-6 py-16">
      <Link href="/" className="text-sm text-zinc-500 hover:underline">
        ← Back
      </Link>
      <h1 className="mt-6 text-3xl font-bold">Sign in to iogrid</h1>
      <p className="mt-2 text-sm text-zinc-600 dark:text-zinc-400">
        One account for both providing and consuming compute. Sign in with
        Google or email — your role (provider / customer / both) is chosen
        after first login.
      </p>

      <div className="mt-8 space-y-3">
        <button
          type="button"
          className="w-full rounded-md border border-zinc-300 px-4 py-3 text-sm font-medium hover:bg-zinc-50"
        >
          Continue with Google
        </button>
        <button
          type="button"
          className="w-full rounded-md border border-zinc-300 px-4 py-3 text-sm font-medium hover:bg-zinc-50"
        >
          Continue with email
        </button>
      </div>

      <p className="mt-6 text-xs text-zinc-500">
        By continuing you agree to the iogrid Terms and Acceptable Use Policy.
      </p>
    </main>
  );
}
