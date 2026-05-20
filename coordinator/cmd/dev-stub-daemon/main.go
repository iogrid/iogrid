// Command dev-stub-daemon is a minimal Go stand-in for the production
// Rust `iogridd` binary. It performs the bare minimum a daemon needs to
// keep a Dispatch bidi stream open with workloads-svc:
//
//  1. Loads the paired mTLS identity from ~/.iogrid/cert.pem + ~/.iogrid/key.pem
//     (paths overridable via IOGRID_CERT_PEM / IOGRID_KEY_PEM).
//  2. Reads the provider id from ~/.iogrid/config.toml (or the
//     IOGRID_PROVIDER_ID env var override).
//  3. Opens a Connect-RPC bidi stream against
//     $IOGRID_COORDINATOR_URL/iogrid.workloads.v1.WorkloadDispatchService/Dispatch
//     (default https://api.iogrid.org).
//  4. Sends a DaemonHello frame, reads the CoordinatorHello ack, then
//     loops:
//     - every 5s, sends a Ping (server-timestamped liveness frame),
//     - on inbound Assignment frames, replies with a synthetic
//     WorkloadStatusUpdate{status: WORKLOAD_STATUS_FAILED, note:
//     "dev-stub: not executed"} so the dispatcher sees the attempt
//     was "tried" and can retry / time-out gracefully,
//     - on inbound TunnelOpen frames, dials the requested
//     target_host_port directly and pumps bytes both ways via
//     TunnelData frames keyed by attempt_id (iogrid#279). On dial
//     failure or destination EOF, sends a TunnelClose to the
//     coordinator so the proxy-gateway-side socket closes cleanly.
//  5. On SIGINT/SIGTERM, sends a Drain frame and exits cleanly.
//
// The point of this stub is to UNBLOCK Phase 0 vCard smoke tests while
// the Rust daemon's reconnect-loop TCP-RST bug (iogrid#273) is being
// fixed. It deliberately does NOT execute workloads — every assignment
// is reported FAILED. The smoke target is the full registration +
// dispatch chain (DaemonHello → CoordinatorHello → workloads-svc
// "daemon hello received" log + a dispatched assignment that lands at
// the daemon side), not customer-job success.
//
// See iogrid#215 (Phase 0 smoke) and iogrid#273 (Rust daemon TCP-RST).
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/http2"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	workloadsv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/workloads/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/workloads/v1/workloadsv1connect"
)

// stubVersion identifies the stub in log lines.
const stubVersion = "dev-stub-daemon/0.1.0"

// heartbeatInterval is how often the stub pings the coordinator. Matches
// the Rust daemon's default 5s cadence (DaemonConfig.heartbeat_secs).
const heartbeatInterval = 5 * time.Second

