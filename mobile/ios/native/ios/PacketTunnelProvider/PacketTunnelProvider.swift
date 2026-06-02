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
// Flow (#587, post-WG-wire):
//   1. Main app generates customer WG keypair, stores private key in
//      App Group Keychain under a known label.
//   2. Main app calls coordinator POST /v1/vpn/sessions/mobile (#588),
//      gets a complete peer config: peer_public_key, peer_endpoint,
//      customer_inner_cidr, allowed_ips, dns_servers, session_id.
//   3. Main app sets NEVPNProtocol.providerConfiguration with the
//      full payload and calls connection.startVPNTunnel().
//   4. iOS launches THIS process, calls startTunnel(options:).
//   5. We read the config, read the private key from Keychain, build
//      a TunnelConfiguration (WGTunnel.swift), call
//      WireGuardAdapter.start.
//   6. NWPathMonitor observes the device network path; on a path
//      change we re-probe the region's top-3 providers and re-pin
//      the WG peer endpoint to the lowest-RTT survivor WITHOUT
//      dropping the tunnel session (#572 seamless roaming).
//   7. Every 1s we tick the stats loop: query adapter.getRuntimeConfiguration,
//      parse to a Stats struct, post to the main app via
//      NEProvider.displayMessage path (the main app's TunnelControl
//      module surfaces these as onStatsUpdate events to JS).
//
// Refs #568, #572, #587. Pairs with WGTunnel.swift + Stats.swift +
// the Expo config plugin `plugins/with-network-extension.ts`.

import Foundation
import Network
import NetworkExtension
import os.log
import WireGuardKit

@objc(PacketTunnelProvider)
final class PacketTunnelProvider: NEPacketTunnelProvider {

    private let logger = OSLog(subsystem: "io.iogrid.app.PacketTunnelProvider", category: "tunnel")

    /// WireGuardAdapter is the WireGuardKit driver — owns the tunnel
    /// lifecycle (start / stop / update) + exposes the runtime config
    /// string we parse for stats. Lazy so we only spin up the adapter
    /// on the first startTunnel call (matches the OS lifecycle).
    private lazy var adapter: WireGuardAdapter = {
        return WireGuardAdapter(with: self) { [weak self] logLevel, message in
            guard let self = self else { return }
            os_log("[wg] %{public}@", log: self.logger, type: .info, message)
        }
    }()

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
    // discards its result.
    private var pathMonitor: NWPathMonitor?
    private let pathMonitorQueue = DispatchQueue(label: "io.iogrid.app.PacketTunnelProvider.path")
    private var lastPath: Network.NWPath?
    private var currentRegion: String?
    private var currentPeerPublicKey: String?
    private var currentSessionConfig: SessionPeerConfig?
    private var probeGeneration: UInt64 = 0
    private let probeLock = NSLock()

    /// NEVPNStatusDidChange observer — held in a strong ref so we can
    /// removeObserver cleanly on stopTunnel (CONTRIBUTING #17 / #22).
    /// nil when no observer is active.
    private var statusObserver: NSObjectProtocol?

    // ── Stats loop (#587) ────────────────────────────────────────
    //
    // Timer-based — fires every `statsIntervalMs` while the tunnel
    // is up, queries adapter.getRuntimeConfiguration, parses into
    // a Stats struct, writes JSON to App Group UserDefaults. The
    // main-app TunnelControl module polls that store + emits each
    // tick as an `onStatsUpdate` event to JS.
    //
    // We use App Group UserDefaults (not sendProviderMessage) for
    // stats because:
    //   - displayMessage is for OS-level alerts, not data IPC
    //   - sendProviderMessage is a request/response (main-app driven)
    //   - App Group UserDefaults gives us a free shared-memory tier
    //     that the main app can subscribe to via KVO
    //   - The wire shape is JSON so the JS bridge can parse without
    //     a binary IPC handshake
    private var statsTimer: DispatchSourceTimer?
    private let statsQueue = DispatchQueue(label: "io.iogrid.app.PacketTunnelProvider.stats")
    private let statsIntervalMs: Int = 1_000
    private static let statsAppGroup    = "group.io.iogrid.app"
    private static let statsDefaultsKey = "io.iogrid.PacketTunnelProvider.stats.latest"

