/**
 * #318 — /provide overview must surface the paired-machine identity
 * (display_name + status + last_seen + registered) above the KPI
 * strip. Without this card, hatice.yildiz signs in and sees Recent
 * Activity + scheduler tiles but never her actual paired Mac.
 *
 * These tests pin the card's contract on the same JSON shape the
 * gateway-bff actually emits (#298/#304/#318: snake_case keys,
 * numeric ProviderStatus enum, {seconds, nanos} timestamps).
 */
import { describe, expect, it } from "vitest";
import { render, screen, within } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import { PairedMachinesCard } from "@/components/dashboard/paired-machines-card";
import type { ProviderRef } from "@/lib/types";

// "now" pinned so relative-time output is deterministic.
const NOW_MS = 1_779_222_754_000; // 2026-05-20T... (after the seconds in the fixture)

// Mirrors the BFF envelope from issue #318's evidence block:
//   "providers":[{"id":{"value":"808ce330-..."},
//                 "display_name":"Hatice's Mac","status":1,
//                 "host_info":{}, "network_info":{"inferred_region":{}},
//                 "capabilities":{}, "registered_at":{...},
//                 "last_seen_at":{...}}]
const hatice: ProviderRef = {
  id: { value: "808ce330-79c1-4390-8cc6-87c5ce5a94d8" },
  owner_user_id: { value: "a7a93576-aebb-453e-bfc5-f9c31514e9da" },
  display_name: "Hatice's Mac",
  status: 1,
  host_info: {},
  network_info: { inferred_region: {} },
  capabilities: {},
  registered_at: { seconds: 1779222554, nanos: 170369000 },
  last_seen_at: { seconds: 1779222554, nanos: 170370000 },
};

