//! TunnelManager — daemon-side data plane for the TCP-over-DispatchFrame
//! byte-forwarding path documented in `proto/iogrid/workloads/v1/dispatch.proto`.
//!
//! ```text
//!  customer ──TCP──▶ proxy-gateway ──TCP──▶ workloads-svc.forwarder
//!                                            │
//!                                            │ TunnelOpen / TunnelData / TunnelClose
//!                                            ▼
//!                                          daemon ──TCP──▶ www.linkedin.com:443
//!                                            │  (response bytes wrapped in
//!                                            ▼   TunnelData frames going back)
//! ```
//!
//! Before this module landed, the daemon dropped all three tunnel frame
//! variants on the floor (`convert.rs:298`'s "PR #228 / future PR" stub),
//! so no bytes ever flowed end-to-end. iogrid/iogrid#482 root-caused the
//! gap; this module is the fix.
//!
//! Per-attempt lifecycle:
//!   1. `TunnelOpen { attempt_id, target_host_port }` arrives from the
//!      dispatch stream → spawn a tunnel task that opens a TCP socket to
//!      `target_host_port`. The task owns two halves: a sender that
//!      receives upstream bytes from the TCP read half and forwards
//!      `TunnelData` frames back through the outbound mpsc, and a
//!      mailbox channel for inbound `TunnelData` payloads coming from
//!      the coordinator.
//!   2. Each subsequent `TunnelData { attempt_id, payload }` is routed
//!      to that task's mailbox; the task writes the payload to the TCP
//!      write half.
//!   3. `TunnelClose { attempt_id, error }` (or EOF on either half)
//!      shuts down the task; we emit a `TunnelClose` back to the
//!      coordinator so the forwarder unblocks the proxy-gateway side.
//!
//! All sockets close on supervisor drop because each task holds an
//! `Arc<Mutex<HashMap<...>>>` weak ref pattern — when the manager is
//! dropped, the mailbox senders close, and the tasks naturally exit
//! their `select!`.

use std::collections::HashMap;
use std::sync::Arc;

use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpStream;
use tokio::sync::{mpsc, Mutex};

use iogrid_transport::DispatchFrame;

/// Max bytes per TunnelData chunk read from upstream. 16 KiB matches
/// the BoringSSL TLS record ceiling — keeps frame sizes predictable on
/// the bidi stream.
const CHUNK_SIZE: usize = 16 * 1024;

/// Mailbox buffer per tunnel. Bytes-from-coordinator arrive faster than
/// the upstream socket can drain when the upstream is slow; 64 chunks
/// (1 MiB at 16 KiB each) caps in-flight memory per tunnel.
const MAILBOX_BUF: usize = 64;

/// Inbound payload for a tunnel: either a chunk of bytes to write to
/// the upstream socket, or a close signal.
#[derive(Debug)]
enum Inbound {
    Data(Vec<u8>),
    Close(String),
}

/// Per-attempt mailbox sender. The manager keeps one per open tunnel
/// and routes inbound `TunnelData`/`TunnelClose` frames to it.
type TunnelTx = mpsc::Sender<Inbound>;

/// TunnelManager owns the in-memory map of attempt_id → mailbox sender.
/// Created once on supervisor startup; shared via `Arc` between the
/// dispatch frame router and the tunnel tasks.
pub struct TunnelManager {
    tunnels: Arc<Mutex<HashMap<String, TunnelTx>>>,
    /// Outbound dispatch channel — every `TunnelData` chunk read from
    /// the upstream socket is wrapped in a `DispatchFrame::TunnelData`
    /// and pushed here. The daemon's bridge pump pulls from this and
    /// sends it on the wire to workloads-svc.
    outbound: mpsc::Sender<DispatchFrame>,
}

impl TunnelManager {
    pub fn new(outbound: mpsc::Sender<DispatchFrame>) -> Self {
        Self {
            tunnels: Arc::new(Mutex::new(HashMap::new())),
            outbound,
        }
    }

