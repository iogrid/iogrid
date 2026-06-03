/**
 * Server-only helper to decide whether Google OAuth sign-in is actually
 * usable in the current environment.
 *
 * In prod the `GOOGLE_CLIENT_ID` secret is seeded with a
 * `phase0-placeholder` value until an operator provisions a real OAuth
 * client in the Google Cloud Console (a genuinely Console-UI-only step).
 * With the placeholder in place, NextAuth still *lists* the Google
 * provider, but redirecting a user to Google returns an `invalid_client`
 * error page — a worse experience than not offering the button at all.
 *
 * `googleSignInEnabled()` returns `false` when the client id is unset or
 * still the placeholder, so the sign-in panel can hide the "Continue
 * with Google" button and show only the working magic-link path.
 *
 * NOTE: this reads `process.env.GOOGLE_CLIENT_ID`, which is never exposed
 * to the browser. It MUST only be called from server components / server
 * actions / route handlers.
 */
export function googleSignInEnabled(
  clientId: string | undefined = process.env.GOOGLE_CLIENT_ID,
): boolean {
  if (!clientId) return false;
  const trimmed = clientId.trim();
  if (trimmed.length === 0) return false;
  // Reject the phase0 placeholder seed in any casing / surrounding form.
  if (trimmed.toLowerCase().includes("phase0-placeholder")) return false;
  if (trimmed.toLowerCase() === "placeholder") return false;
  return true;
}