func main() {
	// --- flags / env -------------------------------------------------
	var (
		coordinatorURL string
		certPath       string
		keyPath        string
		providerID     string
		eligibleCSV    string
		maxConcurrent  uint
		insecureSkip   bool
	)
	flag.StringVar(&coordinatorURL, "coordinator-url", envOrDefault("IOGRID_COORDINATOR_URL", "https://api.iogrid.org"),
		"Coordinator base URL (no trailing slash). Default: $IOGRID_COORDINATOR_URL or https://api.iogrid.org")
	flag.StringVar(&certPath, "cert", envOrDefault("IOGRID_CERT_PEM", defaultStatePath("cert.pem")),
		"Path to client certificate PEM (paired identity).")
	flag.StringVar(&keyPath, "key", envOrDefault("IOGRID_KEY_PEM", defaultStatePath("key.pem")),
		"Path to client private key PEM (paired identity).")
	flag.StringVar(&providerID, "provider-id", os.Getenv("IOGRID_PROVIDER_ID"),
		"Provider UUID. Falls back to $IOGRID_PROVIDER_ID then to provider_id in ~/.iogrid/config.toml.")
	flag.StringVar(&eligibleCSV, "eligible-types", envOrDefault("IOGRID_ELIGIBLE_TYPES", "BANDWIDTH"),
		"Comma-separated WorkloadType slugs to advertise in DaemonHello (e.g. BANDWIDTH,DOCKER). Unknown values are dropped.")
	flag.UintVar(&maxConcurrent, "max-concurrent", 4, "DaemonHello.max_concurrent value.")
	flag.BoolVar(&insecureSkip, "insecure-skip-verify", boolFromEnv("IOGRID_INSECURE_SKIP_VERIFY"),
		"Skip server cert verification (dev only — also via $IOGRID_INSECURE_SKIP_VERIFY=1).")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	log = log.With(slog.String("component", stubVersion))

	if providerID == "" {
		// Try to lift it out of the paired config.toml. Failures here
		// are not fatal — the daemon may have been paired in a fresh
		// shell that exports IOGRID_PROVIDER_ID directly.
		if pid, err := readProviderIDFromConfig(defaultStatePath("config.toml")); err == nil && pid != "" {
			providerID = pid
		}
	}
	if providerID == "" {
		fatal(log, "provider_id is empty; pass --provider-id, set $IOGRID_PROVIDER_ID, or pair this host first so ~/.iogrid/config.toml has provider_id")
	}

	log.Info("startup",
		slog.String("coordinator_url", coordinatorURL),
		slog.String("cert", certPath),
		slog.String("key", keyPath),
		slog.String("provider_id", providerID),
		slog.String("eligible_types", eligibleCSV),
		slog.Uint64("max_concurrent", uint64(maxConcurrent)),
		slog.Bool("insecure_skip_verify", insecureSkip),
	)

	// --- TLS / HTTP client ------------------------------------------
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		fatal(log, "load cert/key", slog.String("error", err.Error()))
	}
	tlsCfg := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		MinVersion:         tls.VersionTLS12,
		NextProtos:         []string{"h2"},
		InsecureSkipVerify: insecureSkip, //nolint:gosec // dev-only flag
	}

	// Bidi streaming over Connect requires HTTP/2. net/http's default
	// transport will not promote a TLS connection to h2 unless the
	// caller explicitly arms it (or uses ForceAttemptHTTP2). Build a
	// dedicated http2.Transport so we know HTTP/2 is in play.
	httpClient := &http.Client{
		Transport: &http2.Transport{
			TLSClientConfig: tlsCfg,
		},
	}

	client := workloadsv1connect.NewWorkloadDispatchServiceClient(
		httpClient,
		strings.TrimRight(coordinatorURL, "/"),
		// Connect default protocol carries proto over HTTP/2 just
		// fine for bidi streams — no need for connect.WithGRPC().
	)

	// --- signal handling --------------------------------------------
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// --- open stream ------------------------------------------------
	stream := client.Dispatch(ctx)

	// Hello frame.
	hello := &workloadsv1.DispatchFrame{
		Frame: &workloadsv1.DispatchFrame_DaemonHello{
			DaemonHello: &workloadsv1.DaemonHello{
				ProviderId:    &commonv1.UUID{Value: providerID},
				EligibleTypes: parseEligibleTypes(eligibleCSV, log),
				MaxConcurrent: uint32(maxConcurrent),
			},
		},
	}
	if err := stream.Send(hello); err != nil {
		fatal(log, "send DaemonHello", slog.String("error", err.Error()))
	}
	log.Info("daemon_hello sent", slog.String("provider_id", providerID))

	// First inbound frame MUST be CoordinatorHello.
	first, err := stream.Receive()
	if err != nil {
		fatal(log, "receive CoordinatorHello", slog.String("error", err.Error()))
	}
	if ch := first.GetCoordinatorHello(); ch != nil {
		log.Info("stream opened, CoordinatorHello received",
			slog.String("coordinator_provider_id", ch.GetProviderId().GetValue()),
			slog.String("accepted_at", ch.GetAcceptedAt().AsTime().Format(time.RFC3339)),
		)
	} else {
		log.Warn("first frame was not coordinator_hello — continuing anyway", slog.Any("frame", first.GetFrame()))
	}

	// --- send loop --------------------------------------------------
	// `sendMu` protects stream.Send — Connect bidi streams are safe to
	// read-and-write concurrently across goroutines, but writes from
	// multiple goroutines must serialise.
	var sendMu sync.Mutex
	sendFrame := func(f *workloadsv1.DispatchFrame) error {
		sendMu.Lock()
		defer sendMu.Unlock()
		return stream.Send(f)
	}

	// Per-attempt tunnel state. The stub now performs real
	// TCP-over-DispatchFrame tunneling (iogrid#279): on TunnelOpen it
	// dials target_host_port directly and pumps bytes both ways.
	// TunnelData frames in either direction are keyed by attempt_id.
	tun := newTunnels(log, sendFrame)
	defer tun.closeAll()

	// Heartbeat ticker — emits a Ping (server-timestamped) every
	// heartbeatInterval, until ctx is cancelled.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(heartbeatInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				ping := &workloadsv1.DispatchFrame{
					Frame: &workloadsv1.DispatchFrame_Ping{
						Ping: timestamppb.New(time.Now().UTC()),
					},
				}
				if err := sendFrame(ping); err != nil {
					log.Warn("heartbeat send failed; bailing", slog.String("error", err.Error()))
					cancel()
					return
				}
				log.Debug("heartbeat sent")
			}
		}
	}()

	// Signal watcher — translates SIGINT/SIGTERM into a Drain + clean
	// stream close + ctx cancel.
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-ctx.Done():
			return
		case sig := <-sigCh:
			log.Info("signal received, draining", slog.String("signal", sig.String()))
			drain := &workloadsv1.DispatchFrame{
				Frame: &workloadsv1.DispatchFrame_Drain{Drain: true},
			}
			if err := sendFrame(drain); err != nil {
				log.Warn("drain send failed", slog.String("error", err.Error()))
			}
			_ = stream.CloseRequest()
			cancel()
		}
	}()

	// --- receive loop -----------------------------------------------
	// Blocks in stream.Receive(). On EOF / error we cancel ctx so the
	// heartbeat + signal goroutines exit; the deferred cancel above is
	// idempotent.
	exitCode := 0
	for {
		f, err := stream.Receive()
		if err != nil {
			if errors.Is(err, io.EOF) {
				log.Info("stream EOF; exiting")
				break
			}
			if errors.Is(err, context.Canceled) {
				log.Info("stream cancelled; exiting")
				break
			}
			// On any other error treat as a non-graceful exit so the
			// operator notices in `journalctl`. Surface code in exit
			// status.
			log.Error("stream receive error", slog.String("error", err.Error()))
			exitCode = 1
			break
		}
		switch {
		case f.GetAssignment() != nil:
			a := f.GetAssignment()
			wid := a.GetWorkload().GetId().GetValue()
			aid := a.GetAttemptId().GetValue()
			log.Info("assignment received — replying FAILED (dev-stub does not execute)",
				slog.String("workload_id", wid),
				slog.String("attempt_id", aid),
			)
			upd := &workloadsv1.DispatchFrame{
				Frame: &workloadsv1.DispatchFrame_Update{
					Update: &workloadsv1.WorkloadStatusUpdate{
						WorkloadId:      &commonv1.UUID{Value: wid},
						AttemptId:       &commonv1.UUID{Value: aid},
						Status:          workloadsv1.WorkloadStatus_WORKLOAD_STATUS_FAILED,
						ObservedAt:      timestamppb.New(time.Now().UTC()),
						Note:            "dev-stub-daemon: workload execution not implemented",
						ExitCode:        -1,
						RejectionReason: "dev_stub_no_execution",
					},
				},
			}
			if err := sendFrame(upd); err != nil {
				log.Warn("status update send failed", slog.String("error", err.Error()))
			}
		case f.GetTunnelOpen() != nil:
			to := f.GetTunnelOpen()
			aid := to.GetAttemptId().GetValue()
			target := to.GetTargetHostPort()
			log.Info("tunnel_open received — opening real TCP tunnel",
				slog.String("attempt_id", aid),
				slog.String("target", target),
			)
			tun.open(ctx, aid, target)
		case f.GetTunnelData() != nil:
			td := f.GetTunnelData()
			aid := td.GetAttemptId().GetValue()
			tun.feed(aid, td.GetPayload())
		case f.GetTunnelClose() != nil:
			tc := f.GetTunnelClose()
			aid := tc.GetAttemptId().GetValue()
			log.Info("tunnel_close received from coordinator",
				slog.String("attempt_id", aid),
				slog.String("error", tc.GetError()),
			)
			tun.close(aid, tc.GetError())
		case f.GetCancelWorkloadId() != nil:
			log.Info("cancel received", slog.String("workload_id", f.GetCancelWorkloadId().GetValue()))
		case f.GetDrain():
			log.Info("coordinator drain received; exiting receive loop")
			cancel()
		default:
			// CoordinatorHello (duplicate), Ping, etc — ignore.
		}
	}

	cancel()
	_ = stream.CloseRequest()
	_ = stream.CloseResponse()
	wg.Wait()
	log.Info("shutdown complete")
	os.Exit(exitCode)
}