    // Roaming budget (#572 acceptance criterion):
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

        // Resolve the customer's WG private key. In production this
        // comes from the App Group Keychain (the main app stores it
        // under a deterministic label keyed off sessionId). For the
        // v1 bring-up we accept it inline from providerConfiguration
        // as a fallback — Track 1 (identity) replaces this with a
        // Keychain-only read once the keystore lands.
        let clientPrivateKey: PrivateKey
        if let pkString = providerConfig["clientPrivateKey"] as? String,
           let key = PrivateKey(base64Key: pkString) {
            clientPrivateKey = key
        } else if let key = readPrivateKeyFromAppGroupKeychain(providerConfig: providerConfig) {
            clientPrivateKey = key
        } else {
            os_log("client WG private key missing from both providerConfiguration and App Group Keychain",
                   log: logger, type: .error)
            completionHandler(PacketTunnelError.wireGuardStartFailed(reason: "private key not found"))
            return
        }

        // Decode the typed peer config.
        let sessionConfig: SessionPeerConfig
        do {
            sessionConfig = try WGTunnel.decode(
                providerConfiguration: providerConfig,
                clientPrivateKey: clientPrivateKey)
        } catch {
            os_log("providerConfiguration decode failed: %{public}@",
                   log: logger, type: .error, String(describing: error))
            completionHandler(PacketTunnelError.malformedProviderConfiguration(
                reason: error.localizedDescription))
            return
        }

        // Build the network settings + the TunnelConfiguration.
        guard let netSettings = WGTunnel.buildNetworkSettings(sessionConfig) else {
            os_log("buildNetworkSettings returned nil — inner CIDR malformed?",
                   log: logger, type: .error)
            completionHandler(PacketTunnelError.malformedProviderConfiguration(
                reason: "inner CIDR malformed"))
            return
        }
        let tunnelConfig: TunnelConfiguration
        do {
            tunnelConfig = try WGTunnel.buildTunnelConfiguration(sessionConfig)
        } catch {
            os_log("buildTunnelConfiguration failed: %{public}@",
                   log: logger, type: .error, String(describing: error))
            completionHandler(PacketTunnelError.wireGuardStartFailed(
                reason: error.localizedDescription))
            return
        }

        // Capture state needed by roaming + stats loops BEFORE the
        // potentially-slow adapter.start so an early NWPath update
        // doesn't race against a half-set state.
        self.currentRegion = providerConfig["region"] as? String
        self.currentPeerPublicKey = sessionConfig.peerPublicKey
        self.currentSessionConfig = sessionConfig

