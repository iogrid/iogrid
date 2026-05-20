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

  it("returns {} on 501 + unimplemented (Phase 0 empty-state, not error)", async () => {
    // gateway-bff translates Connect CodeUnimplemented → HTTP 501 with
    // body `{code:"unimplemented", message:"..."}`. Callers should see
    // the empty-state path, not a "Couldn't load X" banner. (#300)
    const fetcher = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          code: "unimplemented",
          message: "unimplemented: 404 Not Found",
        }),
        { status: 501 },
      ),
    );
    const c = new ApiClient({
      baseUrl: "https://example.com",
      fetcher: fetcher as unknown as typeof fetch,
    });
    const out = await c.get<{ rows?: unknown[] }>("/api/v1/customer/usage");
    expect(out).toEqual({});
    expect((out?.rows ?? []).length).toBe(0);
  });

  it("still throws ApiError for 500 / 502 / 503 (genuine failures)", async () => {
    // 500-class failures must NOT be swallowed — those are real bugs
    // the operator needs to see. Only 501 + code:unimplemented gets
    // the empty-state treatment. (#300)
    for (const status of [500, 502, 503]) {
      const fetcher = vi
        .fn()
        .mockResolvedValue(
          new Response(
            JSON.stringify({ code: "upstream_error", message: "boom" }),
            { status },
          ),
        );
      const c = new ApiClient({
        baseUrl: "https://example.com",
        fetcher: fetcher as unknown as typeof fetch,
      });
      await expect(c.get("/api/v1/customer/usage")).rejects.toMatchObject({
        status,
      });
    }
  });

  it("still throws on 501 if code is not 'unimplemented'", async () => {
    // Defensive: a 501 from an upstream proxy / WAF that doesn't carry
    // the unimplemented envelope is a real failure — surface it.
    const fetcher = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ code: "not_implemented_proxy" }), {
          status: 501,
        }),
      );
    const c = new ApiClient({
      baseUrl: "https://example.com",
      fetcher: fetcher as unknown as typeof fetch,
    });
    await expect(c.get("/api/v1/customer/usage")).rejects.toMatchObject({
      status: 501,
    });
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
