import { NextRequest, NextResponse } from "next/server";

import { auth } from "@/lib/auth";

/**
 * /api/onboard/start — Next.js BFF proxy to gateway-bff
 * `/api/v1/onboard/start`.
 *
 * The browser sends `{ token }`; we read the NextAuth session, mint a
 * fresh access token via the identity-svc shared secret, and forward
 * to the gateway. This keeps the actual JWT out of the browser bundle
 * — only the NextAuth cookie ever touches the client.
 *
 * In Phase 0 with no live identity-svc the proxy still works because
 * the auth middleware on gateway-bff accepts a dev "BFF service token"
 * (env IOGRID_BFF_SERVICE_TOKEN). Production wires real session→JWT
 * issuance via identity-svc.
 */
export async function POST(req: NextRequest) {
  const session = await auth();
  if (!session?.user) {
    return NextResponse.json(
      { code: "unauthenticated", message: "sign in first" },
      { status: 401 },
    );
  }

  const body = await req.text();
  const upstream = process.env.IOGRID_GATEWAY_BFF_URL ?? "http://localhost:8080";
  const serviceToken = process.env.IOGRID_BFF_SERVICE_TOKEN ?? "";

  const res = await fetch(`${upstream}/api/v1/onboard/start`, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      // Pass-through the user's id; gateway-bff in dev mode accepts
      // this without verifying the JWT signature (only set this in
      // local dev environments).
      "x-iogrid-user-id": (session.user as { id?: string }).id ?? "",
      ...(serviceToken
        ? { authorization: `Bearer ${serviceToken}` }
        : {}),
    },
    body,
  });

  const respBody = await res.text();
  return new NextResponse(respBody, {
    status: res.status,
    headers: { "content-type": res.headers.get("content-type") ?? "application/json" },
  });
}
