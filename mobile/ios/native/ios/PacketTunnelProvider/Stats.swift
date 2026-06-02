// Stats.swift — wire shape for the per-second stats loop that flows
// from the PacketTunnelProvider extension → main app via NE IPC.
//
// PacketTunnelProvider runs in a SEPARATE PROCESS from the main Expo
// app (iOS sandboxes NE processes hard — see PacketTunnelProvider.swift
// header). The only IPC channels are:
//   - NEVPNProtocol.providerConfiguration (configured BEFORE start)
//   - NETunnelProviderSession.sendProviderMessage / handleAppMessage
//   - NEProvider.displayMessage(_:) for one-off OS-level toasts
//
// For continuously-streaming stats we use the
// NETunnelProvider.setTunnelNetworkSettings + a separate JSON-over-
// sendProviderMessage callback. The main app subscribes via TunnelControl
// JS module's `onStatsUpdate(...)` event (src/index.ts) — the JS
// module emits each parsed Stats payload to JS listeners.
//
// Wire shape is intentionally narrow: bytes_sent, bytes_received,
// handshake_age_seconds, path_latency_ms. Track 4 (#591) will surface
// these in the UI.
//
// Refs #587. Pairs with WGTunnel.swift + PacketTunnelProvider.swift.

import Foundation
import WireGuardKit

/// Stats is the per-tick snapshot the stats loop emits to the main app.
/// Codable so the JS bridge gets a stable JSON shape.
struct Stats: Codable {
    let sessionID: String
    let bytesSent: UInt64
    let bytesReceived: UInt64
    /// Age (seconds) of the last successful WG handshake. -1 if no
    /// handshake has happened yet (tunnel just established).
    let handshakeAgeSeconds: Int64
    /// Best-effort path latency in milliseconds. WireGuardKit doesn't
    /// expose this directly; we approximate from the tx/rx packet
    /// timestamps. -1 if not yet measurable.
    let pathLatencyMs: Int32
    /// Wall-clock timestamp the stats were captured.
    let capturedAtUnixMs: Int64

    enum CodingKeys: String, CodingKey {
        case sessionID = "session_id"
        case bytesSent = "bytes_sent"
        case bytesReceived = "bytes_received"
        case handshakeAgeSeconds = "handshake_age_seconds"
        case pathLatencyMs = "path_latency_ms"
        case capturedAtUnixMs = "captured_at_unix_ms"
    }
}

/// StatsParser decodes the runtime configuration string that
/// WireGuardAdapter.getRuntimeConfiguration returns. The string is in
/// WG's standard "key=value\n" wireguard.conf format with extra
/// transfer-rx / transfer-tx / latest-handshake counters per peer.
///
/// Returns nil if the string can't be parsed at all — caller logs +
/// skips this tick.
enum StatsParser {
    static func parse(_ runtimeConfig: String, sessionID: String) -> Stats? {
        var rx: UInt64 = 0
        var tx: UInt64 = 0
        var handshakeUnix: Int64 = 0

        for rawLine in runtimeConfig.split(separator: "\n") {
            let line = rawLine.trimmingCharacters(in: .whitespaces)
            guard let eq = line.firstIndex(of: "=") else { continue }
            let key = String(line[..<eq])
            let value = String(line[line.index(after: eq)...])
            switch key {
            case "rx_bytes":
                rx = UInt64(value) ?? 0
            case "tx_bytes":
                tx = UInt64(value) ?? 0
            case "last_handshake_time_sec":
                // WG reports seconds since epoch. 0 = no handshake yet.
                handshakeUnix = Int64(value) ?? 0
            default:
                continue
            }
        }

        if rx == 0 && tx == 0 && handshakeUnix == 0 {
            // Empty / unparseable config — surface nil so caller can
            // distinguish "tunnel up but no traffic yet" from "parser
            // broke" (we treat both as no-update for now; future
            // refinement might split them).
            return nil
        }

        let nowMs = Int64(Date().timeIntervalSince1970 * 1000)
        let handshakeAge: Int64 = {
            guard handshakeUnix > 0 else { return -1 }
            let nowSec = nowMs / 1000
            let age = nowSec - handshakeUnix
            return age >= 0 ? age : 0
        }()

        return Stats(
            sessionID: sessionID,
            bytesSent: tx,
            bytesReceived: rx,
            handshakeAgeSeconds: handshakeAge,
            pathLatencyMs: -1, // Track 4 (#591) will fill from probe RTT
            capturedAtUnixMs: nowMs
        )
    }
}
