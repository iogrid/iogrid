// WGTunnel.swift — bridge between the iogrid mobile providerConfiguration
// shape (#588: peer_public_key, peer_endpoint, customer_inner_cidr,
// allowed_ips, dns_servers, session_id) and WireGuardKit's
// TunnelConfiguration / InterfaceConfiguration / PeerConfiguration.
//
// Why a separate file: keeps the providerConfiguration → WireGuardKit
// adapter pure + unit-testable (no NetworkExtension import; only
// Foundation + WireGuardKit). The PacketTunnelProvider stays focused
// on lifecycle.
//
// Refs #587. Pairs with PacketTunnelProvider.swift + Stats.swift.

import Foundation
import Network
import NetworkExtension
import WireGuardKit

/// Errors produced while building a TunnelConfiguration from the
/// providerConfiguration dictionary that the main app handed us.
/// Each case carries a human-readable reason so the JS layer / OS
/// log surfaces actionable detail.
enum TunnelBuildError: Error, LocalizedError {
    case missingField(String)
    case malformedField(String, reason: String)
    case invalidKey(String)
    case invalidEndpoint(String)
    case invalidInnerCIDR(String)
    case invalidAllowedIPs(String)

    var errorDescription: String? {
        switch self {
        case .missingField(let f):
            return "providerConfiguration missing required field: \(f)"
        case .malformedField(let f, let reason):
            return "providerConfiguration field \(f): \(reason)"
        case .invalidKey(let raw):
            return "invalid WireGuard public key (base64): \(raw)"
        case .invalidEndpoint(let raw):
            return "invalid peer endpoint (expected ip:port): \(raw)"
        case .invalidInnerCIDR(let raw):
            return "invalid customer inner CIDR (expected ipv4/prefix): \(raw)"
        case .invalidAllowedIPs(let raw):
            return "invalid allowed IPs CIDR: \(raw)"
        }
    }
}

/// SessionPeerConfig is the in-memory typed view of providerConfiguration.
/// We decode once into this struct so the rest of the extension treats
/// the wire shape as opaque.
struct SessionPeerConfig {
    let sessionID: String
    let peerPublicKey: String   // base64
    let peerEndpoint: String    // "ip:port"
    let innerCIDR: String       // "10.66.X.Y/32"
    let allowedIPs: [String]    // ["0.0.0.0/0"] for full-tunnel
    let dnsServers: [String]    // ["1.1.1.1", "1.0.0.1"]
    let clientPrivateKey: PrivateKey
}

/// MTU constant — 1280 is the spec-mandated minimum IPv6 PMTU and the
/// safest choice for cellular roaming where path MTU discovery is
/// often broken end-to-end. Matches WireGuardKit's default + the #587
/// DoD ("MTU 1280").
enum TunnelDefaults {
    static let mtu: Int = 1280
    static let dnsServers: [String] = ["1.1.1.1", "1.0.0.1"]
}

/// WGTunnel encapsulates the providerConfiguration → WireGuardKit
/// construction. Static methods only — no per-instance state.
enum WGTunnel {

    /// Decode the NETunnelProviderProtocol.providerConfiguration dict
    /// into a typed SessionPeerConfig. Throws TunnelBuildError with a
    /// reason string on any malformed field.
    static func decode(providerConfiguration cfg: [String: Any],
                       clientPrivateKey: PrivateKey) throws -> SessionPeerConfig {

        guard let sessionID = stringField(cfg, key: "sessionId"), !sessionID.isEmpty else {
            throw TunnelBuildError.missingField("sessionId")
        }
        guard let peerKey = stringField(cfg, key: "peerPublicKey"), !peerKey.isEmpty else {
            throw TunnelBuildError.missingField("peerPublicKey")
        }
        guard let endpoint = stringField(cfg, key: "peerEndpoint"), !endpoint.isEmpty else {
            throw TunnelBuildError.missingField("peerEndpoint")
        }
        guard let innerCIDR = stringField(cfg, key: "customerInnerCIDR"), !innerCIDR.isEmpty else {
            throw TunnelBuildError.missingField("customerInnerCIDR")
        }
        // allowedIPs may be a comma-separated string OR an array of strings.
        let allowedIPs = stringArrayField(cfg, key: "allowedIPs", default: ["0.0.0.0/0"])
        // dns_servers may be a comma-separated string OR an array; default
        // to Cloudflare per #587 DoD.
        let dns = stringArrayField(cfg, key: "dnsServers", default: TunnelDefaults.dnsServers)

        return SessionPeerConfig(
            sessionID: sessionID,
            peerPublicKey: peerKey,
            peerEndpoint: endpoint,
            innerCIDR: innerCIDR,
            allowedIPs: allowedIPs,
            dnsServers: dns,
            clientPrivateKey: clientPrivateKey
        )
    }

