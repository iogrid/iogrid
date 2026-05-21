import Link from "next/link";
import { ThemeToggle } from "@/components/theme-toggle";

/**
 * /onboard (no token) — landing page when a user navigates here without
 * a pairing code in the URL. Shows the manual code-entry form for the
 * case where the install script couldn't auto-open the browser (e.g.
 * headless Linux servers).
 */
export default function OnboardLandingPage() {
  return (
    <main className="mx-auto max-w-md px-6 py-16">
      <div className="flex items-center justify-between">
        <Link href="/" className="text-sm text-muted-foreground hover:underline">
          ← Back
        </Link>
        <ThemeToggle />
      </div>
      <h1 className="mt-6 text-3xl font-bold">Finish iogrid setup</h1>
      <p className="mt-2 text-sm text-muted-foreground dark:text-muted-foreground">
        Enter the 6-character code shown by your installer to link this
        machine to your iogrid account.
      </p>

      <form
        action="/onboard/redirect"
        method="get"
        className="mt-8 space-y-3"
        aria-label="Pairing code form"
      >
        <label htmlFor="token" className="sr-only">
          Pairing code
        </label>
        <input
          id="token"
          name="token"
          type="text"
          placeholder="ABC123"
          inputMode="text"
          autoComplete="off"
          autoCapitalize="characters"
          spellCheck={false}
          maxLength={6}
          pattern="[0-9A-HJ-NP-TV-Z]{6}"
          required
          className="w-full rounded-md border border-border-strong px-4 py-3 text-center font-mono text-2xl uppercase tracking-[0.4em] focus:border-foreground focus:outline-none focus:ring-1 focus:ring-ring"
        />
        <button
          type="submit"
          className="w-full rounded-md bg-foreground px-4 py-3 text-sm font-medium text-background hover:bg-foreground/80"
        >
          Continue
        </button>
      </form>

      <details className="mt-8 text-xs text-muted-foreground">
        <summary className="cursor-pointer">
          What&apos;s a pairing code?
        </summary>
        <p className="mt-2 leading-relaxed">
          When you ran the iogrid installer, the daemon minted a one-time
          6-character code that proves you control this machine. Enter the
          code above and we&apos;ll link this device to your account. The code
          expires in 10 minutes — if you missed it, run{" "}
          <code className="rounded bg-muted px-1 py-0.5 font-mono">
            iogridd pair --request
          </code>{" "}
          on your machine to mint a fresh one.
        </p>
      </details>
    </main>
  );
}
