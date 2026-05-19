import { describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "@/lib/api";

describe("ApiClient", () => {
  it("sends bearer token + JSON headers on GET", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ hello: "world" }), { status: 200 }),
      );
    const c = new ApiClient({
      baseUrl: "https://example.com",
      token: "abc",
      fetcher: fetcher as unknown as typeof fetch,
    });
    const out = await c.get<{ hello: string }>("/api/v1/me");
    expect(out).toEqual({ hello: "world" });
    expect(fetcher).toHaveBeenCalledOnce();
    const [url, init] = fetcher.mock.calls[0];
    expect(url).toBe("https://example.com/api/v1/me");
    const headers = init.headers as Record<string, string>;
    expect(headers.Authorization).toBe("Bearer abc");
    expect(headers["Content-Type"]).toBe("application/json");
  });

  it("throws an ApiError for non-2xx responses", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(
        new Response(
          JSON.stringify({ code: "bad_request", message: "nope" }),
          { status: 400 },
        ),
      );
    const c = new ApiClient({
      baseUrl: "https://example.com",
      fetcher: fetcher as unknown as typeof fetch,
    });
    await expect(c.get("/api/v1/me")).rejects.toMatchObject({
      status: 400,
      code: "bad_request",
      message: "nope",
    });
  });

  it("returns undefined on 204 (delete success)", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(new Response(null, { status: 204 }));
    const c = new ApiClient({
      baseUrl: "https://example.com",
      fetcher: fetcher as unknown as typeof fetch,
    });
    const out = await c.del("/api/v1/customer/api-keys/123?workspace_id=xyz");
    expect(out).toBeUndefined();
  });
});

describe("ApiError", () => {
  it("inherits from Error and carries status + code", () => {
    const e = new ApiError(429, "rate_limited", "slow down");
    expect(e).toBeInstanceOf(Error);
    expect(e.status).toBe(429);
    expect(e.code).toBe("rate_limited");
  });
});
