//! macOS-only IPC server for the menu-bar status app.
//!
//! Issue #388 / EPIC #348 Phase 2-mac. Listens on a Unix-domain socket
//! at `$state_dir/run/iogridd.sock` (typically `~/.iogrid/run/iogridd.sock`)
//! and handles a tiny line-JSON protocol:
//!
//!   request  : {"cmd":"quit"}\n
//!   response : {"ok":true}\n             OR
//!              {"ok":false,"error":"<m>"}\n
//!
//! The Swift status-bar client at
//! `installer/macos/statusbar/Sources/IogriddStatusbar/IPC.swift` is
//! the only consumer today. The protocol stays minimal on purpose: a
//! future "show notification" or "reload config" command can be added
//! by extending [`IpcCommand`] without breaking the wire shape.
//!
//! The listener is opt-in at supervisor wiring time: `Supervisor::run`
//! only spawns it on macOS (we feature-gate via `#[cfg(target_os = "macos")]`
//! at the call site) and only when the state dir is writable. On any
//! other OS the entire module compiles to nothing.

#![cfg(target_os = "macos")]

use std::path::{Path, PathBuf};
use std::sync::Arc;

use serde::{Deserialize, Serialize};
use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};
use tokio::net::UnixListener;
use tokio::sync::Notify;

/// Wire-format request frame. Newline-delimited.
#[derive(Debug, Deserialize)]
struct IpcRequest {
    /// Command name. Currently only "quit" is recognised; everything
    /// else returns `{"ok":false,"error":"unknown command"}` so future
    /// additive commands can be probed for support by callers.
    cmd: String,
}

/// Wire-format response frame. Always written before the connection
/// is closed, so the Swift client can distinguish "daemon ack'd" from
/// "daemon never replied" with a recv timeout.
#[derive(Debug, Serialize)]
struct IpcResponse {
    ok: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    error: Option<String>,
}

impl IpcResponse {
    fn ok() -> Self {
        Self {
            ok: true,
            error: None,
        }
    }

    fn err(msg: impl Into<String>) -> Self {
        Self {
            ok: false,
            error: Some(msg.into()),
        }
    }
}

/// Recognised commands. Only `Quit` exists today; adding variants here
/// is the path for future menu items (e.g. `OpenConsole`).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum IpcCommand {
    Quit,
    Unknown,
}

fn parse_command(name: &str) -> IpcCommand {
    match name {
        "quit" => IpcCommand::Quit,
        _ => IpcCommand::Unknown,
    }
}

/// Resolve the socket path under a daemon state dir. The directory is
/// `$state_dir/run/`, the file is `iogridd.sock`. Public so the
/// Supervisor can pass the same value when binding.
pub fn socket_path(state_dir: &Path) -> PathBuf {
    state_dir.join("run").join("iogridd.sock")
}

/// Spawn the IPC listener as a tokio task.
///
/// Arguments:
///   * `socket_path` — absolute path of the UDS to bind. Parent
///     directory is created if missing. Any stale socket file at the
///     path is unlinked before bind (launchd may leave the previous
///     boot's file behind on a hard kill).
///   * `shutdown` — `Arc<Notify>` the listener will fire when a `quit`
///     command is received. The Supervisor's main loop awaits this
///     `Notified` future alongside SIGINT/SIGTERM so the menu-bar UI
///     and Ctrl+C produce identical shutdown paths.
///
/// Returns immediately with a `JoinHandle`; the listener runs until
/// the task is aborted, the bind fails, or the supervisor process
/// exits and the socket file is removed.
pub fn spawn_listener(
    socket_path: PathBuf,
    shutdown: Arc<Notify>,
) -> tokio::task::JoinHandle<anyhow::Result<()>> {
    tokio::spawn(async move {
        if let Some(parent) = socket_path.parent() {
            if let Err(e) = std::fs::create_dir_all(parent) {
                tracing::warn!(
                    path = %parent.display(),
                    error = %e,
                    "ipc_mac: create_dir_all failed; listener not started"
                );
                return Err(anyhow::Error::from(e));
            }
        }
        // Best-effort unlink — ignore NotFound, surface anything else.
        match std::fs::remove_file(&socket_path) {
            Ok(_) => {}
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => {}
            Err(e) => {
                tracing::warn!(
                    path = %socket_path.display(),
                    error = %e,
                    "ipc_mac: stale socket cleanup failed"
                );
            }
        }

        let listener = UnixListener::bind(&socket_path)?;
        tracing::info!(path = %socket_path.display(), "ipc_mac: listening for status-bar commands");

        loop {
            let (stream, _addr) = match listener.accept().await {
                Ok(pair) => pair,
                Err(e) => {
                    tracing::warn!(error = %e, "ipc_mac: accept failed; continuing");
                    continue;
                }
            };

            let shutdown_clone = shutdown.clone();
            tokio::spawn(async move {
                if let Err(e) = handle_connection(stream, shutdown_clone).await {
                    tracing::debug!(error = %e, "ipc_mac: connection handler ended with error");
                }
            });
        }
    })
}

/// Read one request line, dispatch the command, write one response
/// line, close the connection. Exposed `pub(crate)` for unit tests.
pub(crate) async fn handle_connection(
    stream: tokio::net::UnixStream,
    shutdown: Arc<Notify>,
) -> anyhow::Result<()> {
    let (read_half, mut write_half) = stream.into_split();
    let mut reader = BufReader::new(read_half);
    let mut line = String::new();
    let n = reader.read_line(&mut line).await?;
    if n == 0 {
        // Peer closed without sending; nothing to do.
        return Ok(());
    }

    let request: Result<IpcRequest, _> = serde_json::from_str(line.trim_end());
    let response = match request {
        Ok(req) => dispatch(parse_command(&req.cmd), &shutdown),
        Err(e) => IpcResponse::err(format!("invalid request json: {e}")),
    };

    let mut body = serde_json::to_string(&response).unwrap_or_else(|_| {
        // Should never happen — IpcResponse always serialises.
        "{\"ok\":false,\"error\":\"internal serialization error\"}".to_string()
    });
    body.push('\n');
    write_half.write_all(body.as_bytes()).await?;
    write_half.flush().await?;
    Ok(())
}