    /// Open a new tunnel. Dials `target_host_port` and spawns the
    /// bidirectional pump. On dial failure, emits `TunnelClose` back
    /// up the dispatch stream with the error string.
    pub async fn open(&self, attempt_id: String, target_host_port: String) {
        if attempt_id.is_empty() || target_host_port.is_empty() {
            tracing::warn!(
                target: "tunnel",
                attempt_id = %attempt_id,
                target = %target_host_port,
                "TunnelOpen rejected — empty attempt_id or target_host_port"
            );
            return;
        }

        tracing::info!(
            target: "tunnel",
            attempt_id = %attempt_id,
            target = %target_host_port,
            "tunnel opening — dialling upstream"
        );

        let stream = match TcpStream::connect(&target_host_port).await {
            Ok(s) => {
                tracing::info!(
                    target: "tunnel",
                    attempt_id = %attempt_id,
                    target = %target_host_port,
                    "tunnel upstream dial OK"
                );
                s
            }
            Err(e) => {
                tracing::warn!(
                    target: "tunnel",
                    attempt_id = %attempt_id,
                    target = %target_host_port,
                    error = %e,
                    "tunnel upstream dial FAILED"
                );
                let _ = self
                    .outbound
                    .send(DispatchFrame::TunnelClose {
                        attempt_id,
                        error: format!("dial_failed: {e}"),
                    })
                    .await;
                return;
            }
        };

        let (mailbox_tx, mailbox_rx) = mpsc::channel::<Inbound>(MAILBOX_BUF);
        {
            let mut g = self.tunnels.lock().await;
            if let Some(old) = g.insert(attempt_id.clone(), mailbox_tx) {
                // Same attempt_id reused — the coordinator wouldn't
                // normally do this but drop the old mailbox so its
                // task exits.
                drop(old);
            }
        }

        let tunnels = self.tunnels.clone();
        let outbound = self.outbound.clone();
        let aid = attempt_id.clone();
        tokio::spawn(async move {
            pump(aid.clone(), stream, mailbox_rx, outbound.clone()).await;
            // Drop the map entry on exit so we don't leak.
            tunnels.lock().await.remove(&aid);
        });
    }

    /// Forward a `TunnelData` payload to the tunnel's mailbox. Drops
    /// silently if the attempt_id is unknown (cleanup race with EOF).
    pub async fn data(&self, attempt_id: &str, payload: Vec<u8>) {
        let tx = {
            let g = self.tunnels.lock().await;
            g.get(attempt_id).cloned()
        };
        if let Some(tx) = tx {
            // try_send to avoid blocking the dispatch pump if the
            // upstream socket is slow; a full mailbox means the slow
            // side is the destination, not us.
            if let Err(e) = tx.send(Inbound::Data(payload)).await {
                tracing::debug!(
                    target: "tunnel",
                    attempt_id = %attempt_id,
                    error = ?e,
                    "tunnel mailbox send failed (task already exited)"
                );
            }
        } else {
            tracing::debug!(
                target: "tunnel",
                attempt_id = %attempt_id,
                "tunnel data for unknown attempt — dropping"
            );
        }
    }

    /// Close a tunnel from the coordinator side.
    pub async fn close(&self, attempt_id: &str, error: String) {
        let tx = {
            let mut g = self.tunnels.lock().await;
            g.remove(attempt_id)
        };
        if let Some(tx) = tx {
            let _ = tx.send(Inbound::Close(error)).await;
            // Drop the sender; the pump's recv will return None.
            drop(tx);
        }
    }
}

/// Bidirectional pump for one tunnel. Reads upstream → emits
/// `TunnelData` frames outbound; receives mailbox → writes to upstream.
async fn pump(
    attempt_id: String,
    stream: TcpStream,
    mut mailbox_rx: mpsc::Receiver<Inbound>,
    outbound: mpsc::Sender<DispatchFrame>,
) {
    let (mut read_half, mut write_half) = stream.into_split();
    let aid_for_reader = attempt_id.clone();
    let aid_for_writer = attempt_id.clone();
    let outbound_for_reader = outbound.clone();

    // Reader task: pump upstream bytes → outbound TunnelData frames.
    let reader = tokio::spawn(async move {
        let mut buf = vec![0u8; CHUNK_SIZE];
        loop {
            match read_half.read(&mut buf).await {
                Ok(0) => {
                    tracing::debug!(
                        target: "tunnel",
                        attempt_id = %aid_for_reader,
                        "upstream EOF"
                    );
                    break;
                }
                Ok(n) => {
                    if outbound_for_reader
                        .send(DispatchFrame::TunnelData {
                            attempt_id: aid_for_reader.clone(),
                            payload: buf[..n].to_vec(),
                        })
                        .await
                        .is_err()
                    {
                        tracing::debug!(
                            target: "tunnel",
                            attempt_id = %aid_for_reader,
                            "outbound dispatch channel closed — bridge gone"
                        );
                        break;
                    }
                }
                Err(e) => {
                    tracing::debug!(
                        target: "tunnel",
                        attempt_id = %aid_for_reader,
                        error = %e,
                        "upstream read error"
                    );
                    break;
                }
            }
        }
    });

    // Writer loop: mailbox → upstream socket.
    let mut close_reason = String::new();
    while let Some(inbound) = mailbox_rx.recv().await {
        match inbound {
            Inbound::Data(payload) => {
                if let Err(e) = write_half.write_all(&payload).await {
                    tracing::debug!(
                        target: "tunnel",
                        attempt_id = %aid_for_writer,
                        error = %e,
                        "upstream write error"
                    );
                    close_reason = format!("write_failed: {e}");
                    break;
                }
            }
            Inbound::Close(err) => {
                close_reason = err;
                break;
            }
        }
    }
    // Best-effort flush + shutdown the write half.
    let _ = write_half.shutdown().await;
    reader.abort();

    // Tell the coordinator we're done.
    let _ = outbound
        .send(DispatchFrame::TunnelClose {
            attempt_id: attempt_id.clone(),
            error: close_reason,
        })
        .await;

    tracing::info!(
        target: "tunnel",
        attempt_id = %attempt_id,
        "tunnel closed"
    );
}

