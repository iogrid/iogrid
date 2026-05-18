import type { Metadata } from "next";
import { Hero } from "@/components/Hero";
import { PricingTable } from "@/components/PricingTable";
import { InstallButtons } from "@/components/InstallButtons";
import { vpnPricing } from "@/content/pricing";

export const metadata: Metadata = {
  title: "iogrid VPN — free, transparent, mesh-routed",
  description:
    "Free consumer VPN funded by bandwidth swap. You opt in to which categories transit your IP, and block any you don't want. Or pay $2.99 for Plus with zero bandwidth swap.",
};

export default function VPNPage() {
  return (
    <>
      <Hero
        eyebrow="Consumer VPN"
        title="The free VPN that shows you the deal."
        subtitle={
          <>
            Most free VPNs sell your bandwidth without telling you (looking at
            you, Hola). We tell you. You choose what kinds of traffic transit
            your IP. You can block any category at any time. Or pay $2.99 to skip
            the swap entirely.
          </>
        }
        primaryCta={{ href: "/install/mac", label: "Download free" }}
        secondaryCta={{ href: "#how", label: "How the free tier works" }}
      />

      <section id="how" className="container-page py-16">
        <div className="mx-auto max-w-3xl">
          <h2 className="h-section text-neutral-900">
            How a free, transparent VPN is possible
          </h2>
          <ol className="mt-6 space-y-4 text-neutral-700">
            <li>
              <strong className="text-neutral-900">1. You install iogrid VPN.</strong>{" "}
              Standard WireGuard tunnel to a mesh exit node in your chosen country.
            </li>
            <li>
              <strong className="text-neutral-900">2. Mesh swap.</strong> In
              exchange for a free tunnel, you contribute a small amount of
              bandwidth back to the mesh — capped at 5 GB / month by default,
              and only when your machine is idle.
            </li>
            <li>
              <strong className="text-neutral-900">3. Full transparency.</strong>{" "}
              Open the app to see every byte that&rsquo;s transited your IP,
              labeled by category. Block what you don&rsquo;t want.
            </li>
            <li>
              <strong className="text-neutral-900">4. Or skip the swap.</strong>{" "}
              Plus tier is $2.99 / month — no bandwidth contribution required,
              priority server pool, streaming-friendly residential exits.
            </li>
          </ol>
        </div>
      </section>

      <section className="container-page py-12">
        <div className="mx-auto max-w-2xl">
          <InstallButtons />
        </div>
      </section>

      <PricingTable
        tiers={vpnPricing}
        caption="Free is funded by enterprise customers, not by hidden ads. The math is on /token and /providers."
      />

      <section className="container-page py-16">
        <div className="mx-auto max-w-3xl text-center">
          <h2 className="h-section text-neutral-900">No-log claim with proof</h2>
          <p className="mt-4 text-lead">
            Provider audit logs are cryptographically signed and published. If
            we ever decrypted a customer&rsquo;s HTTPS payload, the audit log
            would show it (we don&rsquo;t — and we can&rsquo;t, by design). The
            daemon is open source under AGPL. Verify it yourself.
          </p>
        </div>
      </section>
    </>
  );
}