// envOrDefault returns the value of key from the environment or, if
// unset/empty, the provided default.
func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// boolFromEnv returns true if the named env var is set to a truthy value.
func boolFromEnv(key string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// defaultStatePath resolves ~/.iogrid/<name> in a way that works on
// Linux + macOS. Falls back to /var/lib/iogrid if HOME is unset
// (matches the Rust daemon's DaemonConfig::default() behaviour).
func defaultStatePath(name string) string {
	if h := os.Getenv("HOME"); h != "" {
		return filepath.Join(h, ".iogrid", name)
	}
	return filepath.Join("/var/lib/iogrid", name)
}

// parseEligibleTypes converts a CSV list like "BANDWIDTH,DOCKER" into
// the WorkloadType enum slice the DaemonHello carries. Unknown slugs
// are logged + dropped (no fatal so a stale env var doesn't take the
// stub down).
func parseEligibleTypes(csv string, log *slog.Logger) []commonv1.WorkloadType {
	out := make([]commonv1.WorkloadType, 0, 4)
	for _, raw := range strings.Split(csv, ",") {
		s := strings.ToUpper(strings.TrimSpace(raw))
		if s == "" {
			continue
		}
		// Accept both bare ("BANDWIDTH") and fully-qualified
		// ("WORKLOAD_TYPE_BANDWIDTH") forms.
		key := s
		if !strings.HasPrefix(key, "WORKLOAD_TYPE_") {
			key = "WORKLOAD_TYPE_" + key
		}
		if v, ok := commonv1.WorkloadType_value[key]; ok {
			out = append(out, commonv1.WorkloadType(v))
		} else {
			log.Warn("dropping unknown eligible_type slug",
				slog.String("input", raw),
				slog.String("looked_up", key))
		}
	}
	if len(out) == 0 {
		// Default to BANDWIDTH so the smoke test always finds at
		// least one eligible type registered.
		out = append(out, commonv1.WorkloadType_WORKLOAD_TYPE_BANDWIDTH)
	}
	return out
}

// readProviderIDFromConfig scrapes the `provider_id = "..."` line out
// of the daemon's TOML config without dragging in a full TOML parser
// (we deliberately keep this binary dep-light so it builds against the
// existing internal/pb go.mod without a network fetch).
//
// Returns an empty string and nil error if the file exists but the
// key is missing — the caller will then refuse to start. Any I/O
// error is bubbled up so the operator sees the cause.
func readProviderIDFromConfig(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "provider_id") {
			continue
		}
		// provider_id = "abcd-..."   OR   provider_id="abcd-..."
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		val := strings.TrimSpace(line[eq+1:])
		val = strings.Trim(val, `"' `)
		return val, nil
	}
	return "", nil
}

