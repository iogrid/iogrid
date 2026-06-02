import type { Metadata } from "next";
import { LegalPage } from "@/components/marketing/legal-page";

export const metadata: Metadata = {
  title: "iogrid mobile — privacy policy",
  description:
    "iogrid mobile (iOS) privacy policy. Required for App Store Connect submission.",
};

/**
 * Mobile-app privacy policy — required for App Store submission
 * (#574). Linked from app.json's NSPrivacyPolicyURL and
 * appInfoLocalization.privacyPolicyUrl on App Store Connect.
 */
export default function MobilePrivacyPage() {
  return (
    <LegalPage title="iogrid mobile — privacy policy" lastUpdated="2026-06-02">
      <h2>Summary</h2>
      <p>
        iogrid mobile (iOS) is a VPN client. It routes your network traffic
        through residential peers operated by other iogrid users. We do not
        log, inspect, or sell your traffic. We do not run third-party
        analytics or advertising SDKs in this app.
      </p>

      <h2>What the app stores on your device</h2>
      <ul>
        <li>
          <strong>Apple sign-in token</strong> — securely held in the iOS
          Keychain. Used to authenticate API requests to iogrid&apos;s
          coordinator. Cleared on Sign out.
        </li>
        <li>
          <strong>WireGuard private key</strong> — generated locally,
          securely held in the iOS Keychain in an App Group container shared
          only with the iogrid VPN extension. Never leaves your device.
        </li>
        <li>
          <strong>Preferences</strong> — your region selection, auto-connect
          toggle, kill-switch toggle, DNS-leak-protection toggle. AsyncStorage
          (unencrypted, app-sandboxed).
        </li>
        <li>
          <strong>Wallet binding</strong> — if you bind a Phantom or Ping
          wallet for $GRID payments, only the public key is stored. Private
          keys remain inside the wallet app.
        </li>
      </ul>

      <h2>What the app sends to iogrid&apos;s servers</h2>
      <ul>
        <li>
          <strong>Apple ID token</strong> — at sign-in. Validated against
          Apple&apos;s JWKS; we store only a salted SHA-256 hash of the Apple
          subject identifier. Your email is never persisted.
        </li>
        <li>
          <strong>Wireguard handshake</strong> — your client public key, the
          region you chose, and (when paying with $GRID) a signed payment
          authorization. We do not see the encrypted tunnel contents.
        </li>
        <li>
          <strong>Byte counters</strong> — total bytes sent and received via
          the tunnel, reported every minute, used for billing only. We do not
          retain per-flow or per-destination data.
        </li>
      </ul>

      <h2>What the app does NOT collect</h2>
      <ul>
        <li>Browsing history, visited URLs, or DNS queries.</li>
        <li>
          Device identifiers (IDFA, IDFV) — we don&apos;t request the
          AppTrackingTransparency prompt and don&apos;t link any data to your
          device.
        </li>
        <li>Contacts, photos, calendar, location, or any other iOS data.</li>
        <li>
          Crash logs are reported to Apple&apos;s built-in TestFlight
          diagnostics only when you opt in at TestFlight install time; iogrid
          does not run its own crash-reporting SDK.
        </li>
      </ul>

      <h2>How we handle $GRID payments</h2>
      <p>
        Payments are made on the Solana blockchain. Transaction signatures are
        public by nature; your wallet address is visible to anyone inspecting
        the chain. iogrid&apos;s billing service settles provider payouts on a
        5-minute cron; aggregated transfer amounts are also public on-chain.
        We do not link your Apple identity to your wallet on any public
        record — the binding lives only in iogrid&apos;s internal database.
      </p>

      <h2>Third-party services</h2>
      <ul>
        <li>
          <strong>Apple</strong> — Sign in with Apple, App Store Connect
          analytics (aggregate), Apple Pay (for $GRID top-up via Apple Pay).
        </li>
        <li>
          <strong>Solana RPC providers</strong> — your wallet provider
          (Phantom / Ping) uses public Solana RPC endpoints to read your
          balance and submit transactions; iogrid does not see this traffic.
        </li>
      </ul>
      <p>
        iogrid does NOT use: Google Analytics, Firebase, Mixpanel, Amplitude,
        Sentry, Bugsnag, AppsFlyer, Adjust, Branch, or any tracking /
        attribution SDK.
      </p>

      <h2>Your rights</h2>
      <p>
        You can sign out at any time — this removes your Apple token + WG
        private key from the device. You can delete your iogrid account by
        emailing privacy@iogrid.org; we remove your salted Apple-subject
        hash, your wallet binding, and your aggregate byte counters within 30
        days. Solana on-chain payment records cannot be deleted by us (they
        are public ledger entries).
      </p>

      <h2>Changes</h2>
      <p>
        Material changes to this policy will be announced in-app via a
        modal you must acknowledge before continuing to use the VPN. Last
        updated: 2026-06-02.
      </p>

      <h2>Contact</h2>
      <p>privacy@iogrid.org</p>
    </LegalPage>
  );
}
