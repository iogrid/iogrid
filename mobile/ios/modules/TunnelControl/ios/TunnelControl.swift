// TunnelControl — Expo native module bridging the JS layer to
// NETunnelProviderManager. The JS app calls `startTunnel(config)` and
// this module:
//   1. Loads (or creates) the saved NETunnelProviderManager preference
//   2. Configures it with the customer/peer details from JS
//   3. Saves to system VPN preferences (triggers iOS "Allow VPN
//      configuration" sheet on first run)
//   4. Calls connection.startVPNTunnel()
//
// The PacketTunnelProvider extension (mobile/ios/native/ios/PacketTunnelProvider/)
// then receives this config + starts the WireGuard tunnel + the
// roaming NWPathMonitor (#572).
//
// Refs #568. Pairs with the JS wrapper at src/index.ts.

import CryptoKit
import ExpoModulesCore
import NetworkExtension

public class TunnelControlModule: Module {
  /// PacketTunnelProvider extension bundle identifier — must match the
  /// EXTENSION_BUNDLE_ID constant in
  /// mobile/ios/scripts/add-network-extension-target.rb. If these
  /// drift, NETunnelProviderManager.saveToPreferences silently fails
  /// (the OS won't load an extension whose bundle ID doesn't match
  /// what's in PreferencesController). One source of truth as a
  /// static constant on this module, referenced everywhere below.
  /// (#577 MINOR 2)
  public static let extensionBundleIdentifier = "io.iogrid.app.PacketTunnelProvider"

  /// Holds the strong reference to the NEVPNStatusDidChange observer
  /// so removeObserver(_:) on OnStopObserving has a valid handle to
  /// release. Setting an observer with addObserver(forName:...) returns
  /// an opaque NSObjectProtocol; passing `self` to removeObserver
  /// wouldn't actually deregister this closure-based observer.
  /// (CONTRIBUTING gotcha #17 / #22.)
  private var statusObserver: NSObjectProtocol?

  /// Stats poll timer — when JS has at least one onStatsUpdate listener
  /// we poll the App Group UserDefaults every `statsPollIntervalMs` and
  /// emit each new tick. nil when no JS listeners (saves battery during
  /// idle screens that don't render stats).
  private var statsTimer: DispatchSourceTimer?
  private var lastStatsCapturedAtUnixMs: Int64 = 0
  private static let statsAppGroup    = "group.io.iogrid.app"
  private static let statsDefaultsKey = "io.iogrid.PacketTunnelProvider.stats.latest"
  private static let statsPollIntervalMs: Int = 1_000

  /// App-Group UserDefaults keys for the device's persistent WireGuard
  /// keypair. The keypair is generated ONCE per install (here, in the
  /// app process) and shared with the PacketTunnelProvider extension via
  /// the App Group, so the device's public key is stable across sessions
  /// and the app can hand it to vpn-svc at session-request time. (#701:
  /// before this, the app sent a stub public key and started the tunnel
  /// with an EMPTY peerPublicKey — the extension's WGTunnel build then
  /// threw `missingField("peerPublicKey")`, so the tunnel never resolved
  /// a peer. The provider also never learned the device key, so the WG
  /// handshake could not complete.)
  private static let devicePrivKeyDefaultsKey = "io.iogrid.device.wg.privateKey"
  private static let devicePubKeyDefaultsKey  = "io.iogrid.device.wg.publicKey"

