import Link from "next/link";
import { auth, signIn } from "@/lib/auth";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

export const metadata = { title: "Sign in — iogrid admin" };

/**
 * /account — sign-in surface for the admin app (EPIC #422 Phase 1).
 *
 * Admin sessions are scoped to admin.iogrid.org via the cookie-domain
 * pin in `src/auth.config.ts`. Signing in here mints a cookie that
 * the browser will NOT send to iogrid.org or iogrid.org — that's
 * the strict-separation invariant the founder asked for.
 *
 * If an authenticated admin somehow lands here, render a tiny "you're
 * signed in" affordance with a continue link. If a non-admin
 * authenticated session lands here, the middleware has already
 * short-circuited with a 403 — they don't reach this route.
 *
 * No PortalShell / AdminShell wrapper: the sign-in surface is
 * deliberately bare so an unauthenticated user doesn't see any
 * staff-nav hints before they sign in.
 */
export default async function AdminAccountPage({
  searchParams,
}: {
  searchParams: Promise<{ callbackUrl?: string }>;
}) {
  const session = await auth();
  const { callbackUrl } = await searchParams;

  if (session?.user) {
    return (
      <main className="mx-auto max-w-md px-6 py-16">
        <h1 className="text-2xl font-bold">You&apos;re signed in</h1>
        <p className="mt-2 text-sm text-foreground dark:text-muted-foreground">
          Signed in as <code className="font-mono">{session.user.email}</code>.
        </p>
        <p className="mt-4">
          <Link
            href={callbackUrl ?? "/"}
            className="text-sm font-medium underline"
          >
            Continue to admin console →
          </Link>
        </p>
      </main>
    );
  }

  async function signInWithGoogle() {
    "use server";
    await signIn("google", { redirectTo: callbackUrl ?? "/" });
  }
  async function signInWithEmail(formData: FormData) {
    "use server";
    const email = String(formData.get("email") ?? "");
    if (!email) return;
    await signIn("nodemailer", {
      email,
      redirectTo: callbackUrl ?? "/",
    });
  }

  return (
    <main className="mx-auto max-w-md px-6 py-16">
      <Link href="/" className="text-sm text-muted-foreground hover:underline">
        ← iogrid admin
      </Link>
      <h1 className="mt-6 text-3xl font-bold">Sign in to iogrid admin</h1>
      <p className="mt-2 text-sm text-foreground dark:text-muted-foreground">
        Staff console — provider pool, abuse review, billing audit, system
        health. Your email must be on the <code>IOGRID_ADMIN_EMAILS</code>{" "}
        allowlist.
      </p>

      <div className="mt-8 space-y-3">
        <form action={signInWithGoogle}>
          <Button type="submit" variant="outline" className="w-full">
            Continue with Google
          </Button>
        </form>
        <form action={signInWithEmail} className="space-y-2">
          <Input
            type="email"
            name="email"
            placeholder="you@openova.io"
            required
            aria-label="Email"
          />
          <Button type="submit" className="w-full">
            Send magic link
          </Button>
        </form>
      </div>

      {callbackUrl ? (
        <p className="mt-4 text-xs text-muted-foreground">
          You&apos;ll return to <code className="font-mono">{callbackUrl}</code>{" "}
          after sign-in.
        </p>
      ) : null}

      <p className="mt-6 text-xs text-muted-foreground">
        Looking for the user-facing app?{" "}
        <a
          href="https://iogrid.org"
          className="underline hover:text-foreground dark:hover:text-border"
        >
          iogrid.org
        </a>
        .
      </p>
    </main>
  );
}
