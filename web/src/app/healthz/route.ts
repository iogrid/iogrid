// Liveness probe target. The k8s Deployment's livenessProbe hits
// GET /healthz (infra/k8s/base/web/deployment.yaml). Without this route
// Next.js returns 404 → the probe fails → the container is killed and
// restarted in a crashloop, so NO new web image ever rolls out (prod web
// silently froze ~14 days on the old ReplicaSet). Keep it dependency-free:
// liveness means "the process is up and serving", nothing more.
export const dynamic = "force-dynamic";

export function GET() {
  return new Response("ok", {
    status: 200,
    headers: { "content-type": "text/plain; charset=utf-8", "cache-control": "no-store" },
  });
}
