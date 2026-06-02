// Readiness probe target. The k8s Deployment's readinessProbe hits
// GET /readyz (infra/k8s/base/web/deployment.yaml). Without this route
// Next.js returns 404 → the pod never becomes Ready → the rollout stalls
// with ProgressDeadlineExceeded and the Service keeps routing to the old
// ReplicaSet. Returning 200 once Next is serving is correct: this is the
// Next.js front-end; it has no hard startup dependency to gate on (the BFF
// is reached per-request, and gating readiness on it would take the whole
// site down during a transient coordinator blip).
export const dynamic = "force-dynamic";

export function GET() {
  return new Response("ready", {
    status: 200,
    headers: { "content-type": "text/plain; charset=utf-8", "cache-control": "no-store" },
  });
}
