// IPC.swift — talk to the headless iogridd daemon over its Unix-domain
// socket at `~/.iogrid/run/iogridd.sock`.
//
// We deliberately use the raw `socket(2)` / `connect(2)` / `write(2)` /
// `read(2)` BSD API rather than Foundation's `URLSession` (which only
// speaks TCP) or `NWConnection` (whose `.unix` support is documented
// only on macOS 14+, and we target macOS 13+). Three syscalls + a
// 1-second receive timeout is enough.
//
// Wire protocol: newline-delimited JSON.
//   request  : {"cmd":"quit"}\n
//   response : {"ok":true}\n      | {"ok":false,"error":"<msg>"}\n
//
// The Rust daemon's matching server lives in
//   daemon/crates/core/src/ipc_mac.rs
// and is only compiled when target_os = "macos" — Linux / Windows
// providers don't run this status-bar app and therefore don't need
// the UDS listener either.

import Darwin
import Foundation

/// One-shot UDS client. No connection pooling — `Check for updates…`
/// and `Quit` are infrequent user-driven actions.
enum IogriddIPC {
    /// Errors surfaced from the IPC helper. Mirrors what we want to log
    /// in `main.swift::quitDaemon`.
    enum IPCError: Error, CustomStringConvertible {
        case homeUnset
        case socketCreateFailed(errno: Int32)
        case pathTooLong(path: String)
        case connectFailed(errno: Int32, path: String)
        case writeFailed(errno: Int32)
        case readFailed(errno: Int32)
        case invalidResponse(String)
        case daemonRejected(message: String)

        var description: String {
            switch self {
            case .homeUnset:
                return "HOME env var is unset; cannot locate iogridd.sock"
            case .socketCreateFailed(let e):
                return "socket(AF_UNIX) failed: errno=\(e) (\(String(cString: strerror(e))))"
            case .pathTooLong(let p):
                return "socket path too long for sockaddr_un.sun_path: \(p)"
            case .connectFailed(let e, let p):
                return "connect(\(p)) failed: errno=\(e) (\(String(cString: strerror(e))))"
            case .writeFailed(let e):
                return "write() failed: errno=\(e) (\(String(cString: strerror(e))))"
            case .readFailed(let e):
                return "read() failed: errno=\(e) (\(String(cString: strerror(e))))"
            case .invalidResponse(let body):
                return "invalid IPC response: \(body)"
            case .daemonRejected(let m):
                return "daemon rejected command: \(m)"
            }
        }
    }

    /// Send a `{"cmd":"<command>"}` JSON-line and wait for the daemon's
    /// reply (1-second receive timeout). Synchronous — call from a
    /// background DispatchQueue.
    static func send(command: String) -> Result<Void, IPCError> {
        guard let home = ProcessInfo.processInfo.environment["HOME"] else {
            return .failure(.homeUnset)
        }
        let path = "\(home)/.iogrid/run/iogridd.sock"
        return send(command: command, socketPath: path)
    }

    /// Variant with an explicit socket path — used by unit tests that
    /// don't want to depend on the real `~/.iogrid` layout.
    static func send(command: String, socketPath: String) -> Result<Void, IPCError> {
        // sockaddr_un.sun_path is a fixed 104-byte buffer on Darwin.
        // Reject pathologically long paths up front so we don't truncate
        // silently and connect() to a wrong / nonexistent endpoint.
        let pathBytes = Array(socketPath.utf8) + [0]
        guard pathBytes.count <= 104 else {
            return .failure(.pathTooLong(path: socketPath))
        }

        let fd = socket(AF_UNIX, SOCK_STREAM, 0)
        guard fd >= 0 else {
            return .failure(.socketCreateFailed(errno: errno))
        }
        defer { close(fd) }

        // Apply a 1-second send + recv timeout so we don't hang the UI
        // if the daemon has crashed mid-handler.
        var tv = timeval(tv_sec: 1, tv_usec: 0)
        _ = setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &tv, socklen_t(MemoryLayout<timeval>.size))
        _ = setsockopt(fd, SOL_SOCKET, SO_SNDTIMEO, &tv, socklen_t(MemoryLayout<timeval>.size))

        // Build sockaddr_un. The Darwin struct is fixed-size with a
        // leading sun_len byte (BSD quirk Linux doesn't have). We write
        // the path bytes into sun_path via unsafe pointer arithmetic.
        var addr = sockaddr_un()
        addr.sun_family = sa_family_t(AF_UNIX)
        addr.sun_len = UInt8(min(255, MemoryLayout<sockaddr_un>.size))
        // Scope the exclusive access to addr.sun_path to its own closure so
        // it ENDS before we take &addr below — Swift's exclusivity rule
        // rejects nested overlapping mutable accesses to the same storage.
        withUnsafeMutablePointer(to: &addr.sun_path) { sunPathPtr in
            let raw = UnsafeMutableRawPointer(sunPathPtr)
            let bytes = raw.assumingMemoryBound(to: UInt8.self)
            for (i, b) in pathBytes.enumerated() {
                bytes[i] = b
            }
        }
        let len = socklen_t(MemoryLayout<sockaddr_un>.size)
        let connectResult: Int32 = withUnsafePointer(to: &addr) { addrPtr in
            addrPtr.withMemoryRebound(to: sockaddr.self, capacity: 1) { castPtr in
                Darwin.connect(fd, castPtr, len)
            }
        }
        guard connectResult == 0 else {
            return .failure(.connectFailed(errno: errno, path: socketPath))
        }

        // Send the command. Wrap as a single line: the Rust side reads
        // until newline, then parses serde_json::from_str on the prefix.
        let payload = "{\"cmd\":\"\(command)\"}\n"
        let payloadBytes = Array(payload.utf8)
        let written = payloadBytes.withUnsafeBufferPointer { buf -> Int in
            Darwin.write(fd, buf.baseAddress, buf.count)
        }
        if written != payloadBytes.count {
            return .failure(.writeFailed(errno: errno))
        }

        // Read up to 256 bytes of response. The daemon's reply is a
        // single newline-terminated JSON object well under that.
        var responseBuf = [UInt8](repeating: 0, count: 256)
        let readCount = responseBuf.withUnsafeMutableBufferPointer { buf -> Int in
            Darwin.read(fd, buf.baseAddress, buf.count)
        }
        if readCount < 0 {
            return .failure(.readFailed(errno: errno))
        }
        if readCount == 0 {
            // Empty reply — treat as failure rather than success; the
            // daemon-side handler always writes at least `{"ok":true}\n`.
            return .failure(.invalidResponse("empty reply"))
        }

        let bodyData = Data(bytes: responseBuf, count: readCount)
        guard let body = String(data: bodyData, encoding: .utf8) else {
            return .failure(.invalidResponse("non-utf8"))
        }
        return parseResponse(body)
    }

    /// Parse the daemon's `{"ok":true}` / `{"ok":false,"error":"..."}`
    /// response. Exposed `internal` so unit tests can hit it directly.
    static func parseResponse(_ body: String) -> Result<Void, IPCError> {
        // Strip trailing whitespace / newline.
        let trimmed = body.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let data = trimmed.data(using: .utf8) else {
            return .failure(.invalidResponse(body))
        }
        do {
            guard let obj = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
                return .failure(.invalidResponse(body))
            }
            let ok = obj["ok"] as? Bool ?? false
            if ok {
                return .success(())
            } else {
                let msg = (obj["error"] as? String) ?? "unspecified"
                return .failure(.daemonRejected(message: msg))
            }
        } catch {
            return .failure(.invalidResponse(body))
        }
    }
}