// fatal logs at ERROR and exits 1.
func fatal(log *slog.Logger, msg string, args ...any) {
	log.Error(msg, args...)
	os.Exit(1)
}

// tunnelChunkSize bounds the size of a single TunnelData payload pushed
// back up the dispatch stream. Connect-RPC has its own framing on top,
// so this is purely a memory / latency knob.
const tunnelChunkSize = 32 * 1024

// tunnelDialTimeout caps how long the stub will wait when dialing the
// destination host:port. Real daemons should keep this conservative —
// proxy-gateway's relay is already deadlined.
const tunnelDialTimeout = 10 * time.Second

// tunnel is a single in-flight TCP-over-DispatchFrame attempt.
type tunnel struct {
	attemptID string
	dest      net.Conn
	closeOnce sync.Once
	done      chan struct{}
}

// tunnels owns the live tunnel map. Frames inbound from the coordinator
// fan in here keyed by attempt_id; the dialed destination connection's
// read goroutine pumps payloads back as TunnelData frames.
type tunnels struct {
	log       *slog.Logger
	sendFrame func(f *workloadsv1.DispatchFrame) error

	mu sync.Mutex
	m  map[string]*tunnel
}

func newTunnels(log *slog.Logger, send func(f *workloadsv1.DispatchFrame) error) *tunnels {
	return &tunnels{log: log, sendFrame: send, m: make(map[string]*tunnel)}
}

// open dials the target_host_port and starts the destination→coordinator
// pump goroutine. On dial failure the coordinator is informed via a
// TunnelClose carrying the error string so the proxy-gateway side
// surfaces a clean failure instead of a half-open socket.
func (t *tunnels) open(ctx context.Context, attemptID, targetHostPort string) {
	if attemptID == "" {
		t.log.Warn("tunnel_open with empty attempt_id; ignoring")
		return
	}
	if targetHostPort == "" {
		t.log.Warn("tunnel_open with empty target_host_port; closing",
			slog.String("attempt_id", attemptID))
		_ = t.sendFrame(closeFrame(attemptID, "empty target_host_port"))
		return
	}
	dialer := &net.Dialer{Timeout: tunnelDialTimeout}
	dctx, cancel := context.WithTimeout(ctx, tunnelDialTimeout)
	defer cancel()
	c, err := dialer.DialContext(dctx, "tcp", targetHostPort)
	if err != nil {
		t.log.Warn("tunnel dial failed",
			slog.String("attempt_id", attemptID),
			slog.String("target", targetHostPort),
			slog.String("error", err.Error()),
		)
		_ = t.sendFrame(closeFrame(attemptID, "dial "+targetHostPort+": "+err.Error()))
		return
	}
	tun := &tunnel{
		attemptID: attemptID,
		dest:      c,
		done:      make(chan struct{}),
	}
	t.mu.Lock()
	if existing := t.m[attemptID]; existing != nil {
		// Duplicate open — close the new one, keep the existing.
		t.mu.Unlock()
		_ = c.Close()
		t.log.Warn("duplicate tunnel_open ignored", slog.String("attempt_id", attemptID))
		return
	}
	t.m[attemptID] = tun
	t.mu.Unlock()
	t.log.Info("tunnel established",
		slog.String("attempt_id", attemptID),
		slog.String("target", targetHostPort),
	)
	go t.pumpFromDest(tun)
}

