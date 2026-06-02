import type { Metadata } from "next";
import { LegalPage } from "@/components/marketing/legal-page";

export const metadata: Metadata = {
  title: "iogrid mobile — end-user license agreement",
  description:
    "iogrid mobile (iOS) EULA. Apple's standard Licensed Application End User License Agreement applies unless this document overrides specific clauses.",
};

/**
 * Mobile-app EULA — referenced from App Store Connect submission
 * (#574). For most clauses we accept Apple's standard
 * "Licensed Application End User License Agreement" (https://www.apple.com/legal/internet-services/itunes/dev/stdeula/);
 * this page enumerates the iogrid-specific overrides.
 */
export default function MobileEulaPage() {
  return (
    <LegalPage title="iogrid mobile — EULA" lastUpdated="2026-06-02">
      <h2>Baseline: Apple&apos;s Standard EULA</h2>
      <p>
        Except where this document explicitly overrides a clause, your use of
        the iogrid mobile (iOS) app is governed by Apple&apos;s{" "}
        <a
          href="https://www.apple.com/legal/internet-services/itunes/dev/stdeula/"
          rel="noopener noreferrer"
        >
          Licensed Application End User License Agreement
        </a>
        .
      </p>

      <h2>iogrid-specific terms</h2>

      <h3>1. Service description</h3>
      <p>
        iogrid is a peer-to-peer VPN: your network traffic exits via another
        consumer&apos;s home internet connection (a &quot;provider&quot;).
        You agree that:
      </p>
      <ul>
        <li>
          Provider IP addresses change as providers come online / offline; we
          do not guarantee a specific provider for any session.
        </li>
        <li>
          Traffic that violates local law at either your location or the
          provider&apos;s location is your responsibility. iogrid&apos;s
          anti-abuse system terminates sessions that hit abuse signals
          (malware C2, child-safety reports, etc.); other use is your call.
        </li>
        <li>
          We do not guarantee bypassing geo-restrictions. Streaming services
          may detect and block residential VPN exit IPs.
        </li>
      </ul>

      <h3>2. $GRID token payments</h3>
      <ul>
        <li>
          $GRID is an SPL token on the Solana blockchain. You can buy $GRID
          via Phantom/Ping (USDC or Apple Pay) or transfer it from another
          wallet.
        </li>
        <li>
          $GRID is burned at a metered rate proportional to bytes consumed
          through the tunnel (~0.001 $GRID per GB at v1, subject to change
          with 14-day in-app notice).
        </li>
        <li>
          Refunds for $GRID balance issues are at iogrid&apos;s discretion;
          tokens consumed for traffic already delivered are non-refundable.
        </li>
        <li>
          Token price volatility is not iogrid&apos;s responsibility. We do
          not guarantee a specific exchange rate to USD or any fiat.
        </li>
      </ul>

      <h3>3. Provider role (if you opt in)</h3>
      <p>
        The iogrid mobile app is currently <em>consumer-only</em> (VPN
        client). The provider daemon runs on macOS / Linux / Windows
        desktops. If you intend to share your home bandwidth, install the
        desktop client at iogrid.org/downloads.
      </p>

      <h3>4. Account loss</h3>
      <p>
        iogrid uses Sign in with Apple. If you lose access to your Apple ID,
        you lose access to your iogrid account, your bound wallet binding,
        and any $GRID balance held in escrow. Apple controls Apple ID
        recovery; iogrid cannot recover your account on your behalf.
      </p>

      <h3>5. Termination</h3>
      <p>
        We may terminate or suspend your access without notice for abuse
        (illegal traffic, attempting to circumvent the anti-abuse system,
        attempting to defraud providers). Standard usage that violates none
        of clause 1 above will not trigger termination.
      </p>

      <h3>6. Disclaimers</h3>
      <p>
        iogrid is provided AS-IS. We do not guarantee uptime, throughput,
        latency, geographic availability, or the absence of any specific
        category of failure. We are not responsible for losses (financial,
        data, or otherwise) arising from VPN downtime or compromised
        provider IPs.
      </p>

      <h3>7. Changes</h3>
      <p>
        Material changes to this EULA will be announced in-app via a modal
        you must acknowledge before continuing to use the VPN. Last
        updated: 2026-06-02.
      </p>

      <h2>Contact</h2>
      <p>legal@iogrid.org</p>
    </LegalPage>
  );
}
