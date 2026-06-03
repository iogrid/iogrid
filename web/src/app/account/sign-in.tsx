import Link from "next/link";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

/**
 * SignInPanel — the unauthenticated /account view. Server Component;
 * the two sign-in handlers are passed as Server Actions from the
 * parent so the secrets stay server-side.
 */
export function SignInPanel({
  signInWithGoogle,
  signInWithEmail,
  callbackUrl,
  googleEnabled = true,
}: {
  signInWithGoogle: () => Promise<void>;
  signInWithEmail: (data: FormData) => Promise<void>;
  callbackUrl?: string;
  /**
   * Whether Google OAuth is actually configured (real client id, not the
   * phase0 placeholder). Decided server-side; when false we hide the
   * "Continue with Google" button so users only see the working
   * magic-link path instead of a Google `invalid_client` error page.
   */
  googleEnabled?: boolean;
}) {
  return (
    <main className="mx-auto max-w-md px-6 py-16">
      <Link href="/" className="text-sm text-muted-foreground hover:underline">
        ← Back
      </Link>
      <h1 className="mt-6 text-3xl font-bold">Sign in to iogrid</h1>
      <p className="mt-2 text-sm text-muted-foreground dark:text-muted-foreground">
        One account for both providing and consuming compute. Sign in with
        {googleEnabled ? " Google or email" : " email"} — your role (provider /
        customer / both) is chosen after first login.
      </p>

      <div className="mt-8 space-y-3">
        {googleEnabled ? (
          <form action={signInWithGoogle}>
            <Button type="submit" variant="outline" className="w-full">
              Continue with Google
            </Button>
          </form>
        ) : null}
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
        <p className="mt-4 text-xs text-muted-foreground">
          You&apos;ll return to <code className="font-mono">{callbackUrl}</code>{" "}
          after sign-in.
        </p>
      ) : null}

      <p className="mt-6 text-xs text-muted-foreground">
        By continuing you agree to the iogrid Terms and Acceptable Use Policy.
      </p>
    </main>
  );
}
