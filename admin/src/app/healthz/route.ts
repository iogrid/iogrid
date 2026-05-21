import { NextResponse } from "next/server";

/**
 * GET /healthz — liveness probe. Always 200 as long as the Node server
 * is up. Used by the Deployment liveness probe in
 * `infra/k8s/base/admin/deployment.yaml`.
 */
export const dynamic = "force-static";

export function GET() {
  return NextResponse.json({ ok: true });
}
