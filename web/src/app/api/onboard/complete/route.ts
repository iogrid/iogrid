import { NextRequest, NextResponse } from "next/server";

import { auth } from "@/lib/auth";

/**
 * /api/onboard/complete — Next.js BFF proxy to gateway-bff
 * `/api/v1/onboard/complete`. Same shape as ./start/route.ts but
 * forwards the wizard's chosen defaults.
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
  // Canonical iogrid coordinator env-var contract (#416 — see
  // docs/RUNBOOKS.md §5). Pre-#416 IOGRID_BFF_SERVICE_TOKEN is gone.
  const serviceToken = process.env.IOGRID_SERVICE_TOKEN ?? "";

  const res = await fetch(`${upstream}/api/v1/onboard/complete`, {
    method: "POST",
    headers: {
      "content-type": "application/json",
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
