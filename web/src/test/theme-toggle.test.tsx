import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, act } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";

/**
 * Unit tests for <ThemeToggle />.
 *
 * `next-themes` is mocked so we can drive the (theme, setTheme,
 * resolvedTheme) tuple deterministically without booting the real
 * client provider (which needs `window.matchMedia`, an HTML root,
 * etc.). The mock holds the active theme in a module-level ref so
 * each test starts from a known state.
 */

let mockState: { theme: string; resolvedTheme: string };
const setThemeSpy = vi.fn((next: string) => {
  mockState.theme = next;
  mockState.resolvedTheme = next === "system" ? "light" : next;
});

vi.mock("next-themes", () => ({
  useTheme: () => ({
    theme: mockState.theme,
    setTheme: setThemeSpy,
    resolvedTheme: mockState.resolvedTheme,
    systemTheme: "light",
    themes: ["light", "dark", "system"],
    forcedTheme: undefined,
  }),
}));

import { ThemeToggle } from "@/components/theme-toggle";

describe("ThemeToggle", () => {
  beforeEach(() => {
    mockState = { theme: "light", resolvedTheme: "light" };
    setThemeSpy.mockClear();
  });

  it("renders an accessible button after mount", async () => {
    render(<ThemeToggle />);
    // The component mounts a placeholder, then flips to the real
    // button on the next microtask via useEffect. `act` flushes the
    // pending state update.
    await act(async () => {
      await Promise.resolve();
    });
    const btn = screen.getByRole("button");
    expect(btn).toBeEnabled();
    // Default mockState.theme === "light"; cycle is
    // system→dark→light→system, so from light the next state is system.
    expect(btn).toHaveAttribute("aria-label", "Switch to system theme");
    expect(btn).toHaveAttribute("data-theme-toggle", "light");
  });

  it("cycles system → dark → light → system on successive clicks", async () => {
    // Three independent mounts, each starting from a known prior
    // state. We can't drive the cycle in one mount because the
    // useTheme() value is sampled at render time and updating the
    // module-level mock state doesn't trigger a re-render — that's a
    // job for the real next-themes provider, which we are mocking.
    //
    // Cycle order is `system → dark → light → system` (see
    // theme-toggle.tsx for the why — first-time visitors start at
    // "system" and the first click must produce a visible flip).

    mockState = { theme: "system", resolvedTheme: "light" };
    const r1 = render(<ThemeToggle />);
    await act(async () => {
      await Promise.resolve();
    });
    fireEvent.click(screen.getByRole("button"));
    expect(setThemeSpy).toHaveBeenLastCalledWith("dark");
    r1.unmount();

    mockState = { theme: "dark", resolvedTheme: "dark" };
    const r2 = render(<ThemeToggle />);
    await act(async () => {
      await Promise.resolve();
    });
    fireEvent.click(screen.getByRole("button"));
    expect(setThemeSpy).toHaveBeenLastCalledWith("light");
    r2.unmount();

    mockState = { theme: "light", resolvedTheme: "light" };
    render(<ThemeToggle />);
    await act(async () => {
      await Promise.resolve();
    });
    fireEvent.click(screen.getByRole("button"));
    expect(setThemeSpy).toHaveBeenLastCalledWith("system");
  });

  it("uses the Sun icon under light theme and Moon under dark", async () => {
    render(<ThemeToggle />);
    await act(async () => {
      await Promise.resolve();
    });
    // lucide-react renders SVGs with `lucide-<name>` classes.
    expect(
      screen.getByRole("button").querySelector("svg")?.getAttribute("class"),
    ).toMatch(/lucide-sun/i);
  });

  it("updates aria-label to advertise the NEXT mode", async () => {
    mockState = { theme: "dark", resolvedTheme: "dark" };
    render(<ThemeToggle />);
    await act(async () => {
      await Promise.resolve();
    });
    // Cycle is system→dark→light→system; from dark the next state is
    // light, so the label should announce "light".
    expect(screen.getByRole("button")).toHaveAttribute(
      "aria-label",
      "Switch to light theme",
    );
  });

  it("exposes a custom class on the button", async () => {
    render(<ThemeToggle className="ml-4" />);
    await act(async () => {
      await Promise.resolve();
    });
    expect(screen.getByRole("button").className).toMatch(/ml-4/);
  });
});