    /// Build a WireGuardKit TunnelConfiguration from a SessionPeerConfig.
    /// Throws on any malformed field.
    static func buildTunnelConfiguration(_ cfg: SessionPeerConfig) throws -> TunnelConfiguration {
        // Interface: inner CIDR + DNS + client private key
        var interfaceConfig = InterfaceConfiguration(privateKey: cfg.clientPrivateKey)

        // The InterfaceConfiguration's addresses array carries the
        // tunnel-side IPv4 (the inner address we just got from vpn-svc).
        guard let innerRange = IPAddressRange(from: cfg.innerCIDR) else {
            throw TunnelBuildError.invalidInnerCIDR(cfg.innerCIDR)
        }
        interfaceConfig.addresses = [innerRange]
        interfaceConfig.mtu = UInt16(TunnelDefaults.mtu)
        interfaceConfig.dns = cfg.dnsServers.compactMap { DNSServer(from: $0) }

        // Peer: public key + endpoint + allowed IPs
        guard let peerPub = PublicKey(base64Key: cfg.peerPublicKey) else {
            throw TunnelBuildError.invalidKey(cfg.peerPublicKey)
        }
        guard let endpoint = Endpoint(from: cfg.peerEndpoint) else {
            throw TunnelBuildError.invalidEndpoint(cfg.peerEndpoint)
        }
        var peer = PeerConfiguration(publicKey: peerPub)
        peer.endpoint = endpoint
        peer.allowedIPs = cfg.allowedIPs.compactMap { IPAddressRange(from: $0) }
        if peer.allowedIPs.isEmpty {
            throw TunnelBuildError.invalidAllowedIPs(cfg.allowedIPs.joined(separator: ","))
        }
        // 25-second keepalive matches the WG default for NAT punching;
        // cellular networks aggressively reap UDP flows < 60s idle.
        peer.persistentKeepAlive = 25

        return TunnelConfiguration(
            name: "iogrid-\(cfg.sessionID)",
            interface: interfaceConfig,
            peers: [peer]
        )
    }

    /// Build NEPacketTunnelNetworkSettings for the same peer config.
    /// WireGuardAdapter takes care of most of this internally, but
    /// PacketTunnelProvider needs to know the inner address for the
    /// initial setTunnelNetworkSettings call (the OS uses these to
    /// configure the utun interface in the host process).
    ///
    /// Returns nil if the inner CIDR is malformed — caller fails the
    /// tunnel start path.
    static func buildNetworkSettings(_ cfg: SessionPeerConfig) -> NEPacketTunnelNetworkSettings? {
        let parts = cfg.innerCIDR.split(separator: "/")
        guard parts.count == 2 else { return nil }
        let host = String(parts[0])
        let prefix = Int(parts[1]) ?? 32

        let settings = NEPacketTunnelNetworkSettings(tunnelRemoteAddress: cfg.peerEndpoint)
        let ipv4 = NEIPv4Settings(addresses: [host], subnetMasks: [Self.subnetMask(for: prefix)])
        ipv4.includedRoutes = [NEIPv4Route.default()]  // full-tunnel
        settings.ipv4Settings = ipv4

        let dns = NEDNSSettings(servers: cfg.dnsServers)
        // matchDomains = [""] forces all DNS through the tunnel, which is
        // what we want for VPN (otherwise iOS prefers system resolvers
        // for queries that don't match a listed domain).
        dns.matchDomains = [""]
        settings.dnsSettings = dns

        settings.mtu = NSNumber(value: TunnelDefaults.mtu)
        return settings
    }

    /// Dotted-quad subnet mask for an IPv4 prefix length (0-32).
    /// Returns "255.255.255.255" for any out-of-range value (safest /32
    /// fallback that won't accidentally widen the route).
    static func subnetMask(for prefix: Int) -> String {
        guard prefix >= 0 && prefix <= 32 else { return "255.255.255.255" }
        let mask = prefix == 0 ? UInt32(0) : ~UInt32(0) << (32 - prefix)
        let a = (mask >> 24) & 0xff
        let b = (mask >> 16) & 0xff
        let c = (mask >> 8) & 0xff
        let d = mask & 0xff
        return "\(a).\(b).\(c).\(d)"
    }

    // ── Helpers ────────────────────────────────────────────────────

    private static func stringField(_ cfg: [String: Any], key: String) -> String? {
        return cfg[key] as? String
    }

    /// Decode `key` as either a String (comma-separated) or [String].
    /// Empty/missing returns the supplied default.
    private static func stringArrayField(_ cfg: [String: Any], key: String, default fallback: [String]) -> [String] {
        if let arr = cfg[key] as? [String], !arr.isEmpty {
            return arr
        }
        if let str = cfg[key] as? String, !str.isEmpty {
            return str.split(separator: ",").map {
                $0.trimmingCharacters(in: .whitespaces)
            }.filter { !$0.isEmpty }
        }
        return fallback
    }
}
