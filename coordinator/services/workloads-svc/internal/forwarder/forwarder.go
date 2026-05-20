// Package forwarder hosts the TCP-over-DispatchFrame bridge for NAT'd
// daemons (issue #222).
//
// Topology:
//
//	customer  ──TCP──▶  proxy-gateway  ──TCP──▶  workloads-svc.forwarder
//	                                                  │
//	                                          (TunnelData frames)
//	                                                  │
//	                                                  ▼
//	                                         daemon (NAT'd)  ──TCP──▶  destination
//
// The proxy-gateway dials the forwarder's listener and presents the
// dispatch attempt id as a one-line ASCII preamble:
//
//	IOGRID-TUN/1 <attempt_id> [target_host_port]\n
//
// The forwarder looks the attempt up via the dispatcher's store, finds
// the matching daemon Connection, fires a TunnelOpen frame down the
// daemon's bidi stream, then pumps bytes both directions until either
// end EOFs. Daemon-side bytes arrive via Dispatcher.DeliverTunnelData ->
// the per-attempt TunnelSink (this package's pipe struct).
//
// The forwarder side is fully testable without #221 — see
// forwarder_test.go for an in-process fake daemon Connection.
package forwarder

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/dispatcher"
)

// PreambleVersion is the ASCII protocol identifier the proxy-gateway
// writes as the first line on every forwarder connection.
const PreambleVersion = "IOGRID-TUN/1"

// MaxPreambleLen caps how many bytes the forwarder will read before it
// expects a newline. Defensive against a stuck client.
const MaxPreambleLen = 512

// PreambleTimeout bounds how long the forwarder waits for the preamble
// line before tearing the connection down.
const PreambleTimeout = 5 * time.Second

// Options configures a Forwarder.
type Options struct {
	// ListenAddr is the host:port the forwarder binds. Defaults to
	// ":9091" when zero-valued (was ":9090" — port 9090 is reserved
	// for the Prometheus /metrics listener exposed by the shared
	// bootstrap; see #267).
	ListenAddr string
	// Dispatcher is the live dispatcher; the forwarder uses it to look
	// up the daemon Connection for each attempt id.
	Dispatcher *dispatcher.D
	// Log is the structured logger; nil falls back to slog.Default().
	Log *slog.Logger
}

// Forwarder is the TCP-over-DispatchFrame bridge.
type Forwarder struct {
	opts     Options
	listener net.Listener
	wg       sync.WaitGroup
	closed   atomic.Bool
}

