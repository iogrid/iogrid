import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import {
  WorkspaceSelector,
  readActiveWorkspaceId,
  writeActiveWorkspaceId,
} from "@/components/layout/workspace-selector";

const FIXTURE = {
  workspaces: [
    {
      id: "11111111-1111-1111-1111-111111111111",
      name: "Personal",
      plan: "FREE",
      owner_user_id: "u-1",
      caller_role: "OWNER",
    },
    {
      id: "22222222-2222-2222-2222-222222222222",
      name: "Acme Corp",
      plan: "STARTER",
      owner_user_id: "u-1",
      caller_role: "ADMIN",
    },
  ],
};

describe("WorkspaceSelector", () => {
  let originalFetch: typeof fetch;

  beforeEach(() => {
    originalFetch = global.fetch;
    window.localStorage.clear();
  });

  afterEach(() => {
    global.fetch = originalFetch;
  });

  it("fetches and renders both workspaces with role badge", async () => {
    global.fetch = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify(FIXTURE), { status: 200 }),
      ) as unknown as typeof fetch;

    render(<WorkspaceSelector />);

    await waitFor(() => {
      expect(screen.getByRole("combobox")).toBeInTheDocument();
    });
    const options = screen.getAllByRole("option");
    expect(options).toHaveLength(2);
    expect(options[0].textContent).toContain("Personal");
    expect(options[0].textContent).toContain("OWNER");
    expect(options[1].textContent).toContain("Acme Corp");
    expect(options[1].textContent).toContain("ADMIN");
  });

  it("persists the active workspace in localStorage on switch", async () => {
    global.fetch = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify(FIXTURE), { status: 200 }),
      ) as unknown as typeof fetch;

    render(<WorkspaceSelector />);
    await waitFor(() => {
      expect(screen.getByRole("combobox")).toBeInTheDocument();
    });

    // Initial pick is the first workspace.
    expect(readActiveWorkspaceId()).toBe(FIXTURE.workspaces[0].id);

    fireEvent.change(screen.getByRole("combobox"), {
      target: { value: FIXTURE.workspaces[1].id },
    });
    expect(readActiveWorkspaceId()).toBe(FIXTURE.workspaces[1].id);
  });

  it("dispatches iogrid:workspaceChanged on switch", () => {
    const handler = vi.fn();
    window.addEventListener("iogrid:workspaceChanged", handler);
    writeActiveWorkspaceId("abc-123");
    window.removeEventListener("iogrid:workspaceChanged", handler);
    expect(handler).toHaveBeenCalled();
  });

  it("renders an error state when the upstream fails", async () => {
    global.fetch = vi
      .fn()
      .mockResolvedValue(new Response(null, { status: 503 })) as unknown as typeof fetch;
    render(<WorkspaceSelector />);
    await waitFor(() => {
      expect(screen.getByRole("alert")).toHaveTextContent(
        /workspaces unavailable/i,
      );
    });
  });

  it("renders 'No workspace' when the upstream returns an empty list", async () => {
    global.fetch = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ workspaces: [] }), { status: 200 }),
      ) as unknown as typeof fetch;
    render(<WorkspaceSelector />);
    await waitFor(() => {
      expect(screen.getByText(/no workspace/i)).toBeInTheDocument();
    });
  });
});
