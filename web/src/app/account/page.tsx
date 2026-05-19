import { auth, signIn } from "@/lib/auth";
import { PortalShell } from "@/components/layout/portal-shell";
import { ACCOUNT_NAV } from "@/app/account/nav";
import { ProfileCard } from "./profile-card";
import { SignInPanel } from "./sign-in";

export const metadata = { title: "Account — iogrid" };

/**
 * /account — gateway for the identity surface.
 *
 *   - Unauthenticated: render the sign-in panel (Google OAuth + magic link).
 *   - Authenticated: render the profile card + section nav.
 *
 * NextAuth v5's `auth()` works inside a Server Component, so the
 * branching happens on the server and we never ship the sign-in form
 * to authenticated users.
 */
export default async function AccountPage({
  searchParams,
}: {
  searchParams: Promise<{ callbackUrl?: string }>;
}) {
  const session = await auth();
  const { callbackUrl } = await searchParams;

  if (!session?.user) {
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
      <SignInPanel
        signInWithGoogle={signInWithGoogle}
        signInWithEmail={signInWithEmail}
        callbackUrl={callbackUrl}
      />
    );
  }

  return (
    <PortalShell
      badge="Account"
      title="Profile"
      subtitle="The identity used for both the provider and customer sides of iogrid."
      nav={ACCOUNT_NAV}
      activeHref="/account"
    >
      <ProfileCard
        name={session.user.name ?? ""}
        email={session.user.email ?? ""}
        image={session.user.image ?? null}
      />
    </PortalShell>
  );
}
