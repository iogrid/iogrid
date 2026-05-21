import { AdminShell } from "@/components/layout/admin-shell";
import { ADMIN_NAV } from "@/app/nav";

export const metadata = { title: "System health — iogrid admin" };

/**
 * /health — control-plane SLO + deployment-status surface (EPIC #422
 * Phase 1 stub).
 *
 * For Phase 0 the operator's primary "is the cluster healthy?"
 * signal is the Grafana dashboards behind grafana.iogrid.org. This
 * page exists so admin nav has a "Health" entry from day one; the
 * panel itself becomes a live cluster summary in a follow-up PR
 * (embeds Grafana panels via signed iframe URLs, plus the BFF's
 * /api/v1/admin/health/* aggregates).
 */
export default function AdminHealthPage() {
  return (
    <AdminShell
      badge="Admin"
      title="System health"
      subtitle="Cluster health, control-plane SLOs, deployment status."
      nav={ADMIN_NAV}
      activeHref="/health"
    >
      <div className="space-y-4">
        <div className="rounded-md border border-zinc-200 bg-zinc-50 p-4 text-sm dark:border-zinc-800 dark:bg-zinc-900/40">
          <p className="font-medium">
            Live dashboards live in Grafana for Phase 0.
          </p>
          <p className="mt-1 text-xs text-zinc-600 dark:text-zinc-400">
            Operator on-call view: <code>grafana.iogrid.org</code> →
            Dashboards → &ldquo;iogrid Control Plane&rdquo; folder. SLO
            panels: gateway-bff latency p99, providers-svc heartbeat
            success rate, antiabuse-svc throughput.
          </p>
        </div>
        <div className="rounded-md border border-zinc-200 p-4 text-sm dark:border-zinc-800">
          <h2 className="font-medium">What will land here</h2>
          <ul className="mt-2 list-inside list-disc space-y-1 text-xs text-zinc-600 dark:text-zinc-400">
            <li>
              Per-service health (gateway-bff, providers-svc,
              workloads-svc, antiabuse-svc, billing-svc, telemetry-svc,
              proxy-gateway, build-gateway, vpn-gateway) with last
              successful probe + recent error rate.
            </li>
            <li>
              Deployment status — image digest, replica count, rolling
              update progress.
            </li>
            <li>
              Embedded Grafana panels for the four SLOs (latency,
              error rate, saturation, traffic).
            </li>
          </ul>
        </div>
      </div>
    </AdminShell>
  );
}
