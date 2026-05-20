"use client";

import * as React from "react";
import { Moon, Sun, Monitor } from "lucide-react";
import { useTheme } from "next-themes";
import { cn } from "@/lib/utils";

/**
 * Three-state theme cycle: light → dark → system → light.
 *
 * Why three states (not the usual two): operators on Linux + macOS
 * who already wire `prefers-color-scheme` at the OS level expect
 * "follow system" as a first-class choice. Stripping it forces a
 * permanent override and breaks the OS-wide dark-at-night flows.
 *
 * Implementation notes:
 *   - `mounted` flag — next-themes resolves `theme` on the client
 *     only; rendering the icon during SSR would produce a hydration
 *     mismatch (server has no localStorage). We render a static
 *     placeholder until `mounted === true`.
 *   - The toggle reads `theme` (the user's chosen mode, including
 *     "system") rather than `resolvedTheme` so the cycle is
 *     predictable across system-pref boundaries.
 *   - `aria-label` updates as the cycle advances so screen-reader
 *     users can hear which mode they are switching INTO. The icon
 *     reflects the current effective theme so sighted users see the
 *     "now" state, not the "next" state — matches GitHub / Vercel
 *     conventions.
 */
export function ThemeToggle({ className }: { className?: string }) {
  const { theme, setTheme, resolvedTheme } = useTheme();
  const [mounted, setMounted] = React.useState(false);

  React.useEffect(() => {
    setMounted(true);
  }, []);

  const cycle = React.useCallback(() => {
    // light → dark → system → light
    const next =
      theme === "light" ? "dark" : theme === "dark" ? "system" : "light";
    setTheme(next);
  }, [theme, setTheme]);

  const baseClasses = cn(
    "inline-flex h-9 w-9 items-center justify-center rounded-md border border-border bg-background text-foreground transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background",
    className,
  );

  // SSR / first-render placeholder. Keeping the same size + border so
  // there is no layout shift once the client mounts.
  if (!mounted) {
    return (
      <button
        type="button"
        className={baseClasses}
        aria-label="Toggle theme"
        data-theme-toggle="pending"
        // Disabled while we don't yet know the user's choice — avoids
        // a flicker if a click lands in the first paint frame.
        disabled
      >
        <Sun className="h-4 w-4" aria-hidden="true" />
      </button>
    );
  }

  // Icon: current effective state. Label: the mode we'll switch INTO.
  const effective = resolvedTheme === "dark" ? "dark" : "light";
  const nextMode =
    theme === "light" ? "dark" : theme === "dark" ? "system" : "light";

  const icon =
    theme === "system" ? (
      <Monitor className="h-4 w-4" aria-hidden="true" />
    ) : effective === "dark" ? (
      <Moon className="h-4 w-4" aria-hidden="true" />
    ) : (
      <Sun className="h-4 w-4" aria-hidden="true" />
    );

  return (
    <button
      type="button"
      onClick={cycle}
      className={baseClasses}
      aria-label={`Switch to ${nextMode} theme`}
      title={`Theme: ${theme} (click for ${nextMode})`}
      data-theme-toggle={theme}
    >
      {icon}
      <span className="sr-only">Current theme: {theme}</span>
    </button>
  );
}