// feed writes a TunnelData payload to the destination socket.
func (t *tunnels) feed(attemptID string, payload []byte) {
	t.mu.Lock()
	tun := t.m[attemptID]
	t.mu.Unlock()
	if tun == nil {
		t.log.Debug("tunnel_data for unknown attempt; dropping",
			slog.String("attempt_id", attemptID))
		return
	}
	if _, err := tun.dest.Write(payload); err != nil {
		t.log.Debug("tunnel dest write failed",
			slog.String("attempt_id", attemptID),
			slog.String("error", err.Error()),
		)
		t.closeWithReason(tun, "dest_write_failed: "+err.Error(), true)
	}
}

// close removes a tunnel by attempt_id without sending a frame back —
// used when the coordinator sends TunnelClose first.
func (t *tunnels) close(attemptID, reason string) {
	t.mu.Lock()
	tun := t.m[attemptID]
	delete(t.m, attemptID)
	t.mu.Unlock()
	if tun == nil {
		return
	}
	t.closeWithReason(tun, reason, false)
}

// closeAll tears down every live tunnel on shutdown.
func (t *tunnels) closeAll() {
	t.mu.Lock()
	all := make([]*tunnel, 0, len(t.m))
	for _, v := range t.m {
		all = append(all, v)
	}
	t.m = map[string]*tunnel{}
	t.mu.Unlock()
	for _, tun := range all {
		t.closeWithReason(tun, "shutdown", false)
	}
}

// pumpFromDest reads bytes from the destination socket and emits
// TunnelData frames keyed by attempt_id. EOF or any read error
// terminates with a TunnelClose to the coordinator so the
// proxy-gateway-side socket flushes cleanly.
func (t *tunnels) pumpFromDest(tun *tunnel) {
	buf := make([]byte, tunnelChunkSize)
	for {
		n, err := tun.dest.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			td := &workloadsv1.DispatchFrame{
				Frame: &workloadsv1.DispatchFrame_TunnelData{
					TunnelData: &workloadsv1.TunnelData{
						AttemptId: &commonv1.UUID{Value: tun.attemptID},
						Payload:   data,
					},
				},
			}
			if sendErr := t.sendFrame(td); sendErr != nil {
				t.log.Debug("tunnel_data send failed",
					slog.String("attempt_id", tun.attemptID),
					slog.String("error", sendErr.Error()),
				)
				t.closeWithReason(tun, "send_failed: "+sendErr.Error(), true)
				return
			}
		}
		if err != nil {
			reason := ""
			if !errors.Is(err, io.EOF) {
				reason = "dest_read_failed: " + err.Error()
			}
			t.closeWithReason(tun, reason, true)
			return
		}
	}
}

// closeWithReason closes the destination socket once, optionally emits
// a TunnelClose back to the coordinator, and unregisters the tunnel.
func (t *tunnels) closeWithReason(tun *tunnel, reason string, notifyCoord bool) {
	tun.closeOnce.Do(func() {
		_ = tun.dest.Close()
		close(tun.done)
		t.mu.Lock()
		delete(t.m, tun.attemptID)
		t.mu.Unlock()
		if notifyCoord {
			_ = t.sendFrame(closeFrame(tun.attemptID, reason))
		}
	})
}

// closeFrame builds a TunnelClose DispatchFrame.
func closeFrame(attemptID, reason string) *workloadsv1.DispatchFrame {
	return &workloadsv1.DispatchFrame{
		Frame: &workloadsv1.DispatchFrame_TunnelClose{
			TunnelClose: &workloadsv1.TunnelClose{
				AttemptId: &commonv1.UUID{Value: attemptID},
				Error:     reason,
			},
		},
	}
}
