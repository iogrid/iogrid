import { NextResponse } from "next/server";

/**
 * GET /readyz — readiness probe. Same shape as /healthz today; if we
 * later want to gate readiness on Postgres reachability we extend this
 * handler. Used by the Deployment readiness probe in
 * `infra/k8s/base/admin/deployment.yaml`.
 */
export const dynamic = "force-static";

export function GET() {
  return NextResponse.json({ ok: true });
}