#[cfg(test)]
mod tests {
    use super::*;
    use tokio::io::AsyncWriteExt;
    use tokio::net::TcpListener;

    #[tokio::test]
    async fn open_dial_failure_emits_tunnel_close() {
        let (outbound_tx, mut outbound_rx) = mpsc::channel::<DispatchFrame>(8);
        let mgr = TunnelManager::new(outbound_tx);

        // Port 1 is reserved + always refuses connections.
        mgr.open("aid-1".into(), "127.0.0.1:1".into()).await;

        let frame = tokio::time::timeout(std::time::Duration::from_secs(2), outbound_rx.recv())
            .await
            .expect("dial-failure TunnelClose should arrive within 2s")
            .expect("outbound channel closed unexpectedly");

        match frame {
            DispatchFrame::TunnelClose { attempt_id, error } => {
                assert_eq!(attempt_id, "aid-1");
                assert!(
                    error.starts_with("dial_failed"),
                    "unexpected error string: {error}"
                );
            }
            other => panic!("expected TunnelClose, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn end_to_end_echo_through_tunnel() {
        // Stand up a tiny TCP echo server on a random port.
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        tokio::spawn(async move {
            let (mut sock, _) = listener.accept().await.unwrap();
            let mut buf = [0u8; 1024];
            let n = sock.read(&mut buf).await.unwrap();
            sock.write_all(&buf[..n]).await.unwrap();
        });

        let (outbound_tx, mut outbound_rx) = mpsc::channel::<DispatchFrame>(8);
        let mgr = TunnelManager::new(outbound_tx);
        mgr.open("echo".into(), format!("127.0.0.1:{}", addr.port()))
            .await;

        // Send the request bytes.
        mgr.data("echo", b"hello world".to_vec()).await;

        // Expect the echoed bytes back as an outbound TunnelData frame.
        let frame = tokio::time::timeout(std::time::Duration::from_secs(2), outbound_rx.recv())
            .await
            .expect("echo response should arrive within 2s")
            .expect("outbound channel closed unexpectedly");

        match frame {
            DispatchFrame::TunnelData {
                attempt_id,
                payload,
            } => {
                assert_eq!(attempt_id, "echo");
                assert_eq!(payload, b"hello world");
            }
            other => panic!("expected TunnelData, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn close_drops_mailbox() {
        let (outbound_tx, mut outbound_rx) = mpsc::channel::<DispatchFrame>(8);
        let mgr = TunnelManager::new(outbound_tx);

        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        tokio::spawn(async move {
            let _ = listener.accept().await;
            // Hold the conn open — we want the writer pump alive when close fires.
            tokio::time::sleep(std::time::Duration::from_secs(5)).await;
        });

        mgr.open("c".into(), format!("127.0.0.1:{}", addr.port()))
            .await;
        // Give the tunnel a tick to register.
        tokio::time::sleep(std::time::Duration::from_millis(50)).await;
        mgr.close("c", "explicit close".into()).await;

        let frame = tokio::time::timeout(std::time::Duration::from_secs(2), outbound_rx.recv())
            .await
            .expect("close should produce TunnelClose within 2s")
            .expect("channel closed");

        match frame {
            DispatchFrame::TunnelClose { attempt_id, error } => {
                assert_eq!(attempt_id, "c");
                assert_eq!(error, "explicit close");
            }
            other => panic!("expected TunnelClose, got {other:?}"),
        }
    }
}
