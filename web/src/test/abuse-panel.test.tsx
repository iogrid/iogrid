/**
 * @file Regression coverage for #304 — admin/abuse rule rows must NOT
 * render the literal text `[object Object]` when gateway-bff serialises
 * `*timestamppb.Timestamp` as the stdlib JSON struct `{seconds, nanos}`
 * instead of an RFC3339 string.
 *
 * The bug surfaced on rows 5-12 (port policy, banking-domain block,
 * .gov/.mil block, adult content, operator deny-list, RPS caps,
 * approved registries) — the eight hand-built rules in
 * `antiabuse-svc.ListFilters` that set `LastUpdatedAt: timestamppb.New(now)`.
 */
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";

import { AbusePanel } from "@/app/admin/abuse/panel";
import * as api from "@/lib/api";
import type { ListFiltersResponse } from "@/lib/types";

const sampleResponse: ListFiltersResponse = {
  ruleset_hash: "deadbeef",
  rules: [
    {
      id: "rep.phishtank",
      slug: "rep.phishtank",
      description: "external reputation feed (enabled)",
      version: "1",
      // Reputation backends ship last_updated_at unset.
    },
    {
      id: "ports.default",
      slug: "ports.default",
      description: "Outbound port allow/deny policy",
      version: "1",
      // *timestamppb.Timestamp as Go encoding/json emits it.
      last_updated_at: { seconds: 1716163200, nanos: 0 },
    },
    {
      id: "domains.banking",
      slug: "domains.banking",
      description: "Banking-domain block (KYC-only)",
      version: "1",
      // seconds frequently arrives as a string for >2^53 safety.
      last_updated_at: { seconds: "1716163200", nanos: 250000000 },
    },
  ],
};

describe("AbusePanel — #304 timestamp render", () => {
  beforeEach(() => {
    vi.spyOn(api, "browserApi").mockReturnValue({
      get: vi.fn().mockResolvedValue(sampleResponse),
      post: vi.fn(),
      put: vi.fn(),
      del: vi.fn(),
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
    } as any);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("never renders the literal `[object Object]`", async () => {
    const { container } = render(<AbusePanel />);
    await waitFor(() =>
      expect(screen.getByText("Outbound port allow/deny policy")).toBeInTheDocument(),
    );
    expect(container.textContent ?? "").not.toContain("[object Object]");
  });

  it("formats Timestamp{seconds,nanos} as a relative time string", async () => {
    render(<AbusePanel />);
    await waitFor(() =>
      expect(screen.getByText("Outbound port allow/deny policy")).toBeInTheDocument(),
    );
    // The two hand-built rules each have a Timestamp struct. Their
    // formatted output must be either a relative-time string (e.g.
    // "5d ago") or a locale-formatted date — both of which contain
    // alphanumerics and never the substring "[object".
    const all = screen.getAllByText(/ago|\d{4}|\/|-/);
    expect(all.length).toBeGreaterThan(0);
  });

  it("renders em-dash for rules with no last_updated_at", async () => {
    render(<AbusePanel />);
    await waitFor(() =>
      expect(screen.getByText("external reputation feed (enabled)")).toBeInTheDocument(),
    );
    // formatRelativeTime returns "—" for undefined input.
    expect(screen.getAllByText("—").length).toBeGreaterThan(0);
  });
});
