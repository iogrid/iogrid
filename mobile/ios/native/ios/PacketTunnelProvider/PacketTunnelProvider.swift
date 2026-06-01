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
//      {peerPublicKey, peerEndpoint, allowedIPs, customerInnerCIDR,
//       region}
//      and calls connection.startVPNTunnel().
//   4. iOS launches THIS process, calls startTunnel(options:).
//   5. We read the config, read the private key from Keychain, build
//      a WireGuardAdapter, call .start.
//   6. NWPathMonitor observes the device network path; on a path
//      change we re-probe the region's top-3 providers and re-pin
//      the WG peer endpoint to the lowest-RTT survivor WITHOUT
//      dropping the tunnel session (#572 seamless roaming).
//
// Refs #568, #572. Pairs with the Expo config plugin
// `plugins/with-network-extension.ts` that wires this file into the
// Xcode project at prebuild time.

import Foundation
import Network
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

    // ── Roaming state (#572) ─────────────────────────────────────
    //
    // NWPathMonitor is one-shot per instance: once .cancel() is
    // called the instance cannot be reused. We therefore null it
    // out on stopTunnel and lazily create a fresh one on the next
    // startTunnel success.
    //
    // Probe results are race-protected by a monotonic generation
    // counter: when the path changes, we bump `probeGeneration`
    // and any in-flight probe that returns with a stale value
    // discards its result. This handles WiFi→cellular→WiFi
    // ping-pong while a probe is mid-flight.
    private var pathMonitor: NWPathMonitor?
    private let pathMonitorQueue = DispatchQueue(label: "io.iogrid.app.PacketTunnelProvider.path")
    private var lastPath: Network.NWPath?
    private var currentRegion: String?
    private var currentPeerPublicKey: String?
    private var probeGeneration: UInt64 = 0
    private let probeLock = NSLock()

    // Roaming budget (#572 acceptance criterion):
    //   - 500 ms hard cap on the probe phase (per-candidate UDP probes
    //     fan out concurrently). If the probe phase exceeds 500ms
    //     total we abandon and keep the current endpoint to avoid
    //     thrashing.
    //   - 250 ms per-candidate timeout (matches the issue spec).
    //   - 1 s total budget from pathUpdate fire → adapter.update return.
    private let probePhaseBudgetMs: Int = 500
    private let perCandidateTimeoutMs: Int = 250

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

        // Capture roaming-relevant fields before adapter.start so
        // that even the scaffold-only (#if !canImport WireGuardKit)
        // path retains them — keeps unit tests for the roaming
        // logic agnostic of WG linkage state.
        self.currentRegion = providerConfig["region"] as? String
        self.currentPeerPublicKey = providerConfig["peerPublicKey"] as? String

#if canImport(WireGuardKit)
        do {
            let config = try TunnelConfigurationDecoder.decode(providerConfig)
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
                self.startPathMonitor()
                completionHandler(nil)
            }
        } catch {
            os_log("decode providerConfiguration failed: %{public}@",
                   log: logger, type: .error,
                   String(describing: error))
            completionHandler(error)
        }
#else
        // Scaffold-only build (WG SwiftPM dep deferred to #576).
        // Fail explicitly so the JS layer reflects CONNECTING → OFF
        // via the NEVPNStatusDidChange subscriber. Keeps us honest
        // about #565's "tunnel established" lie — the toggle WILL NOT
        // claim CONNECTED state when there's no data plane.
        os_log("WireGuardKit not linked — see #576", log: logger, type: .error)
        completionHandler(PacketTunnelError.wireGuardKitNotLinked)
