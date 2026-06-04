import { NextResponse } from "next/server";

/**
 * Same-origin status feed (#674).
 *
 * The StatusPageClient island polls this instead of api.iogrid.org
 * directly because the BFF's public /status/* routes sit outside its
 * CORS'd /api/v1 subtree, and a status page should not depend on
 * cross-origin preflights anyway. Server-side we reach gateway-bff
 * over the in-cluster URL (IOGRID_GATEWAY_BFF_URL — the same env the
 * NextAuth signIn hook uses), falling back to the public API origin
 * for local dev.
 *
 *   GET /status/feed            -> posture (overall + services + incidents)
 *   GET /status/feed?kind=uptime -> uptime ledger
 */
export const dynamic = "force-dynamic";

const UPSTREAM_PATHS: Record<string, string> = {
  posture: "/status/posture",
  uptime: "/status/uptime",
};

export async function GET(req: Request) {
  const params = new URL(req.url).searchParams;
  const kind = params.get("kind") ?? "posture";
  let path = UPSTREAM_PATHS[kind];
  if (!path) {
    return NextResponse.json({ error: "unknown feed kind" }, { status: 400 });
  }
  // The uptime ledger is per-service (#689). Forward EXACTLY one
  // validated param — the route otherwise forwards nothing, mirroring
  // the BFF's anti-open-proxy stance.
  const service = params.get("service");
  if (service) {
    if (!/^[a-z0-9-]{1,40}$/.test(service)) {
      return NextResponse.json({ error: "invalid service" }, { status: 400 });
    }
    path += `?service=${encodeURIComponent(service)}`;
  }
  const base = (
    process.env.IOGRID_GATEWAY_BFF_URL ?? "https://api.iogrid.org"
  ).replace(/\/+$/, "");
  try {
    const upstream = await fetch(`${base}${path}`, {
      signal: AbortSignal.timeout(5000),
      cache: "no-store",
    });
    const body = await upstream.text();
    return new NextResponse(body, {
      status: upstream.status,
      headers: {
        "content-type": "application/json",
        // Mirror the BFF's short shared cache so a hot page doesn't
        // stampede the posture generator.
        "cache-control": "public, max-age=15",
      },
    });
  } catch {
    return NextResponse.json(
      { error: "status feed unreachable" },
      { status: 502 },
    );
  }
}