describe("PairedMachinesCard", () => {
  it("renders the hatice fixture from #318 evidence", () => {
    render(<PairedMachinesCard providers={[hatice]} nowMs={NOW_MS} />);

    // Section heading present.
    expect(screen.getByText("Paired machines")).toBeInTheDocument();
    expect(screen.getByText("1 daemon")).toBeInTheDocument();

    // Display name visible (founder DoD: "hatice signs in must see paired Mac").
    expect(screen.getByText("Hatice's Mac")).toBeInTheDocument();

    // Status badge decodes numeric 1 → "Active".
    const badge = screen.getByTestId("paired-machine-status");
    expect(badge).toHaveTextContent(/Active/i);
    expect(badge).toHaveAttribute("data-status", "active");

    // ID is truncated middle-style (first 8 + last 4).
    const id = screen.getByTestId("paired-machine-id");
    expect(id.textContent).toMatch(/^808ce330/);
    expect(id.textContent).toMatch(/94d8$/);
    expect(id.textContent).toContain("…");
    // Hover-title carries the full UUID for copy.
    expect(id).toHaveAttribute("title", hatice.id!.value);

    // Last seen rendered as relative time (3m20s before NOW_MS → "3m ago").
    const lastSeen = screen.getByTestId("paired-machine-last-seen");
    expect(lastSeen.textContent).toMatch(/ago|just now/);
    expect(lastSeen.textContent).not.toBe("never");

    // Registered rendered as an absolute date (year present).
    const registered = screen.getByTestId("paired-machine-registered");
    expect(registered.textContent).toMatch(/2026/);

    // host_info is empty in this fixture — no platform/architecture row.
    expect(screen.queryByTestId("paired-machine-platform")).toBeNull();
  });

  it("renders multiple paired machines as separate rows", () => {
    const second: ProviderRef = {
      ...hatice,
      id: { value: "11111111-2222-3333-4444-555555555555" },
      display_name: "Hatice's Mac Mini",
      status: 2, // OFFLINE
      last_seen_at: { seconds: 1779222554 - 3600, nanos: 0 }, // 1h ago
    };
    render(
      <PairedMachinesCard providers={[hatice, second]} nowMs={NOW_MS} />,
    );

    expect(screen.getByText("2 daemons")).toBeInTheDocument();
    expect(screen.getByText("Hatice's Mac")).toBeInTheDocument();
    expect(screen.getByText("Hatice's Mac Mini")).toBeInTheDocument();

    const statuses = screen.getAllByTestId("paired-machine-status");
    expect(statuses).toHaveLength(2);
    expect(statuses[0]).toHaveAttribute("data-status", "active");
    expect(statuses[1]).toHaveAttribute("data-status", "offline");
    expect(statuses[1]).toHaveTextContent(/Offline/i);
  });

  it("returns null when providers[] is empty (avoid double empty-state)", () => {
    // Empty array → component renders nothing so the #313 install CTA
    // (rendered by a sibling) owns the empty-state surface entirely.
    const { container } = render(<PairedMachinesCard providers={[]} />);
    expect(container.firstChild).toBeNull();
    expect(screen.queryByText("Paired machines")).toBeNull();
  });

  it("returns null when providers prop is undefined", () => {
    const { container } = render(
      <PairedMachinesCard providers={undefined} />,
    );
    expect(container.firstChild).toBeNull();
  });

  it("returns null when providers prop is null (BFF returns providers:null)", () => {
    const { container } = render(<PairedMachinesCard providers={null} />);
    expect(container.firstChild).toBeNull();
  });

  it("falls back to 'Unnamed daemon' when display_name is empty", () => {
    const unnamed: ProviderRef = { ...hatice, display_name: "" };
    render(<PairedMachinesCard providers={[unnamed]} nowMs={NOW_MS} />);
    expect(screen.getByText("Unnamed daemon")).toBeInTheDocument();
  });

  it("renders 'never' for last_seen_at when seconds is 0", () => {
    const fresh: ProviderRef = {
      ...hatice,
      last_seen_at: { seconds: 0, nanos: 0 },
    };
    render(<PairedMachinesCard providers={[fresh]} nowMs={NOW_MS} />);
    const lastSeen = screen.getByTestId("paired-machine-last-seen");
    expect(lastSeen).toHaveTextContent("never");
  });

  it("decodes numeric Platform/Architecture enums in host_info when populated", () => {
    // Once the daemon wires HostInfo (separate work), the card must
    // surface "macOS · arm64" — proto-numeric values 1 / 2.
    const withHost: ProviderRef = {
      ...hatice,
      host_info: { platform: 1, architecture: 2 },
    };
    render(<PairedMachinesCard providers={[withHost]} nowMs={NOW_MS} />);
    const platform = screen.getByTestId("paired-machine-platform");
    expect(platform).toHaveTextContent("macOS");
    expect(platform).toHaveTextContent("arm64");
  });

  it("omits the host_info row entirely when both fields are empty (no 'Unknown')", () => {
    // §3.3 — defensive 'Unknown' placeholders are a smell. Daemon
    // hasn't populated host_info yet (#318), so the row simply
    // disappears.
    render(<PairedMachinesCard providers={[hatice]} nowMs={NOW_MS} />);
    expect(screen.queryByTestId("paired-machine-platform")).toBeNull();
  });

  it("renders status='Pairing' for UNSPECIFIED (status=0) instead of hiding the row", () => {
    const pairing: ProviderRef = { ...hatice, status: 0 };
    render(<PairedMachinesCard providers={[pairing]} nowMs={NOW_MS} />);
    const badge = screen.getByTestId("paired-machine-status");
    expect(badge).toHaveTextContent(/Pairing/i);
    expect(badge).toHaveAttribute("data-status", "neutral");
    // Row still renders the daemon's name even when status is unknown.
    expect(screen.getByText("Hatice's Mac")).toBeInTheDocument();
  });

  it("accepts string status enum for a future protojson cutover", () => {
    const cutover: ProviderRef = {
      ...hatice,
      status: "PROVIDER_STATUS_ACTIVE",
    };
    render(<PairedMachinesCard providers={[cutover]} nowMs={NOW_MS} />);
    const badge = screen.getByTestId("paired-machine-status");
    expect(badge).toHaveAttribute("data-status", "active");
  });

  it("preserves row order from the providers array", () => {
    const second: ProviderRef = {
      ...hatice,
      id: { value: "11111111-2222-3333-4444-555555555555" },
      display_name: "Hatice's Mac Mini",
    };
    render(
      <PairedMachinesCard providers={[hatice, second]} nowMs={NOW_MS} />,
    );
    const list = screen.getByTestId("paired-machines-card").querySelector("ul");
    expect(list).not.toBeNull();
    const items = within(list!).getAllByRole("listitem");
    expect(items[0]).toHaveTextContent("Hatice's Mac");
    expect(items[1]).toHaveTextContent("Hatice's Mac Mini");
  });
});
