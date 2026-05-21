"use client";

import * as React from "react";
import { ThemeProvider as NextThemesProvider } from "next-themes";

/**
 * Thin wrapper around `next-themes`'s ThemeProvider so the root
 * layout doesn't carry the configuration noise and every test /
 * Storybook mount can re-use the same shape.
 *
 * Strategy:
 *   - `attribute="class"` — next-themes adds/removes `.dark` on
 *     `<html>` which lines up with the `@custom-variant dark` rule in
 *     `globals.css`.
 *   - `defaultTheme="system"` + `enableSystem` — first paint follows
 *     `prefers-color-scheme`, so we don't override the OS-level
 *     choice until the user picks an explicit value via the toggle.
 *   - `storageKey="iogrid-theme"` — namespaced localStorage key so a
 *     sibling app on the same origin (mothership console, future
 *     dashboards) can't collide.
 *   - `disableTransitionOnChange={false}` — we DO want the brief
 *     150ms cross-fade defined in `globals.css` (.theme-transitioning)
 *     to smooth the swap; we only suppress transitions for the very
 *     first paint via next-themes' inline blocking script.
 *
 * next-themes 0.4 ships `ThemeProviderProps extends React.PropsWithChildren`
 * without a type arg, so PropsWithChildren resolves to
 * `unknown & { children?: ReactNode }` which TS 5.6 strict-mode
 * collapses to `unknown` — dropping the `children` member from the
 * interface. We work around by casting the upstream component to a
 * locally-typed shape that explicitly re-states `children`. Pure
 * type-layer fix; runtime contract is unchanged.
 */
type ProviderProps = React.PropsWithChildren<{
  attribute?: "class" | `data-${string}` | Array<"class" | `data-${string}`>;
  defaultTheme?: string;
  enableSystem?: boolean;
  storageKey?: string;
  disableTransitionOnChange?: boolean;
  forcedTheme?: string;
  themes?: string[];
}>;

const TypedProvider = NextThemesProvider as unknown as React.FC<ProviderProps>;

export function ThemeProvider({ children, ...rest }: ProviderProps) {
  return (
    <TypedProvider
      attribute="class"
      defaultTheme="system"
      enableSystem
      storageKey="iogrid-theme"
      disableTransitionOnChange={false}
      {...rest}
    >
      {children}
    </TypedProvider>
  );
}