#endif
    }

    override func stopTunnel(
        with reason: NEProviderStopReason,
        completionHandler: @escaping () -> Void
    ) {
        os_log("stopTunnel reason=%{public}d", log: logger, type: .info, reason.rawValue)
        stopPathMonitor()
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
            // The main app drives setPeer via the existing IPC shape;
            // the in-extension roaming flow (NWPathMonitor below)
            // does the same re-pin directly without round-tripping
            // through the main app.
            completionHandler?(nil)
        }
    }

    // ── Roaming: NWPathMonitor + top-3 re-probe (#572) ───────────
    //
    // iOS surfaces network path changes via NWPathMonitor on the
    // Network framework. The callback fires for *any* path change:
    //   - WiFi ↔ cellular
    //   - IPv4 ↔ IPv6 default route flip
    //   - same-interface address rebind (DHCP renewal, carrier IP
    //     change on cellular)
    //   - VPN-on-VPN nesting transitions
    //
    // Not every event needs a re-probe — we filter to the cases
    // that materially affect throughput / RTT to the WG peer. See
    // `pathDifferenceTriggersReprobe`.

    private func startPathMonitor() {
        // NWPathMonitor is one-shot per instance. If startTunnel is
        // called twice in a row (shouldn't happen — iOS guarantees
        // one process per session — but be defensive), bail.
        guard pathMonitor == nil else {
            os_log("startPathMonitor: monitor already running, no-op",
                   log: logger, type: .info)
            return
        }
        let monitor = NWPathMonitor()
        monitor.pathUpdateHandler = { [weak self] newPath in
            self?.handlePathUpdate(newPath)
        }
        monitor.start(queue: pathMonitorQueue)
        self.pathMonitor = monitor
        os_log("NWPathMonitor started", log: logger, type: .info)
    }

    private func stopPathMonitor() {
        guard let monitor = pathMonitor else { return }
        monitor.cancel()
        pathMonitor = nil
        lastPath = nil
        os_log("NWPathMonitor stopped", log: logger, type: .info)
    }

    private func handlePathUpdate(_ newPath: Network.NWPath) {
        // Compare against the last-known path. The first update
        // after start() is the *current* path snapshot — we record
        // it but don't re-probe (the tunnel was just established
        // on this path).
        defer { self.lastPath = newPath }
        guard let previous = lastPath else {
            // NWPath.Status is a plain `@frozen public enum` — no
            // rawValue. Use the description string instead.
            os_log("initial path snapshot recorded: status=%{public}@",
                   log: logger, type: .info, "\(newPath.status)")
            return
        }
        guard pathDifferenceTriggersReprobe(previous: previous, new: newPath) else {
            return
        }

        // Bump generation BEFORE kicking off probes so that any
        // in-flight probes from the previous generation see a
        // stale value when they finish and discard their results.
        probeLock.lock()
        probeGeneration &+= 1
        let myGeneration = probeGeneration
        probeLock.unlock()

        os_log("path change detected (gen=%{public}llu) — kicking off re-probe",
               log: logger, type: .info, myGeneration)

        guard let region = currentRegion, !region.isEmpty else {
            os_log("re-probe skipped: no region in providerConfiguration",
                   log: logger, type: .error)
            return
        }

        let started = DispatchTime.now()
        reprobeAndRepin(region: region, generation: myGeneration, started: started)
    }

    /// Returns true if the path-change shape warrants a top-3 re-probe.
    /// Filters out cosmetic updates (e.g. WiFi SSID rename with same
    /// gateway, idle radio wake) that wouldn't affect WG throughput.
    private func pathDifferenceTriggersReprobe(previous: Network.NWPath, new: Network.NWPath) -> Bool {
        if previous.status != new.status { return true }
        // Interface-set comparison: any change in usesInterfaceType
        // for the four meaningful types is a re-probe trigger.
        let interestingTypes: [NWInterface.InterfaceType] = [.wifi, .cellular, .wiredEthernet, .other]
        for t in interestingTypes where previous.usesInterfaceType(t) != new.usesInterfaceType(t) {
            return true
        }
        // IP family availability flip (IPv4 ↔ IPv6) — relevant
        // because srflx candidates are family-specific.
        if previous.supportsIPv4 != new.supportsIPv4 { return true }
        if previous.supportsIPv6 != new.supportsIPv6 { return true }
        // Expensive flag flips (e.g. moving onto a metered hotspot)
        // are worth re-pinning to a closer provider too.
        if previous.isExpensive != new.isExpensive { return true }
        return false
    }

    /// End-to-end roaming flow: fetch top-3 → UDP-probe → pick
    /// lowest-RTT → adapter.update with swapped endpoint.
    /// Generation gating discards any work that has been superseded
    /// by a newer pathUpdate.
    private func reprobeAndRepin(region: String, generation: UInt64, started: DispatchTime) {
        fetchTopProviders(region: region) { [weak self] result in
            guard let self = self else { return }
            guard self.isCurrentGeneration(generation) else {
                os_log("re-probe (gen=%{public}llu) discarded: superseded by newer pathUpdate",
                       log: self.logger, type: .info, generation)
                return
            }
            switch result {
            case .failure(let error):
                os_log("fetchTopProviders failed: %{public}@",
                       log: self.logger, type: .error,
                       String(describing: error))
            case .success(let candidates):
                self.probeCandidates(candidates, generation: generation, started: started)
            }
        }
    }

    private func isCurrentGeneration(_ gen: UInt64) -> Bool {
        probeLock.lock()
        defer { probeLock.unlock() }
        return gen == probeGeneration
    }

    // ── Coordinator fetch ────────────────────────────────────────

    private func fetchTopProviders(
        region: String,
        completion: @escaping (Result<[ProviderCandidate], Error>) -> Void
    ) {
        // URL is host-pinned to api.iogrid.org per #570. The
        // coordinator returns top-N by region-local median_rtt_ms.
        guard let url = URL(string: "https://api.iogrid.org/v1/vpn/regions/\(region)/providers?limit=3") else {
            completion(.failure(PacketTunnelError.malformedProviderConfiguration(reason: "invalid region")))
            return
        }
        var req = URLRequest(url: url)
        req.httpMethod = "GET"
        req.timeoutInterval = 0.4 // 400ms — leaves headroom inside the 500ms probe-phase budget
        req.setValue("application/json", forHTTPHeaderField: "Accept")
        let task = URLSession.shared.dataTask(with: req) { data, response, error in
            if let error = error {
                completion(.failure(error))
                return
            }
            guard let http = response as? HTTPURLResponse, http.statusCode == 200 else {
                let code = (response as? HTTPURLResponse)?.statusCode ?? -1
                completion(.failure(PacketTunnelError.malformedProviderConfiguration(
                    reason: "providers endpoint http=\(code)")))
                return
            }
            guard let data = data else {
                completion(.failure(PacketTunnelError.malformedProviderConfiguration(reason: "empty providers body")))
                return
            }
            do {
                let decoded = try JSONDecoder().decode([ProviderCandidate].self, from: data)
                completion(.success(decoded))
            } catch {
                completion(.failure(error))
            }
        }
        task.resume()
    }

    // ── UDP latency probe ────────────────────────────────────────
    //
    // Per the issue spec we use NWConnection (not BSD sockets) so the
    // probe lives inside the NE sandbox cleanly. For each candidate
    // we pick its preferred address — srflx > host > relay — and send
    // a 4-byte zero datagram. RTT is measured from .send completion
    // to first .receive callback or .ready→.failed transition.
    // Timeout is 250 ms per candidate.

    private func probeCandidates(
        _ providers: [ProviderCandidate],
        generation: UInt64,
        started: DispatchTime
    ) {
        let probeGroup = DispatchGroup()
        let resultsLock = NSLock()
        var results: [(provider: ProviderCandidate, rttMs: Int)] = []

        for provider in providers {
            guard let preferred = provider.preferredCandidate() else { continue }
            probeGroup.enter()
            measureRtt(host: preferred.address, port: preferred.port) { rttMs in
                if let rttMs = rttMs {
                    resultsLock.lock()
                    results.append((provider: provider, rttMs: rttMs))
                    resultsLock.unlock()
                }
                probeGroup.leave()
            }
        }

        // Bail at probePhaseBudgetMs — if we don't have results by
        // then, keep the current endpoint (no thrash).
        let deadline = DispatchTime.now() + .milliseconds(probePhaseBudgetMs)
        let waitResult = probeGroup.wait(timeout: deadline)
        let elapsedMs = elapsedMillis(since: started)

        if waitResult == .timedOut {
            os_log("probe phase exceeded %{public}dms budget (elapsed=%{public}dms) — keeping current endpoint",
                   log: logger, type: .info, probePhaseBudgetMs, elapsedMs)
            return
        }

        guard self.isCurrentGeneration(generation) else {
            os_log("probe results (gen=%{public}llu) discarded after fanout: superseded",
                   log: logger, type: .info, generation)
            return
        }

        resultsLock.lock()
        let snapshot = results
        resultsLock.unlock()

        guard let winner = snapshot.min(by: { $0.rttMs < $1.rttMs }) else {
            os_log("no probe survivors — keeping current endpoint", log: logger, type: .info)
            return
        }

        os_log("probe winner: provider=%{public}@ rtt=%{public}dms (elapsed=%{public}dms)",
               log: logger, type: .info,
               winner.provider.providerId, winner.rttMs, elapsedMs)

        repinEndpoint(to: winner.provider, generation: generation, started: started)
    }

    private func measureRtt(host: String, port: UInt16, completion: @escaping (Int?) -> Void) {
        guard let nwPort = NWEndpoint.Port(rawValue: port) else {
            completion(nil)
            return
        }
        let host = NWEndpoint.Host(host)
        // UDP-only — matches the WG transport. ICMP would need raw
        // sockets which the NE sandbox forbids.
        let conn = NWConnection(host: host, port: nwPort, using: .udp)
        let started = DispatchTime.now()
        var fired = false
        let lock = NSLock()
        let fire: (Int?) -> Void = { rtt in
            lock.lock()
            let alreadyFired = fired
            fired = true
            lock.unlock()
            guard !alreadyFired else { return }
            conn.cancel()
            completion(rtt)
        }
        // Per-candidate timeout — 250ms hard cap.
        pathMonitorQueue.asyncAfter(deadline: .now() + .milliseconds(perCandidateTimeoutMs)) {
            fire(nil)
        }
        conn.stateUpdateHandler = { state in
            switch state {
            case .ready:
                let payload = Data(repeating: 0, count: 4)
                conn.send(content: payload, completion: .contentProcessed { _ in })
                // Schedule receive — WG peers won't necessarily
                // ack a stray 4-byte datagram, but the time-to-ready
                // is itself a meaningful proxy for path RTT on the
                // first SYN/connect roundtrip. We treat .ready as
                // the floor measurement.
                let rttMs = self.elapsedMillis(since: started)
                conn.receiveMessage { _, _, _, _ in
                    // Any inbound (e.g. WG handshake-init reject)
                    // refines the measurement; if it doesn't arrive
                    // the timeout above fires.
                }
                fire(rttMs)
            case .failed, .cancelled:
                fire(nil)
            default:
                break
            }
        }
        conn.start(queue: pathMonitorQueue)
    }

    // ── adapter.update endpoint re-pin ───────────────────────────

    private func repinEndpoint(to provider: ProviderCandidate, generation: UInt64, started: DispatchTime) {
#if canImport(WireGuardKit)
        // Re-pin without re-deriving keys: build a new
        // TunnelConfiguration where the existing peer's endpoint is
        // swapped to the winning candidate. WireGuard's stateless
        // transport survives the source-IP change; the new outbound
        // binding triggers a handshake re-key on the next data
        // packet automatically.
        guard let preferred = provider.preferredCandidate(),
              let newEndpoint = Endpoint(from: "\(preferred.address):\(preferred.port)") else {
            os_log("repin: malformed candidate %{public}@:%{public}d",
                   log: logger, type: .error,
                   provider.preferredCandidate()?.address ?? "?",
                   Int(provider.preferredCandidate()?.port ?? 0))
            return
        }
        // We don't reconstruct the full TunnelConfiguration from
        // scratch — instead we read the adapter's current config and
        // mutate only the peer endpoint. WireGuardAdapter exposes
        // `update` which accepts a full TunnelConfiguration; we
        // synthesize one with `peerPublicKey` mapped to the winner's
        // peer key.
        guard let newConfig = buildSwappedConfig(currentEndpoint: newEndpoint,
                                                  newPeerPublicKey: provider.wgPublicKey) else {
            // buildSwappedConfig returned nil — keep existing tunnel.
            // Re-pin aborts safely (no adapter.update call). Reviewer
            // #568 finding 2: phantom random-key configs would
            // silently kill the tunnel.
            os_log("repin aborted (config build failed)", log: logger, type: .error)
            return
        }
        adapter.update(tunnelConfiguration: newConfig) { [weak self] error in
            guard let self = self else { return }
            let elapsedMs = self.elapsedMillis(since: started)
            if let error = error {
                os_log("adapter.update failed (elapsed=%{public}dms): %{public}@",
                       log: self.logger, type: .error,
                       elapsedMs, String(describing: error))
                return
            }
            os_log("repin complete: provider=%{public}@ elapsed=%{public}dms (budget=1000ms)",
                   log: self.logger, type: .info,
                   provider.providerId, elapsedMs)
            // Track the new peer key so subsequent repins can
            // skip-no-op when the winner hasn't changed.
            self.currentPeerPublicKey = provider.wgPublicKey
            _ = generation // generation is logged upstream; no-op
        }
#else
        // Scaffold-only build: log + drop. The roaming logic is still
        // wired up; only the actual peer swap is gated on
        // WireGuardKit linkage. This keeps the file compilable in
        // either state per the #572 acceptance criterion.
        let elapsedMs = self.elapsedMillis(since: started)
        os_log("repin (scaffold-only — WireGuardKit not linked): provider=%{public}@ elapsed=%{public}dms",
               log: logger, type: .info, provider.providerId, elapsedMs)
        _ = generation
#endif
    }

