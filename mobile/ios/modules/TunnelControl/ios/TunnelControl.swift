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
        proto.providerBundleIdentifier = "io.iogrid.app.PacketTunnelProvider"
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
        guard
          let session = manager?.connection as? NETunnelProviderSession,
          let data = "{\"command\":\"\(command)\"}".data(using: .utf8)
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

    Events("status")

    OnStartObserving {
      NotificationCenter.default.addObserver(
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
    }

    OnStopObserving {
      NotificationCenter.default.removeObserver(self, name: .NEVPNStatusDidChange, object: nil)
    }
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
