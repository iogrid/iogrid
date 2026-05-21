/**
 * GET /healthz — Kubernetes liveness probe.
 *
 * Returns 200 when the Next.js server is up. Does NOT touch the
 * database or upstream services — the kubelet uses this signal to
 * decide whether to restart the pod; any dependency check here would
 * trigger crash-loops if a transient downstream blip flagged liveness
 * as failed.
 */
export const dynamic = "force-dynamic";

export function GET() {
  return new Response("ok", {
    status: 200,
    headers: { "content-type": "text/plain; charset=utf-8" },
  });
}