fn dispatch(cmd: IpcCommand, shutdown: &Arc<Notify>) -> IpcResponse {
    match cmd {
        IpcCommand::Quit => {
            tracing::info!("ipc_mac: quit command received from status-bar UI");
            shutdown.notify_waiters();
            IpcResponse::ok()
        }
        IpcCommand::Unknown => IpcResponse::err("unknown command"),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::Duration;
    use tokio::io::AsyncReadExt;
    use tokio::net::UnixStream;

    /// `socket_path` always ends in `run/iogridd.sock`.
    #[test]
    fn socket_path_under_state_dir() {
        let p = socket_path(Path::new("/tmp/foo"));
        assert_eq!(p, PathBuf::from("/tmp/foo/run/iogridd.sock"));
    }

    /// `parse_command` recognises "quit" and falls through to Unknown.
    #[test]
    fn parse_command_table() {
        assert_eq!(parse_command("quit"), IpcCommand::Quit);
        assert_eq!(parse_command("QUIT"), IpcCommand::Unknown);
        assert_eq!(parse_command(""), IpcCommand::Unknown);
        assert_eq!(parse_command("something-else"), IpcCommand::Unknown);
    }

    /// A full roundtrip: bind, connect, send `{"cmd":"quit"}\n`, read
    /// `{"ok":true}\n`, observe `Notify` fired.
    #[tokio::test(flavor = "current_thread")]
    async fn quit_roundtrip_fires_shutdown_notify() {
        let dir = tempfile::tempdir().unwrap();
        let sock = socket_path(dir.path());
        let notify = Arc::new(Notify::new());
        let listener_handle = spawn_listener(sock.clone(), notify.clone());

        // Give the listener a moment to bind.
        for _ in 0..50 {
            if sock.exists() {
                break;
            }
            tokio::time::sleep(Duration::from_millis(20)).await;
        }
        assert!(sock.exists(), "listener never bound the socket");

        // Subscribe BEFORE sending so we don't race the notify_waiters call.
        let notified = notify.notified();
        tokio::pin!(notified);

        let mut stream = UnixStream::connect(&sock).await.expect("connect");
        stream
            .write_all(b"{\"cmd\":\"quit\"}\n")
            .await
            .expect("write");
        stream.flush().await.expect("flush");

        let mut buf = Vec::new();
        // The handler writes one line then drops; read_to_end terminates
        // promptly on EOF.
        stream.read_to_end(&mut buf).await.expect("read");
        let body = String::from_utf8(buf).expect("utf8");
        assert!(body.contains("\"ok\":true"), "body was {body:?}");

        tokio::time::timeout(Duration::from_secs(1), notified)
            .await
            .expect("shutdown notify should fire within 1s");

        listener_handle.abort();
    }

    /// Unknown command returns `{"ok":false,"error":"unknown command"}`
    /// and does NOT fire the shutdown notify.
    #[tokio::test(flavor = "current_thread")]
    async fn unknown_command_does_not_fire_shutdown() {
        let dir = tempfile::tempdir().unwrap();
        let sock = socket_path(dir.path());
        let notify = Arc::new(Notify::new());
        let listener_handle = spawn_listener(sock.clone(), notify.clone());

        for _ in 0..50 {
            if sock.exists() {
                break;
            }
            tokio::time::sleep(Duration::from_millis(20)).await;
        }
        assert!(sock.exists());

        let mut stream = UnixStream::connect(&sock).await.expect("connect");
        stream
            .write_all(b"{\"cmd\":\"reload-config\"}\n")
            .await
            .expect("write");
        stream.flush().await.expect("flush");
        let mut buf = Vec::new();
        stream.read_to_end(&mut buf).await.expect("read");
        let body = String::from_utf8(buf).expect("utf8");
        assert!(body.contains("\"ok\":false"));
        assert!(body.contains("unknown command"));

        // notify_waiters should NOT have fired — confirm by racing a
        // short sleep against `notified()`.
        let fired = tokio::time::timeout(Duration::from_millis(150), notify.notified()).await;
        assert!(fired.is_err(), "shutdown notify must not fire on unknown");

        listener_handle.abort();
    }

    /// Malformed JSON returns a structured error rather than crashing
    /// the handler task.
    #[tokio::test(flavor = "current_thread")]
    async fn malformed_json_returns_error_envelope() {
        let dir = tempfile::tempdir().unwrap();
        let sock = socket_path(dir.path());
        let notify = Arc::new(Notify::new());
        let listener_handle = spawn_listener(sock.clone(), notify.clone());

        for _ in 0..50 {
            if sock.exists() {
                break;
            }
            tokio::time::sleep(Duration::from_millis(20)).await;
        }

        let mut stream = UnixStream::connect(&sock).await.expect("connect");
        stream
            .write_all(b"this is not json\n")
            .await
            .expect("write");
        stream.flush().await.expect("flush");
        let mut buf = Vec::new();
        stream.read_to_end(&mut buf).await.expect("read");
        let body = String::from_utf8(buf).expect("utf8");
        assert!(body.contains("\"ok\":false"));
        assert!(body.contains("invalid request json"));

        listener_handle.abort();
    }
}
