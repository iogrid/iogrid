import { redirect } from "next/navigation";
import Link from "next/link";

import { auth } from "@/lib/auth";
import { OnboardingWizard } from "../wizard";

/**
 * /onboard/[token] — the main onboarding entry point.
 *
 * The pairing code in the URL is forwarded from the installer (which
 * minted it via `iogridd pair --request`). Flow:
 *
 *   1. If the user is NOT signed in, redirect to /account?callbackUrl=...
 *      with the same token preserved → they sign in via Google or
 *      magic-link → bounce back here.
 *   2. POST { token } to gateway-bff /api/v1/onboard/start to link the
 *      code to the authenticated user. (Handled inside the wizard's
 *      first render; we don't do it server-side because we want to
 *      keep the route handler RSC-only — the client-side wizard owns
 *      the lifecycle.)
 *   3. Show the 3-step sensible-defaults wizard (caps / categories /
 *      payout).
 *   4. On submit, POST /api/v1/onboard/complete; on success route to
 *      /provide with a confetti welcome.
 *
 * Token format = 6-char Crockford-base32 minus I/L/O/U. We validate
 * the URL param here and reject malformed codes upfront.
 */
const PAIRING_CODE_RE = /^[0-9A-HJ-NP-TV-Z]{6}$/;

export default async function OnboardWithTokenPage({
  params,
}: {
  params: Promise<{ token: string }>;
}) {
  const { token: raw } = await params;
  const token = raw.toUpperCase();

  if (!PAIRING_CODE_RE.test(token)) {
    return (
      <main className="mx-auto max-w-md px-6 py-16">
        <h1 className="text-3xl font-bold">Invalid pairing code</h1>
        <p className="mt-3 text-sm text-zinc-600 dark:text-zinc-400">
          The pairing code in this URL doesn&apos;t match the expected
          6-character format. The link may be stale or mistyped.
        </p>
        <Link
          href="/onboard"
          className="mt-6 inline-block rounded-md bg-zinc-900 px-4 py-2 text-sm font-medium text-white hover:bg-zinc-700"
        >
          Enter code manually
        </Link>
      </main>
    );
  }

  const session = await auth();
  if (!session?.user) {
    // Preserve the token across the sign-in round-trip — when the user
    // comes back from /account they'll land on this same URL.
    const callback = `/onboard/${token}`;
    redirect(`/account?callbackUrl=${encodeURIComponent(callback)}`);
  }

  return (
    <main className="mx-auto max-w-2xl px-6 py-12">
      <header className="mb-8">
        <Link href="/" className="text-sm text-zinc-500 hover:underline">
          ← Home
        </Link>
        <h1 className="mt-4 text-3xl font-bold tracking-tight">
          Welcome to iogrid
        </h1>
        <p className="mt-2 text-sm text-zinc-600 dark:text-zinc-400">
          Three quick choices and your machine will start earning. You can
          change any of these from your dashboard later.
        </p>
        <p className="mt-2 font-mono text-xs text-zinc-500">
          Device pairing code:{" "}
          <span className="rounded bg-zinc-100 px-2 py-1 tracking-[0.3em] dark:bg-zinc-800">
            {token}
          </span>
        </p>
      </header>

      <OnboardingWizard token={token} />
    </main>
  );
}
