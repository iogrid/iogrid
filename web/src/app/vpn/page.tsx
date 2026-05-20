import { redirect } from "next/navigation";

export const metadata = { title: "iogrid VPN — Install" };

/**
 * /vpn used to host a bespoke 5-card install grid that diverged from
 * the canonical /install page (wrong Linux filename, vague Windows,
 * missing top nav). See #306.
 *
 * The daemon and the consumer VPN client are the same binary, so a
 * separate "VPN install" page only duplicated UX. We permanently
 * redirect to /install, where the per-arch matrix is the single
 * source of truth for download URLs.
 *
 * The paid-tier upgrade flow continues to live at /vpn/upgrade.
 */
export default function VpnPage() {
  redirect("/install");
}
