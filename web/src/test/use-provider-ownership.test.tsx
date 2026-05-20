import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { useProviderOwnership } from "@/lib/use-provider-ownership";

/**
 * useProviderOwnership behavioural tests for #313.
 *
 * The hook must:
 *  - start with `{hasProvider: null, loading: true}` so callers can
 *    render a neutral skeleton
 *  - flip to `hasProvider: false` when the BFF returns
 *    `{has_provider: false, ...}` (post-#310 contract)
 *  - flip to `hasProvider: true` when the BFF returns `has_provider: true`
 *  - default to `hasProvider: true` if the field is absent (older BFF
 *    build — graceful forward-compat)
 *  - default to `hasProvider: true` on fetch errors so we never block
 *    real dashboards behind a probe failure (anti-defensive-coding
 *    §3.3)
 */

const apiMocks = vi.hoisted(() => ({
  get: vi.fn(),
}));

vi.mock("@/lib/api", () => ({
  browserApi: () => ({ get: apiMocks.get }),
}));

beforeEach(() => {
  apiMocks.get.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("useProviderOwnership", () => {
  it("starts loading with hasProvider=null", () => {
    apiMocks.get.mockReturnValue(new Promise(() => {}));
    const { result } = renderHook(() => useProviderOwnership());
    expect(result.current.loading).toBe(true);
    expect(result.current.hasProvider).toBeNull();
  });

  it("resolves to hasProvider=false when BFF says has_provider:false", async () => {
    apiMocks.get.mockResolvedValue({ has_provider: false, providers: null });
    const { result } = renderHook(() => useProviderOwnership());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.hasProvider).toBe(false);
    expect(result.current.error).toBeNull();
  });

  it("resolves to hasProvider=true when BFF says has_provider:true", async () => {
    apiMocks.get.mockResolvedValue({ has_provider: true, providers: [{}] });
    const { result } = renderHook(() => useProviderOwnership());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.hasProvider).toBe(true);
  });

  it("defaults to hasProvider=true when flag is absent (forward-compat)", async () => {
    apiMocks.get.mockResolvedValue({ earnings: null, state: null });
    const { result } = renderHook(() => useProviderOwnership());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.hasProvider).toBe(true);
  });

  it("defaults to hasProvider=true on fetch error (don't mask upstream)", async () => {
    apiMocks.get.mockRejectedValue(new Error("network"));
    const { result } = renderHook(() => useProviderOwnership());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.hasProvider).toBe(true);
    expect(result.current.error).toBe("network");
  });
});