  public func definition() -> ModuleDefinition {
    Name("TunnelControl")

    // MARK: - device keypair ---------------------------------------------------

    // Ensure a stable device WireGuard keypair exists and return its
    // base64 PUBLIC key. The private key never leaves native code: it is
    // persisted in the App Group and injected into the tunnel config by
    // `startTunnel` below. WireGuard keys are raw 32-byte Curve25519,
    // base64-encoded — exactly CryptoKit's Curve25519.KeyAgreement raw
    // representation, so no WireGuardKit dependency is needed here.
    AsyncFunction("ensureDeviceKeypair") { (promise: Promise) in
      // #701 fix: persist the device WG keypair in the MAIN APP's standard
      // UserDefaults — NOT the App Group. The App Group entitlement
      // (`group.io.iogrid.app`) is configured ONLY on the tunnel-extension
      // target, so `UserDefaults(suiteName:)` returned nil in the main app
      // and this AsyncFunction rejected with NO_APP_GROUP → the device sent
      // an empty key → the provider never bound it as a peer → the WG
      // handshake never completed → the app hung at "resolving peer". The
      // private half reaches the extension via providerConfiguration
      // (startTunnel below), so the connection needs no App Group at all.
      //
      // INVERSION (#701, G1 v2): the PRIVATE key is the single source of
      // truth — it is what the NE actually SIGNS the WG handshake with. The
      // public key we hand to vpn-svc MUST be the public half of THAT exact
      // private key, or the server registers a pubkey the NE never signs for
      // ("did not decapsulate against any known peer"). So when a persisted
      // private key exists we DERIVE the public key from it every time
      // (re-storing it if absent/stale) instead of trusting a separately
      // persisted public string that could drift. Only when there is no
      // private key at all do we generate a fresh keypair. CryptoKit's
      // Curve25519.KeyAgreement raw public representation == `wg pubkey`.
      let defaults = UserDefaults.standard
      if let privB64 = defaults.string(forKey: Self.devicePrivKeyDefaultsKey),
         !privB64.isEmpty,
         let privData = Data(base64Encoded: privB64),
         let priv = try? Curve25519.KeyAgreement.PrivateKey(rawRepresentation: privData) {
        // Derive the public key FROM the stored private key (source of truth).
        let pubB64 = priv.publicKey.rawRepresentation.base64EncodedString()
        // Keep the cached public string in sync so other readers (stats UI,
        // diagnostics) see the correct value; harmless if already equal.
        if defaults.string(forKey: Self.devicePubKeyDefaultsKey) != pubB64 {
          defaults.set(pubB64, forKey: Self.devicePubKeyDefaultsKey)
        }
        promise.resolve(pubB64)
        return
      }
      // No usable private key persisted yet → generate a fresh keypair once.
      let priv = Curve25519.KeyAgreement.PrivateKey()
      let privB64 = priv.rawRepresentation.base64EncodedString()
      let pubB64 = priv.publicKey.rawRepresentation.base64EncodedString()
      defaults.set(privB64, forKey: Self.devicePrivKeyDefaultsKey)
      defaults.set(pubB64, forKey: Self.devicePubKeyDefaultsKey)
      promise.resolve(pubB64)
    }

    // MARK: - status -----------------------------------------------------------

    AsyncFunction("getStatus") { (promise: Promise) in
      Self.loadManager { manager, error in
        if let error = error {
          promise.reject("LOAD_FAILED", error.localizedDescription)
          return
        }
        guard let manager = manager else {
          promise.resolve("disconnected")
          return
        }
        promise.resolve(statusString(manager.connection.status))
      }
    }

    // MARK: - startTunnel ------------------------------------------------------

    AsyncFunction("startTunnel") { (config: TunnelConfig, promise: Promise) in
      Self.loadManager { manager, _ in
        // ── G1 root cause (#701, proven 2026-06-14 via on-wire decrypt) ──
        //
        // The Network Extension handshakes using ONLY the values baked into
        // the manager's `providerConfiguration`: `clientPrivateKey` SIGNS the
        // WG handshake and `peerPublicKey` is the SERVER identity it encrypts
        // TO. iOS does NOT reliably push an UPDATED providerConfiguration into
        // an ALREADY-INSTALLED tunnel config — re-`saveToPreferences` on a
        // reused manager silently leaves the OLD config's values in the
        // running NE. So build 181's pure-reuse assumption ("clientPrivateKey
        // is refreshed below on every start") was wrong on-device.
        //
        // EVIDENCE the recreate must key on MORE than the client key:
        // capturing the founder's real handshake init (188.135.27.125:51820)
        // and decrypting it with the daemon's responder static proved the
        // init's MAC1 + static-key AEAD are computed for a STALE
        // *peerPublicKey* — a server key from a pre-Jun-10 daemon deployment,
        // NOT the current server key cM9MQ…Gzs= and NOT any client key.
        // Because Noise-IK mixes the responder pubkey into BOTH the MAC1 key
        // and the handshake hash, a stale peerPublicKey alone makes the server
        // log "did not decapsulate against any known peer". #756 recreated the
        // manager only when `clientPrivateKey` drifted, so it never fired for a
        // stale *server* key — which is why build 183 still failed on-device.
        //
        // FIX: reuse the approved manager ONLY when EVERY identity-bearing
        // field already baked into it (clientPrivateKey, peerPublicKey,
        // peerEndpoint) equals what we're about to configure. Otherwise REMOVE
        // it and recreate a fresh NETunnelProviderManager so iOS is forced to
        // install the CURRENT config. The remove is AWAITED to completion
        // before the re-add, which avoids the rapid remove+re-add race that
        // produced build 180's infinite add-config / re-prompt loop. No App
        // Group entitlement is involved — the private key still travels via
        // providerConfiguration only. (#701)
        let currentPriv = UserDefaults.standard.string(forKey: Self.devicePrivKeyDefaultsKey) ?? ""

        let bakedConfig = (manager?.protocolConfiguration as? NETunnelProviderProtocol)?
          .providerConfiguration
        let bakedPriv = bakedConfig?["clientPrivateKey"] as? String
        let bakedPeerPub = bakedConfig?["peerPublicKey"] as? String
        let bakedPeerEndpoint = bakedConfig?["peerEndpoint"] as? String

        // Reuse is safe ONLY when all three already match what we'd configure.
        // ANY drift (incl. a stale server key the OS never refreshed) forces a
        // full teardown + recreate.
        let canReuse =
          manager != nil &&
          !currentPriv.isEmpty &&
          bakedPriv == currentPriv &&
          bakedPeerPub == config.peerPublicKey &&
          bakedPeerEndpoint == config.peerEndpoint

        // Configure + save + start a (possibly fresh) manager.
        let configureAndStart: (NETunnelProviderManager) -> Void = { mgr in
          let proto = NETunnelProviderProtocol()
          proto.providerBundleIdentifier = TunnelControlModule.extensionBundleIdentifier
          proto.serverAddress = config.peerEndpoint  // arbitrary display string for system VPN list
          var providerConfig: [String: Any] = [
            "peerPublicKey": config.peerPublicKey,
            "peerEndpoint": config.peerEndpoint,
            "customerInnerCIDR": config.customerInnerCIDR,
            "allowedIPs": config.allowedIPs,
            "region": config.region,
            "sessionId": config.sessionId,
          ]
          // The device's persistent WG private key (main-app standard
          // UserDefaults, generated by ensureDeviceKeypair) whose PUBLIC half
          // the app registered with vpn-svc — so the provider accepts this
          // device as a peer and the handshake completes. Passed via
          // providerConfiguration (the extension reads clientPrivateKey from
          // there first), so no App Group entitlement is needed. The private
          // key stays native; JS never sees it. (#701)
          if !currentPriv.isEmpty {
            providerConfig["clientPrivateKey"] = currentPriv
          }
          proto.providerConfiguration = providerConfig
          mgr.protocolConfiguration = proto
          mgr.localizedDescription = "iogrid VPN"
          mgr.isEnabled = true

          mgr.saveToPreferences { saveErr in
            if let saveErr = saveErr {
              promise.reject("SAVE_FAILED", saveErr.localizedDescription)
              return
            }
            // After save, reload to get the system-assigned UUID + the
            // active connection object (Apple's documented requirement).
            mgr.loadFromPreferences { loadErr in
              if let loadErr = loadErr {
                promise.reject("RELOAD_FAILED", loadErr.localizedDescription)
                return
              }
              do {
                try mgr.connection.startVPNTunnel()
                promise.resolve(nil)
              } catch let err {
                promise.reject("START_FAILED", err.localizedDescription)
              }
            }
          }
        }

        if canReuse, let mgr = manager {
          // Steady state: identity unchanged → reuse the approved manager (no
          // system prompt). Re-saving the identical config is a no-op for the
          // running NE, which is exactly what we want.
          configureAndStart(mgr)
        } else if let stale = manager {
          // Drift detected (stale client key, stale server key, or endpoint
          // change), or a leftover manager from an earlier build: tear the
          // installed manager DOWN to completion, THEN recreate a fresh one so
          // iOS installs the current key + server identity. Awaiting the remove
          // (not firing remove+add back-to-back) keeps this out of build 180's
          // add-config loop.
          stale.removeFromPreferences { _ in
            // A remove error is non-fatal: we still recreate; the save below
            // surfaces any real failure to JS via SAVE_FAILED.
            configureAndStart(NETunnelProviderManager())
          }
        } else {
          // No existing manager at all (clean install / first connect).
          configureAndStart(NETunnelProviderManager())
        }
      }
    }

    // MARK: - stopTunnel ------------------------------------------------------

    AsyncFunction("stopTunnel") { (promise: Promise) in
      Self.loadManager { manager, error in
        if let error = error {
          promise.reject("LOAD_FAILED", error.localizedDescription)
          return
        }
        guard let manager = manager else {
          promise.resolve(nil)
          return
        }
        manager.connection.stopVPNTunnel()
        promise.resolve(nil)
      }
    }

    // MARK: - sendMessage (to PacketTunnelProvider) ---------------------------
    //
    // For the roaming flow (#572), JS can ask the extension for its
    // current status or force a re-probe. Routed via
    // NETunnelProviderSession.sendProviderMessage which the
    // extension's handleAppMessage receives.

    AsyncFunction("sendProviderMessage") { (command: String, promise: Promise) in
      Self.loadManager { manager, _ in
        // #577 MINOR 3 fix: previously assembled JSON via string
        // interpolation — one careless caller with a quote or
        // backslash in `command` produced invalid JSON. Use
        // JSONSerialization so the wire shape is always parseable.
        guard
          let session = manager?.connection as? NETunnelProviderSession,
          let data = try? JSONSerialization.data(
            withJSONObject: ["command": command],
            options: [])
        else {
          promise.reject("NO_SESSION", "tunnel session not available")
          return
        }
        do {
          try session.sendProviderMessage(data) { response in
            if let response = response, let str = String(data: response, encoding: .utf8) {
              promise.resolve(str)
            } else {
              promise.resolve(nil)
            }
          }
        } catch {
          promise.reject("SEND_FAILED", error.localizedDescription)
        }
      }
    }

    // MARK: - status change events --------------------------------------------
    //
    // Emit "status" events whenever NETunnelProviderManager's
    // connection changes state. JS subscribes via the Expo module
    // event API.
    //
    // ALSO emit "stats" events every ~1s with the latest WireGuard
    // stats from the PacketTunnelProvider extension (#587). The
    // extension writes to App Group UserDefaults; we poll + diff on
    // capturedAtUnixMs so we only emit when there's a new tick.

    Events("status", "stats")

    OnStartObserving {
      // Status observer — keep a strong ref so OnStopObserving can
      // remove it cleanly (CONTRIBUTING #17/#22).
      self.statusObserver = NotificationCenter.default.addObserver(
        forName: .NEVPNStatusDidChange,
        object: nil,
        queue: .main
      ) { [weak self] notification in
        guard
          let connection = notification.object as? NEVPNConnection,
          let self = self
        else { return }
        self.sendEvent("status", ["status": statusString(connection.status)])
      }

      // Stats poll — every statsPollIntervalMs, read latest extension
      // tick from App Group UserDefaults + emit if its
      // capturedAtUnixMs is newer than what we last emitted.
      self.startStatsPolling()
    }

    OnStopObserving {
      if let observer = self.statusObserver {
        NotificationCenter.default.removeObserver(observer)
        self.statusObserver = nil
      }
      self.stopStatsPolling()
    }
  }