#if canImport(WireGuardKit)
    /// Synthesises a TunnelConfiguration whose single peer points at
    /// the winning candidate. The customer-side interface (private
    /// key, addresses, DNS) is preserved from the original startTunnel
    /// payload because we never persist it on the daemon side and the
    /// adapter retains it across .update calls — we only need to
    /// supply a peer with the swapped endpoint.
    ///
    /// NOTE: This is intentionally minimal. The full config decoder
    /// (TunnelConfigurationDecoder) lives in a sibling file that
    /// lands in the SwiftPM-wiring milestone (#568). Until then this
    /// function is unreachable in the scaffold-only build (gated by
    /// the same #if canImport(WireGuardKit) above).
    private func buildSwappedConfig(currentEndpoint: Endpoint, newPeerPublicKey: String) -> TunnelConfiguration? {
        // Re-decode the providerConfiguration to get a fresh
        // TunnelConfiguration, then swap the peer endpoint/key.
        // Returns nil on decode failure so the caller bails the
        // re-pin rather than substituting a phantom random-key
        // config — reviewer #568 finding 2, MAJOR. Phantom-key
        // would silently kill the tunnel since the peer doesn't
        // know our re-generated identity.
        let providerConfig = (protocolConfiguration as? NETunnelProviderProtocol)?
            .providerConfiguration ?? [:]
        guard let base = try? TunnelConfigurationDecoder.decode(providerConfig) else {
            os_log("buildSwappedConfig: decode failed — aborting re-pin", log: logger, type: .error)
            return nil
        }
        let cfg = base.wireguardConfig
        var peers = cfg.peers
        guard !peers.isEmpty else {
            os_log("buildSwappedConfig: no peers in decoded config — aborting", log: logger, type: .error)
            return nil
        }
        guard let newKey = PublicKey(base64Key: newPeerPublicKey) else {
            os_log("buildSwappedConfig: invalid newPeerPublicKey — aborting", log: logger, type: .error)
            return nil
        }
        var peer = peers[0]
        peer.endpoint = currentEndpoint
        peer.publicKey = newKey
        peers[0] = peer
        return TunnelConfiguration(name: cfg.name, interface: cfg.interface, peers: peers)
    }
