import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import { Cpu } from "lucide-react";

import { PersonaPickerCard } from "@/components/welcome/PersonaPickerCard";

// Mocks for next/navigation router + sonner toast + browserApi PUT.
// The card composes all three; each test pins one collaborator at a
// time.
const pushMock = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: pushMock }),
}));

const toastErrorMock = vi.fn();
vi.mock("sonner", () => ({
  toast: { error: (...args: unknown[]) => toastErrorMock(...args) },
}));

const putMock = vi.fn();
vi.mock("@/lib/api", () => ({
  browserApi: () => ({
    put: (path: string, body: unknown) => putMock(path, body),
  }),
}));

beforeEach(() => {
  pushMock.mockReset();
  toastErrorMock.mockReset();
  putMock.mockReset();
});

describe("PersonaPickerCard (EPIC #422 /welcome picker)", () => {
  it("renders the badge + title + blurb + CTA", () => {
    render(
      <PersonaPickerCard
        icon={Cpu}
        badge="Provider"
        title="Share my hardware and earn."
        blurb="Donate spare CPU, GPU, and bandwidth."
        cta="Become a provider"
        persona="provider"
      />,
    );
    expect(screen.getByText("Provider")).toBeInTheDocument();
    expect(screen.getByText("Share my hardware and earn.")).toBeInTheDocument();
    expect(screen.getByText(/Donate spare CPU/)).toBeInTheDocument();
    expect(screen.getByText(/Become a provider/)).toBeInTheDocument();
  });

  it("PUTs the role + navigates on click (happy path)", async () => {
    putMock.mockResolvedValue({});
    render(
      <PersonaPickerCard
        icon={Cpu}
        badge="Provider"
        title="t"
        blurb="b"
        cta="cta"
        persona="provider"
      />,
    );
    fireEvent.click(screen.getByTestId("welcome-pick-provider"));
    await waitFor(() => expect(putMock).toHaveBeenCalledOnce());
    expect(putMock).toHaveBeenCalledWith("/api/v1/me/preferred-landing-role", {
      role: "provider",
    });
    await waitFor(() => expect(pushMock).toHaveBeenCalledOnce());
    expect(pushMock).toHaveBeenCalledWith("/provider?from=welcome");
    expect(toastErrorMock).not.toHaveBeenCalled();
  });

  it("toasts + keeps the user on /welcome when the PUT rejects", async () => {
    putMock.mockRejectedValue(new Error("503 identity_svc_unavailable"));
    render(
      <PersonaPickerCard
        icon={Cpu}
        badge="Customer"
        title="t"
        blurb="b"
        cta="cta"
        persona="customer"
      />,
    );
    fireEvent.click(screen.getByTestId("welcome-pick-customer"));
    await waitFor(() => expect(toastErrorMock).toHaveBeenCalledOnce());
    expect(toastErrorMock.mock.calls[0][0]).toMatch(/Couldn't save your pick/);
    expect(toastErrorMock.mock.calls[0][0]).toMatch(/503 identity_svc_unavailable/);
    expect(pushMock).not.toHaveBeenCalled();
  });

  it("blocks double-click while submitting", async () => {
    let resolvePut: (() => void) | null = null;
    putMock.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          resolvePut = resolve;
        }),
    );
    render(
      <PersonaPickerCard
        icon={Cpu}
        badge="VPN"
        title="t"
        blurb="b"
        cta="cta"
        persona="vpn"
      />,
    );
    const btn = screen.getByTestId("welcome-pick-vpn");
    fireEvent.click(btn);
    fireEvent.click(btn);
    fireEvent.click(btn);
    expect(putMock).toHaveBeenCalledTimes(1);
    expect(btn).toBeDisabled();
    if (resolvePut) {
      // local copy sidesteps TS2349 on the optional-chain call form;
      // narrowing through if-guard alone wasn't enough under the
      // project's strict-mode tsconfig.
      const r: () => void = resolvePut;
      r();
    }
    await waitFor(() => expect(pushMock).toHaveBeenCalledOnce());
  });

  it("encodes the persona in both the PUT body AND the push target", async () => {
    putMock.mockResolvedValue({});
    for (const persona of ["provider", "customer", "vpn"] as const) {
      pushMock.mockReset();
      putMock.mockReset();
      putMock.mockResolvedValue({});
      const { unmount } = render(
        <PersonaPickerCard
          icon={Cpu}
          badge={persona}
          title="t"
          blurb="b"
          cta="cta"
          persona={persona}
        />,
      );
      fireEvent.click(screen.getByTestId(`welcome-pick-${persona}`));
      await waitFor(() => expect(putMock).toHaveBeenCalledOnce());
      expect(putMock).toHaveBeenCalledWith("/api/v1/me/preferred-landing-role", {
        role: persona,
      });
      await waitFor(() => expect(pushMock).toHaveBeenCalledOnce());
      expect(pushMock).toHaveBeenCalledWith(`/${persona}?from=welcome`);
      unmount();
    }
  });
});
