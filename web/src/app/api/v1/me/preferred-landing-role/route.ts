/**
 * PUT /api/v1/me/preferred-landing-role — same-origin BFF proxy (issue #644).
 *
 * Backs the /welcome persona picker (EPIC #422). PersonaPickerCard PUTs
 * `{ role }` here; gateway-bff (routes.go: SetMyPreferredLandingRole)
 * forwards to identity-svc which owns the enum-cast validation.
 *
 * Without this route the PUT hit Next.js's default 404 and the picker
 * toasted "Couldn't save your pick" — the landing-role never persisted
 * (#644, same class as #641 / #289). Mirrors me/identifiers/[id]/route.ts.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function PUT(req: NextRequest) {
  return proxyToBff(req);
}
