/**
 * Vitest coverage for /account/identifiers (issue #801).
 *
 * #801 regression: the page rendered an EMPTY list ("No identifiers
 * bound") even though GET /api/v1/me returned a verified magic-link
 * identifier. Root cause was the gateway-bff GetMe handler serialising
 * the proto response with stdlib encoding/json (snake_case +
 * enum-as-int: `verified_email`, `"kind":2`) while the panel — and the
 * rest of the codebase post-#630/#633 — speaks canonical proto3-JSON
 * (camelCase + enum-as-string: `verifiedEmail`,
 * `"kind":"IDENTIFIER_KIND_MAGIC_LINK"`). The BFF was moved to protojson;
 * the panel reads the canonical shape with the snake_case twin kept as a
 * transition fallback.
 *
 * Pins:
 *   - Canonical proto3-JSON identifier renders the row + Verified pill
 *     + correct "Magic-link email" label (the exact bug: list must NOT
 *     collapse to its empty state).
 *   - The legacy stdlib snake_case + numeric-enum shape still renders
 *     (so a stale BFF pod mid-roll doesn't blank the page).
 *   - An empty identifiers array renders the "No identifiers bound" copy.
 *   - A failed /api/v1/me call renders the load-error copy.
 */
import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";

import { IdentifiersPanel } from "@/app/account/identifiers/panel";

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

const apiGet = vi.fn();
const apiDel = vi.fn();
vi.mock("@/lib/api", () => ({
  // ApiError is referenced by the panel's catch block; provide a real class.
  ApiError: class ApiError extends Error {
    code: string;
    constructor(code: string, message: string) {
      super(message);
      this.code = code;
    }
  },
  browserApi: () => ({ get: apiGet, del: apiDel }),
}));

const ID_VALUE = "720a2323-8f8f-43a9-a90d-ed5202800dd7";
const USER_ID = "18c9fd5d-0000-0000-0000-000000000000";

describe("IdentifiersPanel", () => {
  beforeEach(() => {
    apiGet.mockReset();
    apiDel.mockReset();
  });

  it("renders a verified magic-link row from the canonical proto3-JSON shape (#801)", async () => {
    // This is exactly what gateway-bff GetMe now emits via protojson.
    apiGet.mockResolvedValueOnce({
      user: { id: { value: USER_ID }, primaryEmail: "emrah.baysal@openova.io" },
      identifiers: [
        {
          id: { value: ID_VALUE },
          userId: { value: USER_ID },
          kind: "IDENTIFIER_KIND_MAGIC_LINK",
          verifiedEmail: "emrah.baysal@openova.io",
          subject: "",
          createdAt: "2026-06-14T05:14:45Z",
          lastUsedAt: "2026-06-14T05:14:45Z",
        },
      ],
    });

    render(<IdentifiersPanel />);

    // The whole point of #801: the email row renders — NOT the empty state.
    await waitFor(() =>
      expect(screen.getByText("emrah.baysal@openova.io")).toBeInTheDocument(),
    );
    expect(screen.getByText("Verified")).toBeInTheDocument();
    expect(screen.getByText(/Magic-link email/)).toBeInTheDocument();
    expect(
      screen.queryByText(/No identifiers bound/i),
    ).not.toBeInTheDocument();
    // The Remove button is wired to the canonical id.value.
    expect(screen.getByTestId("remove-identifier")).toBeInTheDocument();
  });

  it("still renders the legacy stdlib snake_case + numeric-enum shape", async () => {
    // Pre-#801 gateway-bff (encoding/json) — kept working as a fallback
    // so a stale pod during a rolling deploy doesn't blank the page.
    apiGet.mockResolvedValueOnce({
      user: { id: { value: USER_ID }, primary_email: "legacy@iogrid.org" },
      identifiers: [
        {
          id: { value: ID_VALUE },
          user_id: { value: USER_ID },
          kind: 2,
          verified_email: "legacy@iogrid.org",
        },
      ],
    });

    render(<IdentifiersPanel />);

    await waitFor(() =>
      expect(screen.getByText("legacy@iogrid.org")).toBeInTheDocument(),
    );
    expect(screen.getByText("Verified")).toBeInTheDocument();
    expect(screen.getByText(/Magic-link email/)).toBeInTheDocument();
    expect(
      screen.queryByText(/No identifiers bound/i),
    ).not.toBeInTheDocument();
  });

  it("renders the empty-state copy when no identifiers are bound", async () => {
    apiGet.mockResolvedValueOnce({
      user: { id: { value: USER_ID }, primaryEmail: "nobody@iogrid.org" },
      identifiers: [],
    });

    render(<IdentifiersPanel />);

    await waitFor(() =>
      expect(screen.getByText(/No identifiers bound/i)).toBeInTheDocument(),
    );
  });

  it("renders the load-error copy when /api/v1/me fails", async () => {
    apiGet.mockRejectedValueOnce(new Error("boom"));

    render(<IdentifiersPanel />);

    await waitFor(() =>
      expect(
        screen.getByText(/Couldn't load identifiers/i),
      ).toBeInTheDocument(),
    );
  });
});
