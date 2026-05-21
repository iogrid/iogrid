/**
 * GET /readyz — Kubernetes readiness probe.
 *
 * Returns 200 when the admin app is ready to serve traffic. For Phase 1
 * we keep the same simple "Next.js server up" signal as /healthz; once
 * a per-request session-DB warmup is wired in, this probe should also
 * verify the Postgres DSN resolves (without holding a long-lived
 * connection in the probe path).
 */
export const dynamic = "force-dynamic";

export function GET() {
  return new Response("ok", {
    status: 200,
    headers: { "content-type": "text/plain; charset=utf-8" },
  });
}
