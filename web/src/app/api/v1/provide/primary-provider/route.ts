/**
 * PUT /api/v1/provide/primary-provider — same-origin BFF proxy (#325).
 *
 * Forwards to gateway-bff → providers-svc.SetPrimaryProvider. The
 * server validates that the caller owns the requested provider_id
 * inside the SQL UPDATE; the BFF translates a PERMISSION_DENIED into
 * HTTP 403 here.
 *
 * PUT body: { provider_id: "<UUID>" }
 *
 * Used by the schedule editor picker when an owner with ≥2 paired
 * daemons re-elects which one answers for /provide/* by default.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function PUT(req: NextRequest) {
  return proxyToBff(req);
}