// New builds a Forwarder. The listener is NOT bound yet — call Start.
func New(opts Options) *Forwarder {
	if opts.ListenAddr == "" {
		opts.ListenAddr = ":9091"
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	return &Forwarder{opts: opts}
}

// Start binds the TCP listener and spawns the accept loop. Returns the
// resolved local address (useful for tests using `:0`).
func (f *Forwarder) Start(ctx context.Context) (net.Addr, error) {
	if f.opts.Dispatcher == nil {
		return nil, errors.New("forwarder: dispatcher is required")
	}
	ln, err := net.Listen("tcp", f.opts.ListenAddr)
	if err != nil {
		return nil, fmt.Errorf("forwarder: listen %s: %w", f.opts.ListenAddr, err)
	}
	f.listener = ln
	f.opts.Log.Info("tcp-over-dispatch forwarder listening",
		slog.String("addr", ln.Addr().String()))

	f.wg.Add(1)
	go f.acceptLoop(ctx)
	return ln.Addr(), nil
}

// Close stops the listener and waits for in-flight connections to drain.
func (f *Forwarder) Close() error {
	if !f.closed.CompareAndSwap(false, true) {
		return nil
	}
	var err error
	if f.listener != nil {
		err = f.listener.Close()
	}
	f.wg.Wait()
	return err
}

func (f *Forwarder) acceptLoop(ctx context.Context) {
	defer f.wg.Done()
	for {
		c, err := f.listener.Accept()
		if err != nil {
			if f.closed.Load() {
				return
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			f.opts.Log.Warn("forwarder accept error", slog.String("error", err.Error()))
			return
		}
		f.wg.Add(1)
		go func(conn net.Conn) {
			defer f.wg.Done()
			f.handleConn(ctx, conn)
		}(c)
	}
}

// handleConn is the per-connection driver: read preamble → resolve
// attempt → open daemon tunnel → pump bytes → close.
func (f *Forwarder) handleConn(ctx context.Context, c net.Conn) {
	defer c.Close()

	br := bufio.NewReaderSize(c, 32*1024)
	attemptID, targetHostPort, err := readPreamble(c, br)
	if err != nil {
		f.opts.Log.Warn("forwarder preamble error",
			slog.String("remote", c.RemoteAddr().String()),
			slog.String("error", err.Error()))
		return
	}

	providerID := f.opts.Dispatcher.LookupAssignmentProvider(ctx, attemptID)
	if providerID == "" {
		f.opts.Log.Warn("forwarder rejecting unknown attempt",
			slog.String("attempt_id", attemptID))
		return
	}
	conn := f.opts.Dispatcher.ConnectionByProviderID(providerID)
	if conn == nil || conn.SendTunnelOpen == nil || conn.SendTunnelData == nil || conn.SendTunnelClose == nil {
		f.opts.Log.Warn("forwarder daemon connection not ready",
			slog.String("attempt_id", attemptID),
			slog.String("provider_id", providerID))
		return
	}

	pipe := newTunnelPipe(c, br, conn, attemptID, f.opts.Log)
	f.opts.Dispatcher.RegisterTunnel(attemptID, pipe)
	defer f.opts.Dispatcher.UnregisterTunnel(attemptID)

	if err := conn.SendTunnelOpen(attemptID, targetHostPort); err != nil {
		f.opts.Log.Warn("forwarder tunnel_open send failed",
			slog.String("attempt_id", attemptID),
			slog.String("error", err.Error()))
		return
	}

	pipe.pumpInbound()
	pipe.waitClose()
}

// readPreamble reads the first line off the inbound connection (via the
// shared bufio.Reader passed in by handleConn) and parses it into
// (attempt_id, target_host_port). Format:
//
//	IOGRID-TUN/1 <attempt_id> [target_host_port]\n
//
// target_host_port is optional — the daemon-side router may pick the
// destination from workload context. Reads beyond the newline stay in
// the bufio.Reader so the pump can replay them.
func readPreamble(c net.Conn, br *bufio.Reader) (attemptID, targetHostPort string, err error) {
	_ = c.SetReadDeadline(time.Now().Add(PreambleTimeout))
	defer func() { _ = c.SetReadDeadline(time.Time{}) }()

	// ReadSlice caps at the bufio buffer size; we further enforce
	// MaxPreambleLen to be defensive against a stuck client.
	lineBytes, err := br.ReadSlice('\n')
	if err != nil {
		return "", "", fmt.Errorf("read preamble: %w", err)
	}
	if len(lineBytes) > MaxPreambleLen {
		return "", "", errors.New("preamble too long")
	}
	line := strings.TrimRight(string(lineBytes), "\r\n")
	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 2 || parts[0] != PreambleVersion {
		return "", "", fmt.Errorf("malformed preamble: %q", line)
	}
	attemptID = parts[1]
	if attemptID == "" {
		return "", "", errors.New("empty attempt_id in preamble")
	}
	if len(parts) == 3 {
		targetHostPort = parts[2]
	}
	return attemptID, targetHostPort, nil
}

// tunnelPipe is the per-attempt TunnelSink + io.Copy driver. Implements
// dispatcher.TunnelSink so daemon-side bytes flow back into the TCP
// socket.
type tunnelPipe struct {
	conn      net.Conn
	reader    io.Reader // bufio over conn — replays bytes buffered past preamble
	dispConn  *dispatcher.Connection
	attemptID string
	log       *slog.Logger

	closeOnce sync.Once
	done      chan struct{}
}

func newTunnelPipe(c net.Conn, r io.Reader, dispConn *dispatcher.Connection, attemptID string, log *slog.Logger) *tunnelPipe {
	return &tunnelPipe{
		conn:      c,
		reader:    r,
		dispConn:  dispConn,
		attemptID: attemptID,
		log:       log,
		done:      make(chan struct{}),
	}
}

// OnTunnelData is invoked by the dispatcher when a daemon-side TunnelData
// frame arrives. Bytes are written verbatim to the customer-facing TCP
// socket.
func (p *tunnelPipe) OnTunnelData(payload []byte) {
	if len(payload) == 0 {
		return
	}
	if _, err := p.conn.Write(payload); err != nil {
		p.log.Debug("forwarder TCP write failed",
			slog.String("attempt_id", p.attemptID),
			slog.String("error", err.Error()))
		p.closeWithReason("tcp_write_failed: " + err.Error())
	}
}

// OnTunnelClose is invoked when the daemon side signals EOF/error.
func (p *tunnelPipe) OnTunnelClose(reason string) {
	p.closeOnce.Do(func() {
		_ = p.conn.Close()
		close(p.done)
	})
	if reason != "" {
		p.log.Debug("daemon-side tunnel close",
			slog.String("attempt_id", p.attemptID),
			slog.String("reason", reason))
	}
}

// closeWithReason both closes the local side AND notifies the daemon.
func (p *tunnelPipe) closeWithReason(reason string) {
	p.closeOnce.Do(func() {
		_ = p.dispConn.SendTunnelClose(p.attemptID, reason)
		_ = p.conn.Close()
		close(p.done)
	})
}

// pumpInbound reads bytes from the TCP socket and wraps them into
// TunnelData frames bound for the daemon. Blocks until the socket EOFs
// or the daemon side signals close.
func (p *tunnelPipe) pumpInbound() {
	buf := make([]byte, 32*1024)
	for {
		n, err := p.reader.Read(buf)
		if n > 0 {
			if sendErr := p.dispConn.SendTunnelData(p.attemptID, append([]byte(nil), buf[:n]...)); sendErr != nil {
				p.log.Debug("forwarder SendTunnelData failed",
					slog.String("attempt_id", p.attemptID),
					slog.String("error", sendErr.Error()))
				p.closeWithReason("send_failed: " + sendErr.Error())
				return
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				p.closeWithReason("")
				return
			}
			// Daemon may have already closed the pipe — silent if so.
			select {
			case <-p.done:
				return
			default:
			}
			p.closeWithReason("tcp_read_failed: " + err.Error())
			return
		}
	}
}

// waitClose blocks until the tunnel is closed from either side. The
// per-connection goroutine uses this to keep the deferred Close + sink
// unregister anchored to the lifetime of both directions.
func (p *tunnelPipe) waitClose() {
	<-p.done
}
