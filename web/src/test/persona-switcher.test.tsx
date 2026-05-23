import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";

import { PersonaSwitcher } from "@/components/layout/persona-switcher";

// next/navigation mock — PersonaSwitcher uses Link only, not useRouter,
// but vitest's jsdom needs the next/link router context.
vi.mock("next/navigation", () => ({
  usePathname: () => "/provider",
}));

describe("PersonaSwitcher (#470 header dropdown)", () => {
  beforeEach(() => {
    document.body.innerHTML = "";
  });

  it("renders the current persona label + chevron in the trigger button", () => {
    render(<PersonaSwitcher active="provider" />);
    const trigger = screen.getByTestId("persona-switcher-trigger");
    expect(trigger).toHaveTextContent("Provider");
    expect(trigger).toHaveAttribute("aria-expanded", "false");
    expect(trigger).toHaveAttribute("aria-haspopup", "menu");
  });

  it("opens the menu on click + lists all 4 personas + sign out", () => {
    render(<PersonaSwitcher active="provider" />);
    fireEvent.click(screen.getByTestId("persona-switcher-trigger"));

    expect(screen.getByTestId("persona-switch-provider")).toBeInTheDocument();
    expect(screen.getByTestId("persona-switch-customer")).toBeInTheDocument();
    expect(screen.getByTestId("persona-switch-vpn")).toBeInTheDocument();
    expect(screen.getByTestId("persona-switch-account")).toBeInTheDocument();
    expect(screen.getByTestId("persona-switcher-signout")).toBeInTheDocument();

    expect(
      screen.getByTestId("persona-switcher-trigger"),
    ).toHaveAttribute("aria-expanded", "true");
  });

  it("marks the active persona row with the indicator + accent text", () => {
    render(<PersonaSwitcher active="customer" />);
    fireEvent.click(screen.getByTestId("persona-switcher-trigger"));

    const customer = screen.getByTestId("persona-switch-customer");
    // active row has the bg-primary-50 class applied
    expect(customer.className).toMatch(/bg-primary-50/);
    // inactive row does not
    const provider = screen.getByTestId("persona-switch-provider");
    expect(provider.className).not.toMatch(/bg-primary-50/);
  });

  it("each persona row links to /<persona>", () => {
    render(<PersonaSwitcher active="provider" />);
    fireEvent.click(screen.getByTestId("persona-switcher-trigger"));

    expect(screen.getByTestId("persona-switch-provider")).toHaveAttribute(
      "href",
      "/provider",
    );
    expect(screen.getByTestId("persona-switch-customer")).toHaveAttribute(
      "href",
      "/customer",
    );
    expect(screen.getByTestId("persona-switch-vpn")).toHaveAttribute(
      "href",
      "/vpn",
    );
    expect(screen.getByTestId("persona-switch-account")).toHaveAttribute(
      "href",
      "/account",
    );
    expect(screen.getByTestId("persona-switcher-signout")).toHaveAttribute(
      "href",
      "/api/auth/signout",
    );
  });

  it("Escape key closes the menu", () => {
    render(<PersonaSwitcher active="provider" />);
    fireEvent.click(screen.getByTestId("persona-switcher-trigger"));
    expect(
      screen.getByTestId("persona-switcher-trigger"),
    ).toHaveAttribute("aria-expanded", "true");

    fireEvent.keyDown(document, { key: "Escape" });
    // The menu unmounts; trigger aria-expanded flips back to false on
    // next render. Use waitFor since the listener fires asynchronously.
    return waitFor(() => {
      expect(
        screen.getByTestId("persona-switcher-trigger"),
      ).toHaveAttribute("aria-expanded", "false");
    });
  });

  it("click on a persona link closes the menu", () => {
    render(<PersonaSwitcher active="provider" />);
    fireEvent.click(screen.getByTestId("persona-switcher-trigger"));
    fireEvent.click(screen.getByTestId("persona-switch-vpn"));

    return waitFor(() => {
      expect(
        screen.getByTestId("persona-switcher-trigger"),
      ).toHaveAttribute("aria-expanded", "false");
    });
  });

  it("renders each persona's blurb", () => {
    render(<PersonaSwitcher active="provider" />);
    fireEvent.click(screen.getByTestId("persona-switcher-trigger"));
    expect(screen.getByText(/Share idle hardware/i)).toBeInTheDocument();
    expect(screen.getByText(/Buy compute, proxy/i)).toBeInTheDocument();
    expect(screen.getByText(/Unmetered private routing/i)).toBeInTheDocument();
    expect(screen.getByText(/Profile, billing/i)).toBeInTheDocument();
  });
});