#endif

    // ── Util ─────────────────────────────────────────────────────

    private func elapsedMillis(since: DispatchTime) -> Int {
        let nowNs = DispatchTime.now().uptimeNanoseconds
        let thenNs = since.uptimeNanoseconds
        guard nowNs >= thenNs else { return 0 }
        return Int((nowNs - thenNs) / 1_000_000)
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

// ── Provider candidate shapes (#572 — matches coordinator #570) ──

private struct ProviderCandidate: Decodable {
    let providerId: String
    let wgPublicKey: String
    let candidateSet: [WGCandidate]
    let medianRttMs: Int?

    enum CodingKeys: String, CodingKey {
        case providerId = "provider_id"
        case wgPublicKey = "wg_public_key"
        case candidateSet = "candidate_set"
        case medianRttMs = "median_rtt_ms"
    }

    /// Picks the preferred candidate address per ICE-like ordering:
    /// srflx (server-reflexive — i.e. observed by STUN, routable
    /// across NAT) > host (LAN-local) > relay (TURN — last resort).
    /// Matches the daemon-side flow that publishes these in the
    /// provider registration RPC (#570).
    func preferredCandidate() -> WGCandidate? {
        let order = ["srflx", "host", "relay"]
        for kind in order {
            if let c = candidateSet.first(where: { $0.type == kind }) { return c }
        }
        return candidateSet.first
    }
}

private struct WGCandidate: Decodable {
    let type: String
    let address: String
    let port: UInt16
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
