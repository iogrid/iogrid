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
//       - every 5s, sends a Ping (server-timestamped liveness frame),
//       - on inbound Assignment frames, replies with a synthetic
//         WorkloadStatusUpdate{status: WORKLOAD_STATUS_FAILED, note:
//         "dev-stub: not executed"} so the dispatcher sees the attempt
//         was "tried" and can retry / time-out gracefully,
//       - on inbound TunnelOpen frames, immediately answers with a
//         TunnelClose{error: "dev-stub: tunneling not implemented"} so
//         the proxy-gateway forwarder unblocks.
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
			log.Info("tunnel_open received — replying TunnelClose (dev-stub does not tunnel)",
				slog.String("attempt_id", aid),
				slog.String("target", to.GetTargetHostPort()),
			)
			tc := &workloadsv1.DispatchFrame{
				Frame: &workloadsv1.DispatchFrame_TunnelClose{
					TunnelClose: &workloadsv1.TunnelClose{
						AttemptId: &commonv1.UUID{Value: aid},
						Error:     "dev-stub-daemon: tunneling not implemented",
					},
				},
			}
			if err := sendFrame(tc); err != nil {
				log.Warn("tunnel_close send failed", slog.String("error", err.Error()))
			}
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