        setTunnelNetworkSettings(netSettings) { [weak self] settingsErr in
            guard let self = self else { return }
            if let settingsErr = settingsErr {
                os_log("setTunnelNetworkSettings failed: %{public}@",
                       log: self.logger, type: .error,
                       String(describing: settingsErr))
                completionHandler(PacketTunnelError.wireGuardStartFailed(
                    reason: "setTunnelNetworkSettings failed: \(settingsErr.localizedDescription)"))
                return
            }
            self.adapter.start(tunnelConfiguration: tunnelConfig) { [weak self] error in
                guard let self = self else { return }
                if let error = error {
                    os_log("WireGuardAdapter.start failed: %{public}@",
                           log: self.logger, type: .error,
                           String(describing: error))
                    completionHandler(PacketTunnelError.wireGuardStartFailed(
                        reason: String(describing: error)))
                    return
                }
                os_log("tunnel established session=%{public}@",
                       log: self.logger, type: .info,
                       sessionConfig.sessionID)
                self.startPathMonitor()
                self.startStatsLoop(sessionID: sessionConfig.sessionID)
                self.subscribeToStatusChanges()
                completionHandler(nil)
            }
        }
    }

    override func stopTunnel(
        with reason: NEProviderStopReason,
        completionHandler: @escaping () -> Void
    ) {
        os_log("stopTunnel reason=%{public}d", log: logger, type: .info, reason.rawValue)
        stopPathMonitor()
        stopStatsLoop()
        unsubscribeFromStatusChanges()
        // Post a session-end note to the coordinator so the
        // /heartbeat → terminate flow doesn't have to wait for a
        // stale timeout. Best-effort — we don't gate the stop on it.
        if let sess = currentSessionConfig {
            postSessionEnd(sessionID: sess.sessionID, reason: reason)
        }
        adapter.stop { [weak self] error in
            if let error = error {
                os_log("WireGuardAdapter.stop error: %{public}@",
                       log: self?.logger ?? OSLog.default, type: .error,
                       String(describing: error))
            }
            self?.currentSessionConfig = nil
            completionHandler()
        }
    }

    // ── IPC from the main app ────────────────────────────────────

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
            // Reflect WG adapter state — adapter.getRuntimeConfiguration
            // returns a non-nil non-empty string once the handshake has
            // completed, which is our connected signal.
            adapter.getRuntimeConfiguration { [weak self] runtime in
                let status: String
                if let r = runtime, !r.isEmpty {
                    // Probe for a recent handshake — without one the
                    // tunnel is up but not authenticated.
                    if r.contains("last_handshake_time_sec=0") {
                        status = "HANDSHAKING"
                    } else {
                        status = "CONNECTED"
                    }
                } else {
                    status = "DISCONNECTED"
                }
                _ = self
                let resp = IPCResponse(status: status)
                completionHandler?(try? JSONEncoder().encode(resp))
            }
        case .forceReconnect:
            // Re-apply the existing config to force a fresh handshake.
            if let cfg = currentSessionConfig,
               let tcfg = try? WGTunnel.buildTunnelConfiguration(cfg) {
                adapter.update(tunnelConfiguration: tcfg) { _ in }
            }
            completionHandler?(nil)
        case .setPeer:
            // #572 roaming path — re-pin endpoint without re-deriving
            // keys. The in-extension NWPathMonitor handler also calls
            // this codepath directly; the main app's setPeer is only
            // used for forced testing.
            completionHandler?(nil)
        }
    }

    // ── Roaming: NWPathMonitor + top-3 re-probe (#572) ───────────

    private func startPathMonitor() {
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
        defer { self.lastPath = newPath }
        guard let previous = lastPath else {
            os_log("initial path snapshot recorded: status=%{public}@",
                   log: logger, type: .info, "\(newPath.status)")
            return
        }
        guard pathDifferenceTriggersReprobe(previous: previous, new: newPath) else {
            return
        }

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

    private func pathDifferenceTriggersReprobe(previous: Network.NWPath, new: Network.NWPath) -> Bool {
        if previous.status != new.status { return true }
        let interestingTypes: [NWInterface.InterfaceType] = [.wifi, .cellular, .wiredEthernet, .other]
        for t in interestingTypes where previous.usesInterfaceType(t) != new.usesInterfaceType(t) {
            return true
        }
        if previous.supportsIPv4 != new.supportsIPv4 { return true }
        if previous.supportsIPv6 != new.supportsIPv6 { return true }
        if previous.isExpensive != new.isExpensive { return true }
        return false
    }

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
        // #577 MINOR fix: API base URL must be configurable so staging /
        // dev / QA builds don't accidentally hit prod. Read from the
        // providerConfiguration the main app handed us; fall back to
        // prod so existing v1.0 customers don't have to update their
        // saved profiles when this lands.
        let apiBase: String = {
            if let cfg = (self.protocolConfiguration as? NETunnelProviderProtocol)?
                .providerConfiguration,
               let raw = cfg["apiBaseUrl"] as? String,
               !raw.isEmpty {
                return raw.hasSuffix("/") ? String(raw.dropLast()) : raw
            }
            return "https://api.iogrid.org"
        }()
        guard let url = URL(string: "\(apiBase)/v1/vpn/regions/\(region)/providers?limit=3") else {
            completion(.failure(PacketTunnelError.malformedProviderConfiguration(reason: "invalid region")))
            return
        }
        var req = URLRequest(url: url)
        req.httpMethod = "GET"
        req.timeoutInterval = 0.4
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
                let envelope = try JSONDecoder().decode(ProvidersEnvelope.self, from: data)
                completion(.success(envelope.providers))
            } catch {
                completion(.failure(error))
            }
        }
        task.resume()
    }

    // ── UDP latency probe ────────────────────────────────────────

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

        // #577 MINOR 7 fix: enforce the COMBINED budget across
        // fetchTopProviders + probeCandidates rather than giving each
        // phase its own 500ms (which let the combined path drift to
        // ~1100ms even when the "1s total budget" comment claimed
        // otherwise). Compute remaining budget from the original
        // `started` timestamp and clamp to a 50ms floor — if we've
        // already burned the budget, give probes a brief window to
        // either return a fast LAN result or be discarded.
        let elapsedBeforeProbe = elapsedMillis(since: started)
        let remainingMs = max(50, probePhaseBudgetMs * 2 - elapsedBeforeProbe)
        let deadline = DispatchTime.now() + .milliseconds(remainingMs)
        let waitResult = probeGroup.wait(timeout: deadline)
        let elapsedMs = elapsedMillis(since: started)

        if waitResult == .timedOut {
            os_log("probe phase exceeded combined budget (remaining=%{public}dms, total=%{public}dms) — keeping current endpoint",
                   log: logger, type: .info, remainingMs, elapsedMs)
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
        pathMonitorQueue.asyncAfter(deadline: .now() + .milliseconds(perCandidateTimeoutMs)) {
            fire(nil)
        }
        conn.stateUpdateHandler = { state in
            switch state {
            case .ready:
                // #577 MINOR 5 fix: UDP .ready fires on local socket
                // bind regardless of network reachability, so timing
                // here measured socket setup not peer RTT. Instead
                // send the payload at .ready and START the RTT clock
                // there; record the actual elapsed time on the first
                // receiveMessage callback, which only fires when the
                // peer (or an intermediate router) actually responds.
                let payload = Data(repeating: 0, count: 4)
                let sendStarted = DispatchTime.now()
                conn.send(content: payload, completion: .contentProcessed { _ in })
                conn.receiveMessage { _, _, _, _ in
                    // Inbound = peer actually responded — this is
                    // a real round-trip number.
                    let rttMs = self.elapsedMillis(since: sendStarted)
                    fire(rttMs)
                }
                // Note: if the peer never responds, the asyncAfter
                // perCandidateTimeoutMs deadline above will fire(nil)
                // and mark this candidate as unreachable — exactly
                // the signal the picker needs.
            case .failed, .cancelled:
                fire(nil)
            default:
                break
            }
        }
        _ = started // kept for symmetry — overall start time logged elsewhere
        conn.start(queue: pathMonitorQueue)
    }

    // ── adapter.update endpoint re-pin ───────────────────────────

    private func repinEndpoint(to provider: ProviderCandidate, generation _: UInt64, started: DispatchTime) {
        guard let preferred = provider.preferredCandidate(),
              let newEndpoint = Endpoint(from: "\(preferred.address):\(preferred.port)") else {
            os_log("repin: malformed candidate %{public}@:%{public}d",
                   log: logger, type: .error,
                   provider.preferredCandidate()?.address ?? "?",
                   Int(provider.preferredCandidate()?.port ?? 0))
            return
        }
        guard let newConfig = buildSwappedConfig(currentEndpoint: newEndpoint,
                                                  newPeerPublicKey: provider.wgPublicKey) else {
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
            self.currentPeerPublicKey = provider.wgPublicKey
        }
    }

    /// Re-decode providerConfiguration into a TunnelConfiguration, then
    /// swap the peer endpoint + public key on the resulting peer[0]. We
    /// re-derive from providerConfiguration so the customer's
    /// addresses / DNS / private key stay consistent across re-pins.
    private func buildSwappedConfig(currentEndpoint: Endpoint, newPeerPublicKey: String) -> TunnelConfiguration? {
        guard let providerConfig = (protocolConfiguration as? NETunnelProviderProtocol)?
                .providerConfiguration,
              let cfg = currentSessionConfig else {
            os_log("buildSwappedConfig: no session config in memory — aborting", log: logger, type: .error)
            return nil
        }

        // Build from current session config (preserves client private key).
        // Override the peer fields with the swap targets.
        do {
            let base = try WGTunnel.buildTunnelConfiguration(cfg)
            var peers = base.peers
            guard !peers.isEmpty else {
                os_log("buildSwappedConfig: no peers in base config", log: logger, type: .error)
                return nil
            }
            guard let newKey = PublicKey(base64Key: newPeerPublicKey) else {
                os_log("buildSwappedConfig: invalid newPeerPublicKey", log: logger, type: .error)
                return nil
            }
            var peer = peers[0]
            peer.endpoint = currentEndpoint
            peer.publicKey = newKey
            peers[0] = peer
            _ = providerConfig
            return TunnelConfiguration(name: base.name, interface: base.interface, peers: peers)
        } catch {
            os_log("buildSwappedConfig: rebuild failed: %{public}@",
                   log: logger, type: .error, String(describing: error))
            return nil
        }
    }

    // ── Stats loop (#587) ────────────────────────────────────────

    private func startStatsLoop(sessionID: String) {
        stopStatsLoop() // defensive — never double-fire
        let timer = DispatchSource.makeTimerSource(queue: statsQueue)
        timer.schedule(deadline: .now() + .milliseconds(statsIntervalMs),
                       repeating: .milliseconds(statsIntervalMs))
        timer.setEventHandler { [weak self] in
            self?.tickStats(sessionID: sessionID)
        }
        timer.resume()
        statsTimer = timer
        os_log("stats loop started (interval=%{public}dms)",
               log: logger, type: .info, statsIntervalMs)
    }

    private func stopStatsLoop() {
        statsTimer?.cancel()
        statsTimer = nil
    }

    private func tickStats(sessionID: String) {
        adapter.getRuntimeConfiguration { [weak self] runtime in
            guard let self = self, let runtime = runtime else { return }
            guard let stats = StatsParser.parse(runtime, sessionID: sessionID) else { return }
            self.emitStats(stats)
        }
    }

    /// Encode the stats payload as JSON + write to App Group UserDefaults.
    /// The main-app TunnelControl module polls that key + emits each
    /// new tick as an `onStatsUpdate` event to JS (see TunnelControl.swift).
    private func emitStats(_ stats: Stats) {
        guard let json = try? JSONEncoder().encode(stats),
              let str = String(data: json, encoding: .utf8) else { return }
        guard let defaults = UserDefaults(suiteName: Self.statsAppGroup) else { return }
        defaults.set(str, forKey: Self.statsDefaultsKey)
        // synchronize() is deprecated but the App Group store needs an
        // explicit flush to be visible to the host process — the OS
        // doesn't write-back as aggressively as the standard suite.
        defaults.synchronize()
    }

    // ── Status observer (CONTRIBUTING #17/#22) ──────────────────

    /// Subscribe to NEVPNStatusDidChange so we can react to OS-driven
    /// teardowns (low-power mode, kill switch, user-toggled Settings
    /// VPN switch). The observer must be removed on stopTunnel — keep
    /// the reference in a strong property so removeObserver(self,...)
    /// has a non-nil handle.
    private func subscribeToStatusChanges() {
        guard statusObserver == nil else { return }
        statusObserver = NotificationCenter.default.addObserver(
            forName: .NEVPNStatusDidChange,
            object: nil,
            queue: .main
        ) { [weak self] notification in
            guard let self = self,
                  let conn = notification.object as? NEVPNConnection else { return }
            os_log("NEVPNStatusDidChange status=%{public}d",
                   log: self.logger, type: .info, conn.status.rawValue)
        }
    }

    private func unsubscribeFromStatusChanges() {
        if let obs = statusObserver {
            NotificationCenter.default.removeObserver(obs)
            statusObserver = nil
        }
    }

    // ── Session-end best-effort POST ─────────────────────────────

    /// Best-effort fire-and-forget POST to
    /// /v1/vpn/sessions/{id}/heartbeat with a final byte count so the
    /// coordinator's billing batcher closes out the session promptly
    /// instead of waiting for the staleness window.
    private func postSessionEnd(sessionID: String, reason: NEProviderStopReason) {
        adapter.getRuntimeConfiguration { [weak self] runtime in
            guard let self = self,
                  let runtime = runtime,
                  let stats = StatsParser.parse(runtime, sessionID: sessionID),
                  let url = URL(string: "https://api.iogrid.org/v1/vpn/sessions/\(sessionID)/heartbeat") else {
                return
            }
            var req = URLRequest(url: url)
            req.httpMethod = "POST"
            req.timeoutInterval = 1.0
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
            let body: [String: Any] = [
                "bytes_in":  stats.bytesReceived,
                "bytes_out": stats.bytesSent,
                "last_handshake_age_seconds": stats.handshakeAgeSeconds,
                "sent_at_unix_ms": Int64(Date().timeIntervalSince1970 * 1000),
                "stop_reason": reason.rawValue,
            ]
            req.httpBody = try? JSONSerialization.data(withJSONObject: body)
            URLSession.shared.dataTask(with: req) { _, _, err in
                if let err = err {
                    os_log("postSessionEnd error: %{public}@",
                           log: self.logger, type: .info,
                           String(describing: err))
                }
            }.resume()
        }
    }

    // ── App Group Keychain — client WG private key ───────────────

    /// Look up the customer's WG private key in the App Group Keychain
    /// under a label derived from the session id (or the customer id
    /// if the main app stashed it that way). Returns nil if absent —
    /// caller falls back to the providerConfiguration inline path.
    ///
    /// Track 1 (identity) will lock this down to keychain-only once
    /// the keystore lands; v1 keeps the inline fallback so the
    /// bring-up flow stays bootstrappable.
    private func readPrivateKeyFromAppGroupKeychain(providerConfig: [String: Any]) -> PrivateKey? {
        // We accept either an explicit label OR derive from sessionId.
        let label: String
        if let l = providerConfig["clientPrivateKeyLabel"] as? String, !l.isEmpty {
            label = l
        } else if let sid = providerConfig["sessionId"] as? String, !sid.isEmpty {
            label = "wg.client.\(sid)"
        } else {
            return nil
        }
        let query: [String: Any] = [
            kSecClass as String:        kSecClassGenericPassword,
            kSecAttrAccessGroup as String: "group.io.iogrid.app",
            kSecAttrLabel as String:    label,
            kSecReturnData as String:   true,
            kSecMatchLimit as String:   kSecMatchLimitOne,
        ]
        var item: AnyObject?
        let status = SecItemCopyMatching(query as CFDictionary, &item)
        guard status == errSecSuccess,
              let data = item as? Data,
              let str = String(data: data, encoding: .utf8),
              let key = PrivateKey(base64Key: str) else {
            return nil
        }
        return key
    }

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

private struct ProvidersEnvelope: Decodable {
    let providers: [ProviderCandidate]
}

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
    case wireGuardStartFailed(reason: String)

    var errorDescription: String? {
        switch self {
        case .missingProviderConfiguration:
            return "providerConfiguration was nil — main app must set NEVPNProtocol.providerConfiguration before startTunnel"
        case .malformedProviderConfiguration(let reason):
            return "providerConfiguration malformed: \(reason)"
        case .wireGuardStartFailed(let reason):
            return "WireGuardAdapter.start failed: \(reason)"
        }
    }
}
