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

  public func definition() -> ModuleDefinition {
    Name("TunnelControl")

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
        let mgr = manager ?? NETunnelProviderManager()
        let proto = NETunnelProviderProtocol()
        proto.providerBundleIdentifier = TunnelControlModule.extensionBundleIdentifier
        proto.serverAddress = config.peerEndpoint  // arbitrary display string for system VPN list
        proto.providerConfiguration = [
          "peerPublicKey": config.peerPublicKey,
          "peerEndpoint": config.peerEndpoint,
          "customerInnerCIDR": config.customerInnerCIDR,
          "allowedIPs": config.allowedIPs,
          "region": config.region,
          "sessionId": config.sessionId,
        ]
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
