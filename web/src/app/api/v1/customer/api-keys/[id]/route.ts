/**
 * DELETE /api/v1/customer/api-keys/[id] — same-origin BFF proxy (#244).
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function DELETE(req: NextRequest) {
  return proxyToBff(req);
}
