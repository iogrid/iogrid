import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";

import { PrimaryProviderPicker } from "@/components/dashboard/primary-provider-picker";
import type { ProviderRef } from "@/lib/types";

// Issue #325 — multi-daemon ownership UI. These tests pin the
// load-bearing surface contracts:
//   1. zero-providers   → component renders nothing (parent owns the
//                          empty-state path).
//   2. single provider  → read-only inline pill, no dropdown chrome.
//   3. multi providers  → dropdown + per-row "Set as primary" buttons,
//                          primary row shows a badge instead of the
//                          promote button.
// The schedule editor is the only call site; if it regresses these
// shapes, the picker is the wrong surface (re-introduces the founder
// wrong-daemon-shown bug).

const primary: ProviderRef = {
  id: { value: "11111111-1111-1111-1111-111111111111" },
  display_name: "Hatice's Mac",
  is_primary: true,
};
const secondary: ProviderRef = {
  id: { value: "22222222-2222-2222-2222-222222222222" },
  display_name: "manual-test",
  is_primary: false,
};

describe("PrimaryProviderPicker", () => {
  it("renders nothing when zero providers are supplied", () => {
    const { container } = render(
      <PrimaryProviderPicker
        providers={[]}
        selectedId={null}
        onSelect={() => {}}
        onPromote={() => {}}
      />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("renders the read-only inline pill (no dropdown) for a single-daemon owner", () => {
    render(
      <PrimaryProviderPicker
        providers={[primary]}
        selectedId={primary.id?.value ?? null}
        onSelect={() => {}}
        onPromote={() => {}}
      />,
    );
    expect(screen.getByTestId("primary-provider-pill")).toBeInTheDocument();
    expect(screen.getByText("Hatice's Mac")).toBeInTheDocument();
    expect(screen.queryByTestId("primary-provider-select")).toBeNull();
    expect(
      screen.queryByTestId("primary-provider-promote"),
    ).toBeNull();
  });

  it("renders the dropdown + per-row controls when ≥2 daemons", () => {
    render(
      <PrimaryProviderPicker
        providers={[primary, secondary]}
        selectedId={primary.id?.value ?? null}
        onSelect={() => {}}
        onPromote={() => {}}
      />,
    );
    expect(
      screen.getByTestId("primary-provider-picker"),
    ).toBeInTheDocument();
    const select = screen.getByTestId("primary-provider-select");
    expect(select).toBeInTheDocument();
    expect(select).toHaveValue("11111111-1111-1111-1111-111111111111");

    const rows = screen.getAllByTestId("primary-provider-row");
    expect(rows).toHaveLength(2);
    // Primary row shows the badge + NO promote button.
    expect(screen.getByTestId("primary-provider-badge")).toBeInTheDocument();
    // Non-primary row exposes a "Set as primary" button.
    expect(screen.getByTestId("primary-provider-promote")).toBeInTheDocument();
  });

  it("invokes onSelect with the chosen provider id on dropdown change", () => {
    const onSelect = vi.fn();
    render(
      <PrimaryProviderPicker
        providers={[primary, secondary]}
        selectedId={primary.id?.value ?? null}
        onSelect={onSelect}
        onPromote={() => {}}
      />,
    );
    fireEvent.change(screen.getByTestId("primary-provider-select"), {
      target: { value: secondary.id?.value },
    });
    expect(onSelect).toHaveBeenCalledWith(secondary.id?.value);
  });

  it("invokes onPromote when the operator clicks 'Set as primary'", () => {
    const onPromote = vi.fn();
    render(
      <PrimaryProviderPicker
        providers={[primary, secondary]}
        selectedId={primary.id?.value ?? null}
        onSelect={() => {}}
        onPromote={onPromote}
      />,
    );
    fireEvent.click(screen.getByTestId("primary-provider-promote"));
    expect(onPromote).toHaveBeenCalledWith(secondary.id?.value);
  });

  it("disables every promote button while a swap is in flight", () => {
    render(
      <PrimaryProviderPicker
        providers={[primary, secondary]}
        selectedId={primary.id?.value ?? null}
        onSelect={() => {}}
        onPromote={() => {}}
        promoting={true}
      />,
    );
    expect(screen.getByTestId("primary-provider-promote")).toBeDisabled();
  });

  it("falls back to a daemon-id label when display_name is empty", () => {
    const anon: ProviderRef = {
      id: { value: "abcdef12-3456-7890-abcd-ef1234567890" },
      display_name: "",
      is_primary: true,
    };
    render(
      <PrimaryProviderPicker
        providers={[anon]}
        selectedId={anon.id?.value ?? null}
        onSelect={() => {}}
        onPromote={() => {}}
      />,
    );
    // Slice(0,8) → "abcdef12".
    expect(screen.getByText(/daemon abcdef12/)).toBeInTheDocument();
  });
});
