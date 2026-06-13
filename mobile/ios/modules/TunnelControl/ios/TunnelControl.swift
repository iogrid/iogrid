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
      let defaults = UserDefaults.standard
      if let pub = defaults.string(forKey: Self.devicePubKeyDefaultsKey),
         defaults.string(forKey: Self.devicePrivKeyDefaultsKey) != nil,
         !pub.isEmpty {
        promise.resolve(pub)
        return
      }
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
        // The Network Extension handshakes with the `clientPrivateKey` baked
        // into the manager's providerConfiguration. iOS does NOT reliably push
        // an UPDATED providerConfiguration into an already-installed tunnel
        // config, so a manager created by an earlier build/session keeps its
        // STALE key even after we re-save a new one here — which is why build
        // 181's pure-reuse assumption ("the key is still refreshed below") was
        // wrong on-device. When the device keypair has since rotated and we
        // registered the NEW public half with vpn-svc, the provider drops the
        // handshake as "did not decapsulate against any known peer" (verified
        // 2026-06-13: the server's own keypair + customer peer-bind are correct,
        // so the mismatch is purely the extension's stale private key).
        //
        // Fix: REUSE the approved manager only when its baked key already equals
        // the current device key (no system prompt — the common steady state).
        // Otherwise remove it and recreate so the NE picks up the CURRENT
        // keypair. The remove is AWAITED before recreate, which avoids the rapid
        // remove+re-add race that produced build 180's infinite add-config /
        // re-prompt loop. (#701)
        let currentPriv = UserDefaults.standard.string(forKey: Self.devicePrivKeyDefaultsKey) ?? ""
        let bakedPriv = (manager?.protocolConfiguration as? NETunnelProviderProtocol)?
          .providerConfiguration?["clientPrivateKey"] as? String

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
          // UserDefaults, generated by ensureDeviceKeypair) whose PUBLIC half the
          // app registered with vpn-svc. Passed via providerConfiguration (the
          // extension reads clientPrivateKey from there first), so no App Group
          // entitlement is needed. The private key stays native; JS never sees it.
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

        if let mgr = manager, !currentPriv.isEmpty, bakedPriv == currentPriv {
          // Approved manager already carries the current keypair → reuse it, no
          // system prompt, no race.
          configureAndStart(mgr)
        } else if let stale = manager {
          // Baked key is stale/rotated (or absent) → remove then recreate so the
          // NE handshakes with the CURRENT keypair. Awaiting the remove before
          // recreating is what keeps this from regressing into build 180's loop.
          stale.removeFromPreferences { _ in
            configureAndStart(NETunnelProviderManager())
          }
        } else {
          // First run on this device: no manager yet (one approval prompt is
          // expected and correct here).
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
