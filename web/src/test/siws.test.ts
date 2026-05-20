import { describe, expect, it, vi } from "vitest";
import { ApiClient } from "@/lib/api";
import {
  completeSiwsBinding,
  encodeSignature,
  listBoundWallets,
  startSiwsBinding,
  unbindWallet,
} from "@/lib/solana/siws";

function fakeClient(handler: (url: string, init: RequestInit) => Response): ApiClient {
  const fetcher = vi
    .fn<typeof fetch>()
    .mockImplementation(async (input, init) => {
      const url = typeof input === "string" ? input : (input as URL).toString();
      return handler(url, init ?? {});
    });
  return new ApiClient({
    baseUrl: "https://test.example",
    fetcher: fetcher as unknown as typeof fetch,
  });
}

describe("encodeSignature", () => {
  it("encodes a known byte sequence to base58", () => {
    // Input from bitcoin BIP-173 test vector: "Hello" → base58
    expect(encodeSignature(new Uint8Array([72, 101, 108, 108, 111]))).toBe(
      "9Ajdvzr",
    );
  });

  it("encodes the empty array to empty string", () => {
    expect(encodeSignature(new Uint8Array([]))).toBe("");
  });

  it("preserves leading zero bytes as leading '1's", () => {
    const out = encodeSignature(new Uint8Array([0, 0, 1]));
    expect(out.startsWith("11")).toBe(true);
  });
});

describe("SIWS bind flow API helpers", () => {
  it("POSTs the challenge endpoint with the wallet address and returns nonce + challenge bytes", async () => {
    const client = fakeClient((url, init) => {
      expect(url).toBe(
        "https://test.example/api/v1/account/wallets/challenge",
      );
      expect(init.method).toBe("POST");
      expect(init.body).toBe(JSON.stringify({ walletAddress: "WALLET" }));
      return new Response(
        JSON.stringify({
          nonce: "abc",
          challenge: "Sign in to iogrid: abc",
          expiresAt: "2026-05-19T00:00:00Z",
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    });
    const res = await startSiwsBinding(client, "WALLET");
    expect(res).toEqual({
      nonce: "abc",
      challenge: "Sign in to iogrid: abc",
      expiresAt: "2026-05-19T00:00:00Z",
    });
  });

  it("POSTs to /account/wallets with the signed payload to complete binding", async () => {
    const client = fakeClient((url, init) => {
      expect(url).toBe("https://test.example/api/v1/account/wallets");
      expect(JSON.parse(String(init.body))).toEqual({
        walletAddress: "WALLET",
        nonce: "abc",
        signature: "SIG",
      });
      return new Response(
        JSON.stringify({
          walletAddress: "WALLET",
          chain: "solana",
          boundAt: "2026-05-19T00:00:00Z",
        }),
        { status: 200 },
      );
    });
    const bound = await completeSiwsBinding(client, {
      walletAddress: "WALLET",
      nonce: "abc",
      signature: "SIG",
    });
    expect(bound.walletAddress).toBe("WALLET");
  });

  it("lists bound wallets via GET /account/wallets", async () => {
    const client = fakeClient((url, init) => {
      expect(url).toBe("https://test.example/api/v1/account/wallets");
      expect(init.method ?? "GET").toBe("GET");
      return new Response(
        JSON.stringify({
          wallets: [
            {
              walletAddress: "W1",
              chain: "solana",
              boundAt: "2026-05-19T00:00:00Z",
            },
          ],
        }),
        { status: 200 },
      );
    });
    const res = await listBoundWallets(client);
    expect(res.wallets).toHaveLength(1);
    expect(res.wallets[0].walletAddress).toBe("W1");
  });

  it("DELETEs an unbind request at /account/wallets/{address}", async () => {
    const client = fakeClient((url, init) => {
      expect(init.method).toBe("DELETE");
      expect(url).toBe("https://test.example/api/v1/account/wallets/W1");
      return new Response(null, { status: 204 });
    });
    await expect(unbindWallet(client, "W1")).resolves.toBeUndefined();
  });
});
