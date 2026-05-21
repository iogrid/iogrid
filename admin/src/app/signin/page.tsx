import Link from "next/link";
import { auth, signIn } from "@/lib/auth";
import { redirect } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

export const metadata = {
  title: "Sign in — iogrid admin",
  robots: { index: false, follow: false },
};

/**
 * /signin — admin sign-in landing.
 *
 * The middleware sends every unauthenticated visitor here. Once the
 * user signs in (Google OAuth or magic-link via nodemailer) the
 * NextAuth callback returns them to `callbackUrl`. The middleware
 * then re-checks IOGRID_ADMIN_EMAILS — if the email isn't on the
 * allowlist the user gets a 403 JSON response, NOT a silent redirect.
 */
export default async function AdminSignInPage({
  searchParams,
}: {
  searchParams: Promise<{ callbackUrl?: string }>;
}) {
  const session = await auth();
  const { callbackUrl } = await searchParams;

  if (session?.user) {
    // Already signed in — bounce to the original destination (or root).
    redirect(callbackUrl && callbackUrl.startsWith("/") ? callbackUrl : "/");
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
      <Link href="/" className="text-sm text-zinc-500 hover:underline">
        ← Back
      </Link>
      <h1 className="mt-6 text-3xl font-bold">Sign in to iogrid admin</h1>
      <p className="mt-2 text-sm text-zinc-600 dark:text-zinc-400">
        Staff console. Access requires an entry in the operator-managed{" "}
        <code className="font-mono">IOGRID_ADMIN_EMAILS</code> allowlist —
        signing in with a non-listed address returns a 403.
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
            placeholder="you@example.com"
            required
            aria-label="Email"
          />
          <Button type="submit" className="w-full">
            Send magic link
          </Button>
        </form>
      </div>

      {callbackUrl ? (
        <p className="mt-4 text-xs text-zinc-500">
          You&apos;ll return to <code className="font-mono">{callbackUrl}</code>{" "}
          after sign-in.
        </p>
      ) : null}
    </main>
  );
}