  // ── Stats poll (#587) ──────────────────────────────────────────────

  private func startStatsPolling() {
    stopStatsPolling()
    let timer = DispatchSource.makeTimerSource(queue: .main)
    timer.schedule(deadline: .now() + .milliseconds(Self.statsPollIntervalMs),
                   repeating: .milliseconds(Self.statsPollIntervalMs))
    timer.setEventHandler { [weak self] in
      self?.pollStats()
    }
    timer.resume()
    self.statsTimer = timer
  }

  private func stopStatsPolling() {
    statsTimer?.cancel()
    statsTimer = nil
    lastStatsCapturedAtUnixMs = 0
  }

  private func pollStats() {
    guard let defaults = UserDefaults(suiteName: Self.statsAppGroup),
          let str = defaults.string(forKey: Self.statsDefaultsKey),
          let data = str.data(using: .utf8),
          let dict = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any],
          let captured = (dict["captured_at_unix_ms"] as? NSNumber)?.int64Value
    else {
      return
    }
    if captured <= lastStatsCapturedAtUnixMs { return }
    lastStatsCapturedAtUnixMs = captured
    // Forward the parsed dict as-is — JS layer's index.ts types it as
    // { sessionId, sent, received, latency, handshakeAge, ... }.
    sendEvent("stats", dict)
  }

  // ── Helpers ─────────────────────────────────────────────────────

  private static func loadManager(completion: @escaping (NETunnelProviderManager?, Error?) -> Void) {
    NETunnelProviderManager.loadAllFromPreferences { managers, error in
      if let error = error {
        completion(nil, error)
        return
      }
      // Single-VPN-config-per-app pattern: take the first if any.
      completion(managers?.first, nil)
    }
  }
}

// ── Record types ───────────────────────────────────────────────────

struct TunnelConfig: Record {
  @Field var peerPublicKey: String
  @Field var peerEndpoint: String
  @Field var customerInnerCIDR: String
  @Field var allowedIPs: String
  @Field var region: String
  @Field var sessionId: String
}

// ── Status mapping ─────────────────────────────────────────────────

private func statusString(_ s: NEVPNStatus) -> String {
  switch s {
  case .invalid:       return "invalid"
  case .disconnected:  return "disconnected"
  case .connecting:    return "connecting"
  case .connected:     return "connected"
  case .reasserting:   return "reasserting"
  case .disconnecting: return "disconnecting"
  @unknown default:    return "unknown"
  }
}
