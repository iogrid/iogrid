// PacketTunnelProvider.swift — the iOS NetworkExtension entry point.
//
// This Swift class runs in a SEPARATE PROCESS from the main Expo /
// React Native app. iOS sandboxes NE processes hard: it has its own
// PID, no shared memory with the host, communicates only via:
//   - NEVPNProtocol.providerConfiguration (stringly-typed dict set by
//     the main app via NETunnelProviderManager BEFORE startTunnel)
//   - handleAppMessage (binary IPC during tunnel lifetime)
//   - the App Group Keychain (shared via the `group.io.iogrid.app`
//     access group — the only way to pass a WG private key from the
//     main app's Keychain into the extension without exposing it on
//     disk in plaintext)
//
// Flow:
//   1. Main app generates customer WG keypair, stores private key in
//      App Group Keychain under a known label.
//   2. Main app calls coordinator POST /v1/vpn/sessions, gets a
//      provider WG pubkey + endpoint + AllowedIPs.
//   3. Main app sets NEVPNProtocol.providerConfiguration with
//      {peerPublicKey, peerEndpoint, allowedIPs, customerInnerCIDR}
//      and calls connection.startVPNTunnel().
//   4. iOS launches THIS process, calls startTunnel(options:).
//   5. We read the config, read the private key from Keychain, build
//      a WireGuardAdapter, call .start.
//
// Refs #568. Pairs with the Expo config plugin
// `plugins/with-network-extension.ts` that wires this file into the
// Xcode project at prebuild time.

import Foundation
import NetworkExtension
import os.log

// WireGuardKit comes from the wireguard-apple Swift package, embedded
// via SwiftPM in the extension target by the config plugin. The
// import is gated behind a #if canImport so this file still compiles
// in a basic scaffold before the package is wired (matters during
// the v1 bootstrap — we ship the .swift first, the package wiring
// follows in the same #568 milestone).
#if canImport(WireGuardKit)
import WireGuardKit
#endif

@objc(PacketTunnelProvider)
final class PacketTunnelProvider: NEPacketTunnelProvider {

    private let logger = OSLog(subsystem: "io.iogrid.app.PacketTunnelProvider", category: "tunnel")

#if canImport(WireGuardKit)
    private lazy var adapter: WireGuardAdapter = {
        return WireGuardAdapter(with: self) { [weak self] logLevel, message in
            guard let self = self else { return }
            os_log("%{public}@", log: self.logger, type: .info, message)
        }
    }()
#endif

    // ── Lifecycle ────────────────────────────────────────────────

    override func startTunnel(
        options: [String: NSObject]?,
        completionHandler: @escaping (Error?) -> Void
    ) {
        os_log("startTunnel called", log: logger, type: .info)

        guard let providerConfig = (protocolConfiguration as? NETunnelProviderProtocol)?
            .providerConfiguration
        else {
            os_log("missing providerConfiguration", log: logger, type: .error)
            completionHandler(PacketTunnelError.missingProviderConfiguration)
            return
        }

        do {
            let config = try TunnelConfigurationDecoder.decode(providerConfig)
#if canImport(WireGuardKit)
            adapter.start(tunnelConfiguration: config.wireguardConfig) { [weak self] error in
                guard let self = self else { return }
                if let error = error {
                    os_log("WireGuardAdapter.start failed: %{public}@",
                           log: self.logger, type: .error,
                           String(describing: error))
                    completionHandler(error)
                    return
                }
                os_log("tunnel established", log: self.logger, type: .info)
                completionHandler(nil)
            }
#else
            // Compile path for the scaffold-only build before
            // WireGuardKit is wired in. Fail explicitly rather than
            // silently succeeding — keeps us honest about #565's
            // "tunnel established" lie.
            completionHandler(PacketTunnelError.wireGuardKitNotLinked)
#endif
        } catch {
            os_log("decode providerConfiguration failed: %{public}@",
                   log: logger, type: .error,
                   String(describing: error))
            completionHandler(error)
        }
    }

    override func stopTunnel(
        with reason: NEProviderStopReason,
        completionHandler: @escaping () -> Void
    ) {
        os_log("stopTunnel reason=%{public}d", log: logger, type: .info, reason.rawValue)
#if canImport(WireGuardKit)
        adapter.stop { [weak self] error in
            if let error = error {
                os_log("WireGuardAdapter.stop error: %{public}@",
                       log: self?.logger ?? OSLog.default, type: .error,
                       String(describing: error))
            }
            completionHandler()
        }
#else
        completionHandler()
#endif
    }

    // ── IPC from the main app ────────────────────────────────────
    //
    // The main app sends JSON-encoded commands via
    // NETunnelProviderSession.sendProviderMessage(_:returnError:). We
    // decode + handle. Three commands recognised in v1:
    //   - "getStatus" — returns CONNECTED / CONNECTING / DISCONNECTED
    //   - "forceReconnect" — re-runs the WG handshake on the current peer
    //   - "setPeer" — re-pins to a new peer endpoint (used by #572
    //     roaming flow to switch providers without dropping the
    //     tunnel)

    override func handleAppMessage(
        _ messageData: Data,
        completionHandler: ((Data?) -> Void)?
    ) {
        guard let message = try? JSONDecoder().decode(IPCMessage.self, from: messageData) else {
            completionHandler?(nil)
            return
        }
        switch message.command {
        case .getStatus:
            // TODO(#568) wire to adapter state once WireGuardKit lands.
            let response = IPCResponse(status: "UNKNOWN")
            completionHandler?(try? JSONEncoder().encode(response))
        case .forceReconnect:
            // TODO(#568) call adapter.update with same config to retrigger handshake.
            completionHandler?(nil)
        case .setPeer:
            // TODO(#572) re-pin endpoint without re-deriving keys.
            completionHandler?(nil)
        }
    }
}

// ── IPC message shapes ────────────────────────────────────────────

private struct IPCMessage: Decodable {
    enum Command: String, Decodable {
        case getStatus
        case forceReconnect
        case setPeer
    }
    let command: Command
    let payload: [String: String]?
}

private struct IPCResponse: Encodable {
    let status: String
}

// ── Errors ────────────────────────────────────────────────────────

enum PacketTunnelError: Error, LocalizedError {
    case missingProviderConfiguration
    case malformedProviderConfiguration(reason: String)
    case wireGuardKitNotLinked

    var errorDescription: String? {
        switch self {
        case .missingProviderConfiguration:
            return "providerConfiguration was nil — main app must call setTunnelNetworkSettings before startTunnel"
        case .malformedProviderConfiguration(let reason):
            return "providerConfiguration malformed: \(reason)"
        case .wireGuardKitNotLinked:
            return "WireGuardKit Swift package not linked into the extension target (config plugin not finished wiring SwiftPM dep)"
        }
    }
}
