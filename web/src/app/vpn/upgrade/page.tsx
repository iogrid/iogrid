import Link from "next/link";
import { UpgradePanel } from "./panel";

export const metadata = { title: "iogrid VPN — Upgrade" };

/**
 * /vpn/upgrade — three plan cards (Plus / Pro / Enterprise) each
 * starting a Stripe Checkout session via POST /api/v1/vpn/upgrade.
 */
export default function VpnUpgradePage() {
  return (
    <main className="mx-auto max-w-4xl px-6 py-12">
      <Link href="/install" className="text-sm text-muted-foreground hover:underline">
        ← Back to install
      </Link>
      <h1 className="mt-6 text-3xl font-bold">Upgrade iogrid VPN</h1>
      <p className="mt-2 text-sm text-muted-foreground dark:text-muted-foreground">
        The free tier ships 50 GB/month. Pick a paid plan for higher quotas,
        more concurrent regions, and per-app exit selection.
      </p>
      <UpgradePanel />
    </main>
  );
}
