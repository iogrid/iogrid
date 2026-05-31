import { PortalShell } from "@/components/layout/portal-shell";
import { CUSTOMER_NAV } from "@/app/customer/nav";
import { VpnPanel } from "./panel";

export const metadata = { title: "VPN — iogrid customer" };

/**
 * /customer/vpn — customer-facing VPN dashboard.
 *
 * - Shows active VPN sessions (region, provider, bytes_in/out, connected_at).
 * - Mints / lists / revokes VPN-tagged API keys (reuses billing-svc ApiKeyService).
 * - One-click copy of the iogrid CLI install snippet.
 *
 * Closes #541. Refs #504.
 */
export default function CustomerVpnPage() {
  return (
    <PortalShell
      badge="Customer"
      title="VPN"
      subtitle="P2P VPN with residential exit IPs — mint a key, install the CLI, connect."
      nav={CUSTOMER_NAV}
      activeHref="/customer/vpn"
    >
      <VpnPanel />
    </PortalShell>
  );
}
