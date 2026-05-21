// Package proxy is the top-level customer-facing entry. One TCP listener
// accepts both SOCKS5 (RFC 1928) and HTTP CONNECT clients on the same
// port; the first byte of the stream disambiguates:
//
//   - 0x05 → SOCKS5 greeting.
//   - 'C'  → HTTP "CONNECT ...".
//
// Each accepted connection runs through the same pipeline:
//
//  1. Protocol-specific handshake + credential extraction.
//  2. Auth via billing-svc.ValidateApiKey (auth.Validator).
//  3. Outbound port allow/block check (docs/LEGAL.md mandate).
//  4. antiabuse-svc.CheckUrl pre-flight (CSAM / fraud / rate limit /
//     domain class).
//  5. Sticky-session lookup → workloads-svc.SubmitWorkload dispatch
//     (with sticky_provider_hint propagated).
//  6. Dial the chosen provider tunnel endpoint.
//  7. Send the SOCKS5/HTTP "success" reply to the customer.
//  8. relay.Run bidirectional with metering every 1 MiB.
//  9. On provider drop or relay error, re-dispatch up to N times
//     (configurable; default 3) — invisible to the customer except
//     for a one-time connection reset.
//
// Everything emits structured AuditEvents to JetStream AUDIT and
// metering BillingEvents to JetStream BILLING.
package proxy

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/abuse"
	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/audit"
	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/auth"
	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/config"
	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/dispatch"
	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/httpconnect"
	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/relay"
	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/sessions"
	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/socks5"
)

// Server is the customer-facing TCP/TLS acceptor.
type Server struct {
	Config     config.Config
	Logger     *slog.Logger
	Validator  auth.Validator
	Filter     abuse.Filter
	Dispatcher dispatch.Dispatcher
	Sessions   sessions.Store
	Emitter    *audit.Emitter
	// TLSConfig — when non-nil the server terminates TLS before reading
	// SOCKS5/HTTP. The same listener flows both protocols.
	TLSConfig *tls.Config

	// Dialer is exported so tests can substitute a mock that dials
	// in-process pipes. Defaults to a 10s timeout TCP dialer.
	Dialer func(ctx context.Context, network, addr string) (net.Conn, error)

	// EnableForwarderPreamble controls whether the proxy-gateway writes
	// the IOGRID-TUN/1 preamble (issue #222 wire spec) on every freshly
	// dialed provider connection BEFORE handing the socket to relay.Run.
	//
	// In production the assignment's Endpoint points at workloads-svc's
	// TCP-over-DispatchFrame forwarder, which expects the preamble line
	// "IOGRID-TUN/1 <attempt_id> [target_host_port]\n" so it can resolve
	// the daemon Connection and open a TunnelOpen on the bidi stream.
	//
	// Local-dev mode with a StaticPool dispatcher points the endpoint at
	// a raw TCP target (e.g. an echo server in tests); writing a preamble
	// would prepend garbage to the byte stream and fail the integration
	// tests. The flag defaults to false; main.go flips it on when the
	// real workloads-svc Connect dispatcher is wired.
	//
	// Refs iogrid#279 — "forwarder rejects raw TLS bytes from
	// proxy-gateway — preamble protocol mismatch".
	EnableForwarderPreamble bool

	// listener captured for graceful shutdown.
	mu       sync.Mutex
	listener net.Listener
	closed   atomic.Bool
}

// New constructs a Server with sane defaults.
func New(cfg config.Config, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		Config: cfg,
		Logger: logger,
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := &net.Dialer{Timeout: cfg.DialTimeout}
			return d.DialContext(ctx, network, addr)
		},
	}
}

// ListenAndServe binds Config.ListenAddr (TLS if configured) and serves
// until ctx is canceled or Close is called.
func (s *Server) ListenAndServe(ctx context.Context) error {
	addr := s.Config.ListenAddr
	if addr == "" {
		addr = ":443"
	}
	var ln net.Listener
	var err error
	if s.TLSConfig != nil {
		ln, err = tls.Listen("tcp", addr, s.TLSConfig)
	} else {
		ln, err = net.Listen("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("proxy: listen %q: %w", addr, err)
	}
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()
	s.Logger.Info("proxy listener ready",
		slog.String("addr", ln.Addr().String()),
		slog.Bool("tls", s.TLSConfig != nil),
	)
	return s.Serve(ctx, ln)
}

// Serve runs the accept loop on an externally-provided listener (test seam).
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	go func() {
		<-ctx.Done()
		_ = s.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if s.closed.Load() {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return err
		}
		go s.handle(ctx, conn)
	}
}

// Close stops accepting new connections.
func (s *Server) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	s.mu.Lock()
	ln := s.listener
	s.mu.Unlock()
	if ln != nil {
		return ln.Close()
	}
	return nil
}

// Addr returns the bound address (test helper).
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// handle owns a single accepted connection through dispatch + relay.
func (s *Server) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	// Hard cap on handshake time so a slow-loris client can't sit forever.
	if dl, ok := ctx.Deadline(); !ok || time.Until(dl) > 30*time.Second {
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	}

	br := bufio.NewReader(conn)
	first, err := br.Peek(1)
	if err != nil {
		s.Logger.Debug("proxy peek failed", slog.String("error", err.Error()))
		return
	}

	clientAddr := conn.RemoteAddr().String()
	sessionID := newSessionID()
	traceID := newSessionID()
	logger := s.Logger.With(
		slog.String("session_id", sessionID),
		slog.String("client", clientAddr),
	)

	switch {
	case first[0] == 0x05:
		s.handleSocks5(ctx, conn, br, sessionID, traceID, logger)
	case first[0] == 'C' || first[0] == 'c':
		s.handleHTTPConnect(ctx, conn, br, sessionID, traceID, logger)
	default:
		logger.Warn("proxy unsupported protocol byte", slog.Int("byte", int(first[0])))
		s.emitAudit(ctx, audit.AuditEvent{
			EventKind:  "rejected",
			Protocol:   "unknown",
			ClientAddr: clientAddr,
			SessionID:  sessionID,
			Reason:     "unknown_protocol",
			TraceID:    traceID,
		})
	}
}

// handleSocks5 owns the SOCKS5 handshake → dispatch → relay path.
func (s *Server) handleSocks5(ctx context.Context, conn net.Conn, br *bufio.Reader, sessionID, traceID string, logger *slog.Logger) {
	rw := &readerWriter{r: br, w: conn}
	method, err := socks5.Greet(rw)
	if err != nil || method != socks5.AuthUserPass {
		logger.Info("socks5 greet refused", slog.String("error", errString(err)))
		s.emitAudit(ctx, audit.AuditEvent{
			EventKind:  "rejected",
			Protocol:   "socks5",
			ClientAddr: conn.RemoteAddr().String(),
			SessionID:  sessionID,
			Reason:     "auth_method_missing",
			TraceID:    traceID,
		})
		return
	}
	creds, err := socks5.ReadCredentials(rw)
	if err != nil {
		logger.Info("socks5 read credentials failed", slog.String("error", err.Error()))
		return
	}
	_, apiKey, ok := auth.SplitUserPass(creds.Username, creds.Password)
	if !ok {
		_ = socks5.WriteAuthStatus(rw, false)
		s.emitAudit(ctx, audit.AuditEvent{
			EventKind:  "rejected",
			Protocol:   "socks5",
			ClientAddr: conn.RemoteAddr().String(),
			SessionID:  sessionID,
			Reason:     "credentials_empty",
			TraceID:    traceID,
		})
		return
	}
	customer, err := s.validate(ctx, apiKey)
	if err != nil {
		_ = socks5.WriteAuthStatus(rw, false)
		logger.Info("socks5 auth rejected", slog.String("error", err.Error()))
		s.emitAudit(ctx, audit.AuditEvent{
			EventKind:  "rejected",
			Protocol:   "socks5",
			ClientAddr: conn.RemoteAddr().String(),
			SessionID:  sessionID,
			Reason:     authReason(err),
			TraceID:    traceID,
		})
		return
	}
	if err := socks5.WriteAuthStatus(rw, true); err != nil {
		return
	}
	req, replyCode, err := socks5.ReadConnectRequest(rw)
	if err != nil || replyCode != socks5.ReplySucceeded {
		_ = socks5.WriteReply(rw, replyCode, nil)
		logger.Info("socks5 read connect failed", slog.String("error", errString(err)))
		s.emitAudit(ctx, audit.AuditEvent{
			EventKind:   "rejected",
			Protocol:    "socks5",
			ClientAddr:  conn.RemoteAddr().String(),
			SessionID:   sessionID,
			CustomerID:  customer.CustomerID,
			WorkspaceID: customer.WorkspaceID,
			Reason:      "connect_parse_failed",
			TraceID:     traceID,
		})
		return
	}
	destination := req.String()

	// Look up any sticky-session binding BEFORE dispatch so that
	// abuse_flagged audit events can be attributed to the would-be
	// provider — the transparency feed needs a provider_id to surface
	// the entry, and the dispatcher would have picked the sticky
	// provider next anyway. If no binding exists the event still lands
	// in the AUDIT stream for legal retention; per-provider feeds
	// simply don't see this customer's first-block-ever.
	stickyProviderID := ""
	if s.Sessions != nil {
		if b, err := s.Sessions.Get(ctx, customer.CustomerID, destination); err == nil {
			stickyProviderID = b.ProviderID
		}
	}

	if !s.portAllowed(int(req.Port)) {
		_ = socks5.WriteReply(rw, socks5.ReplyConnNotAllowed, nil)
		s.emitAudit(ctx, audit.AuditEvent{
			EventKind:   "abuse_flagged",
			Protocol:    "socks5",
			ClientAddr:  conn.RemoteAddr().String(),
			SessionID:   sessionID,
			CustomerID:  customer.CustomerID,
			WorkspaceID: customer.WorkspaceID,
			ProviderID:  stickyProviderID,
			Destination: destination,
			Reason:      "outbound_port_blocked",
			Decision:    "block",
			TraceID:     traceID,
		})
		return
	}

	// Pre-flight anti-abuse check (after we know dest host + port).
	verdict, _ := s.preflight(ctx, customer, req.Host, uint32(req.Port), traceID)
	if verdict.Decision == abuse.DecisionBlock || verdict.Decision == abuse.DecisionRateLimit {
		_ = socks5.WriteReply(rw, socks5.ReplyGeneralFailure, nil)
		s.emitAudit(ctx, audit.AuditEvent{
			EventKind:   "abuse_flagged",
			Protocol:    "socks5",
			ClientAddr:  conn.RemoteAddr().String(),
			SessionID:   sessionID,
			CustomerID:  customer.CustomerID,
			WorkspaceID: customer.WorkspaceID,
			ProviderID:  stickyProviderID,
			Destination: destination,
			Reason:      verdict.Reason,
			Decision:    "block",
			TraceID:     traceID,
		})
		return
	}

	asg, providerConn, err := s.dialWithFailover(ctx, customer, destination, req.Host, uint32(req.Port), sessionID, logger)
	if err != nil {
		code := byte(socks5.ReplyHostUnreachable)
		if errors.Is(err, dispatch.ErrNoEligibleProvider) {
			code = socks5.ReplyGeneralFailure
		}
		_ = socks5.WriteReply(rw, code, nil)
		s.emitAudit(ctx, audit.AuditEvent{
			EventKind:   "rejected",
			Protocol:    "socks5",
			ClientAddr:  conn.RemoteAddr().String(),
			SessionID:   sessionID,
			CustomerID:  customer.CustomerID,
			WorkspaceID: customer.WorkspaceID,
			Destination: destination,
			Reason:      "dispatch_failed:" + err.Error(),
			Decision:    "block",
			TraceID:     traceID,
		})
		return
	}
	defer providerConn.Close()

	if err := socks5.WriteReply(rw, socks5.ReplySucceeded, providerConn.LocalAddr()); err != nil {
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	_ = conn.SetWriteDeadline(time.Time{})

	s.emitAudit(ctx, audit.AuditEvent{
		EventKind:   "relay_started",
		Protocol:    "socks5",
		ClientAddr:  conn.RemoteAddr().String(),
		SessionID:   sessionID,
		CustomerID:  customer.CustomerID,
		WorkspaceID: customer.WorkspaceID,
		ProviderID:  asg.ProviderID,
		Destination: destination,
		Decision:    "allow",
		Reason:      verdict.Reason,
		TraceID:     traceID,
	})

	counters := s.runRelay(ctx, conn, providerConn, customer, asg, sessionID)
	s.emitAudit(ctx, audit.AuditEvent{
		EventKind:   "relay_ended",
		Protocol:    "socks5",
		ClientAddr:  conn.RemoteAddr().String(),
		SessionID:   sessionID,
		CustomerID:  customer.CustomerID,
		WorkspaceID: customer.WorkspaceID,
		ProviderID:  asg.ProviderID,
		Destination: destination,
		TraceID:     traceID,
		Metadata: map[string]string{
			"bytes_in":  fmt.Sprintf("%d", counters.BytesIn),
			"bytes_out": fmt.Sprintf("%d", counters.BytesOut),
			"duration":  counters.Duration.String(),
		},
	})
}

// handleHTTPConnect owns the HTTP CONNECT handshake → dispatch → relay path.
func (s *Server) handleHTTPConnect(ctx context.Context, conn net.Conn, br *bufio.Reader, sessionID, traceID string, logger *slog.Logger) {
	req, err := httpconnect.ReadRequest(br)
	if err != nil {
		_ = httpconnect.WriteError(conn, 400, "Bad Request")
		s.emitAudit(ctx, audit.AuditEvent{
			EventKind:  "rejected",
			Protocol:   "http_connect",
			ClientAddr: conn.RemoteAddr().String(),
			SessionID:  sessionID,
			Reason:     "parse_failed",
			TraceID:    traceID,
		})
		return
	}
	if req.APIKey == "" {
		_ = httpconnect.WriteError(conn, 407, "Proxy Authentication Required")
		s.emitAudit(ctx, audit.AuditEvent{
			EventKind:   "rejected",
			Protocol:    "http_connect",
			ClientAddr:  conn.RemoteAddr().String(),
			SessionID:   sessionID,
			Reason:      "missing_proxy_auth",
			Destination: req.Target(),
			TraceID:     traceID,
		})
		return
	}
	customer, err := s.validate(ctx, req.APIKey)
	if err != nil {
		_ = httpconnect.WriteError(conn, 407, "Proxy Authentication Required")
		logger.Info("http_connect auth rejected", slog.String("error", err.Error()))
		s.emitAudit(ctx, audit.AuditEvent{
			EventKind:   "rejected",
			Protocol:    "http_connect",
			ClientAddr:  conn.RemoteAddr().String(),
			SessionID:   sessionID,
			Reason:      authReason(err),
			Destination: req.Target(),
			TraceID:     traceID,
		})
		return
	}

	// Sticky-binding peek — see SOCKS5 path for rationale.
	stickyProviderID := ""
	if s.Sessions != nil {
		if b, err := s.Sessions.Get(ctx, customer.CustomerID, req.Target()); err == nil {
			stickyProviderID = b.ProviderID
		}
	}

	if !s.portAllowed(int(req.Port)) {
		_ = httpconnect.WriteError(conn, 403, "Forbidden")
		s.emitAudit(ctx, audit.AuditEvent{
			EventKind:   "abuse_flagged",
			Protocol:    "http_connect",
			ClientAddr:  conn.RemoteAddr().String(),
			SessionID:   sessionID,
			CustomerID:  customer.CustomerID,
			WorkspaceID: customer.WorkspaceID,
			ProviderID:  stickyProviderID,
			Destination: req.Target(),
			Reason:      "outbound_port_blocked",
			Decision:    "block",
			TraceID:     traceID,
		})
		return
	}

	verdict, _ := s.preflight(ctx, customer, req.Host, uint32(req.Port), traceID)
	if verdict.Decision == abuse.DecisionBlock {
		_ = httpconnect.WriteError(conn, 403, "Forbidden")
		s.emitAudit(ctx, audit.AuditEvent{
			EventKind:   "abuse_flagged",
			Protocol:    "http_connect",
			ClientAddr:  conn.RemoteAddr().String(),
			SessionID:   sessionID,
			CustomerID:  customer.CustomerID,
			WorkspaceID: customer.WorkspaceID,
			ProviderID:  stickyProviderID,
			Destination: req.Target(),
			Reason:      verdict.Reason,
			Decision:    "block",
			TraceID:     traceID,
		})
		return
	}
	if verdict.Decision == abuse.DecisionRateLimit {
		_ = httpconnect.WriteError(conn, 429, "Too Many Requests")
		s.emitAudit(ctx, audit.AuditEvent{
			EventKind:   "abuse_flagged",
			Protocol:    "http_connect",
			ClientAddr:  conn.RemoteAddr().String(),
			SessionID:   sessionID,
			CustomerID:  customer.CustomerID,
			WorkspaceID: customer.WorkspaceID,
			ProviderID:  stickyProviderID,
			Destination: req.Target(),
			Reason:      verdict.Reason,
			Decision:    "block",
			TraceID:     traceID,
		})
		return
	}

	asg, providerConn, err := s.dialWithFailover(ctx, customer, req.Target(), req.Host, uint32(req.Port), sessionID, logger)
	if err != nil {
		_ = httpconnect.WriteError(conn, 502, "Bad Gateway")
		s.emitAudit(ctx, audit.AuditEvent{
			EventKind:   "rejected",
			Protocol:    "http_connect",
			ClientAddr:  conn.RemoteAddr().String(),
			SessionID:   sessionID,
			CustomerID:  customer.CustomerID,
			WorkspaceID: customer.WorkspaceID,
			Destination: req.Target(),
			Reason:      "dispatch_failed:" + err.Error(),
			Decision:    "block",
			TraceID:     traceID,
		})
		return
	}
	defer providerConn.Close()

	if err := httpconnect.WriteEstablished(conn); err != nil {
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	_ = conn.SetWriteDeadline(time.Time{})

	s.emitAudit(ctx, audit.AuditEvent{
		EventKind:   "relay_started",
		Protocol:    "http_connect",
		ClientAddr:  conn.RemoteAddr().String(),
		SessionID:   sessionID,
		CustomerID:  customer.CustomerID,
		WorkspaceID: customer.WorkspaceID,
		ProviderID:  asg.ProviderID,
		Destination: req.Target(),
		Decision:    "allow",
		Reason:      verdict.Reason,
		TraceID:     traceID,
	})

	counters := s.runRelay(ctx, conn, providerConn, customer, asg, sessionID)
	s.emitAudit(ctx, audit.AuditEvent{
		EventKind:   "relay_ended",
		Protocol:    "http_connect",
		ClientAddr:  conn.RemoteAddr().String(),
		SessionID:   sessionID,
		CustomerID:  customer.CustomerID,
		WorkspaceID: customer.WorkspaceID,
		ProviderID:  asg.ProviderID,
		Destination: req.Target(),
		TraceID:     traceID,
		Metadata: map[string]string{
			"bytes_in":  fmt.Sprintf("%d", counters.BytesIn),
			"bytes_out": fmt.Sprintf("%d", counters.BytesOut),
			"duration":  counters.Duration.String(),
		},
	})
}

// validate runs the api-key lookup. Returns an *auth.Customer or an error.
func (s *Server) validate(ctx context.Context, apiKey string) (*auth.Customer, error) {
	if s.Validator == nil {
		return nil, auth.ErrInvalidKey
	}
	c, err := s.Validator.Validate(ctx, apiKey)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, auth.ErrInvalidKey
	}
	return c, nil
}

// preflight runs the antiabuse-svc.CheckUrl pre-flight check.
//
// Fail-mode policy is EXPLICIT (per the §3.3 anti-pattern catalog: a
// filter that fails open must declare it):
//
//   - Default = fail CLOSED. RPC error / timeout / empty verdict →
//     DecisionBlock with reason="antiabuse_unavailable". This is the
//     docs/LEGAL.md mandate — a control-plane outage MUST NOT silently
//     disable the legal-defence kill switch (issue #360).
//   - Opt-in fail OPEN via ANTIABUSE_FAIL_OPEN=true. Operators flip
//     this during a declared antiabuse-svc incident to keep the data
//     plane flowing; every fail-open is still audited so the
//     transparency feed records the gap.
//
// Either way the returned Verdict.Decision is one of Allow / Review /
// Block / RateLimit — DecisionError is collapsed inside this function.
func (s *Server) preflight(ctx context.Context, c *auth.Customer, host string, port uint32, traceID string) (abuse.Verdict, error) {
	if s.Filter == nil {
		return abuse.Verdict{Decision: abuse.DecisionAllow, Reason: "allow_no_filter"}, nil
	}
	category := ""
	if len(c.AllowedCategories) > 0 {
		category = c.AllowedCategories[0]
	}
	v, err := s.Filter.Check(ctx, abuse.CheckInput{
		CustomerID:  c.CustomerID,
		WorkspaceID: c.WorkspaceID,
		Category:    category,
		URL:         fmt.Sprintf("https://%s:%d/", host, port),
		Host:        host,
		Port:        port,
		TraceID:     traceID,
	})
	if v.Decision == abuse.DecisionError {
		if s.Config.AntiabuseFailOpen {
			s.Logger.Warn("antiabuse-svc unreachable; failing OPEN per ANTIABUSE_FAIL_OPEN",
				slog.String("reason", v.Reason),
				slog.String("trace_id", traceID),
				slog.String("destination", fmt.Sprintf("%s:%d", host, port)),
			)
			return abuse.Verdict{Decision: abuse.DecisionAllow, Reason: "antiabuse_fail_open"}, err
		}
		s.Logger.Warn("antiabuse-svc unreachable; failing CLOSED (default)",
			slog.String("reason", v.Reason),
			slog.String("trace_id", traceID),
			slog.String("destination", fmt.Sprintf("%s:%d", host, port)),
		)
		return abuse.Verdict{Decision: abuse.DecisionBlock, Reason: "antiabuse_unavailable"}, err
	}
	return v, err
}

// portAllowed checks the configured port allow/block lists.
func (s *Server) portAllowed(port int) bool {
	if port <= 0 || port > 65535 {
		return false
	}
	if len(s.Config.AllowPorts) > 0 {
		for _, p := range s.Config.AllowPorts {
			if p == port {
				return true
			}
		}
		return false
	}
	for _, p := range s.Config.BlockPorts {
		if p == port {
			return false
		}
	}
	return true
}

// dialWithFailover walks dispatch + dial. Tries up to MaxFailoverAttempts.
func (s *Server) dialWithFailover(ctx context.Context, c *auth.Customer, destination, host string, port uint32, sessionID string, logger *slog.Logger) (*dispatch.Assignment, net.Conn, error) {
	max := s.Config.MaxFailoverAttempts
	if max <= 0 {
		max = 3
	}
	excluded := map[string]struct{}{}

	// Honour sticky binding if available.
	sticky := ""
	if s.Sessions != nil && c != nil {
		if b, err := s.Sessions.Get(ctx, c.CustomerID, destination); err == nil {
			sticky = b.ProviderID
		}
	}

	for attempt := 0; attempt < max; attempt++ {
		req := dispatch.Request{
			CustomerID:       c.CustomerID,
			WorkspaceID:      c.WorkspaceID,
			SessionID:        sessionID,
			GeoTarget:        c.GeoTarget,
			DestinationHost:  host,
			DestinationPort:  port,
			Category:         firstCategory(c),
			StickyProviderID: sticky,
			Excluded:         excluded,
		}
		asg, err := s.Dispatcher.Dispatch(ctx, req)
		if err != nil {
			return nil, nil, err
		}
		dialCtx, cancel := context.WithTimeout(ctx, s.dialTimeout())
		conn, dialErr := s.Dialer(dialCtx, "tcp", asg.Endpoint)
		cancel()
		if dialErr == nil {
			// Wire-spec preamble (issue #222) — write the
			// "IOGRID-TUN/1 <attempt_id> <host>:<port>\n" line BEFORE
			// the relay starts pumping the customer's raw TLS bytes.
			// Without this the workloads-svc forwarder rejects with
			// "malformed preamble" the moment it sees \x16\x03 (the
			// TLS handshake). Refs iogrid#279.
			if s.EnableForwarderPreamble && asg.AttemptID != "" {
				if pErr := writeForwarderPreamble(conn, asg.AttemptID, host, port, s.dialTimeout()); pErr != nil {
					logger.Warn("forwarder preamble write failed; failing over",
						slog.String("provider_id", asg.ProviderID),
						slog.String("endpoint", asg.Endpoint),
						slog.String("error", pErr.Error()),
						slog.Int("attempt", attempt+1),
					)
					_ = conn.Close()
					s.emitAudit(ctx, audit.AuditEvent{
						EventKind:   "failover",
						Protocol:    "internal",
						SessionID:   sessionID,
						CustomerID:  c.CustomerID,
						WorkspaceID: c.WorkspaceID,
						ProviderID:  asg.ProviderID,
						Destination: destination,
						Reason:      "preamble_write_failed",
						Metadata: map[string]string{
							"error":   pErr.Error(),
							"attempt": fmt.Sprintf("%d", attempt+1),
						},
					})
					excluded[asg.ProviderID] = struct{}{}
					if s.Sessions != nil {
						_ = s.Sessions.Invalidate(ctx, c.CustomerID, destination)
					}
					sticky = ""
					continue
				}
			}
			if s.Sessions != nil {
				_ = s.Sessions.Put(ctx, sessions.Binding{
					CustomerID:  c.CustomerID,
					Destination: destination,
					ProviderID:  asg.ProviderID,
				})
			}
			return asg, conn, nil
		}
		logger.Warn("provider dial failed; failing over",
			slog.String("provider_id", asg.ProviderID),
			slog.String("endpoint", asg.Endpoint),
			slog.String("error", dialErr.Error()),
			slog.Int("attempt", attempt+1),
		)
		// Audit the failover.
		s.emitAudit(ctx, audit.AuditEvent{
			EventKind:   "failover",
			Protocol:    "internal",
			SessionID:   sessionID,
			CustomerID:  c.CustomerID,
			WorkspaceID: c.WorkspaceID,
			ProviderID:  asg.ProviderID,
			Destination: destination,
			Reason:      "provider_dial_failed",
			Metadata: map[string]string{
				"error":   dialErr.Error(),
				"attempt": fmt.Sprintf("%d", attempt+1),
			},
		})
		excluded[asg.ProviderID] = struct{}{}
		// Invalidate sticky binding so we don't keep picking the dead one.
		if s.Sessions != nil {
			_ = s.Sessions.Invalidate(ctx, c.CustomerID, destination)
		}
		sticky = ""
	}
	return nil, nil, fmt.Errorf("proxy: %d failover attempts exhausted", max)
}

// writeForwarderPreamble writes the IOGRID-TUN/1 preamble line on a
// freshly-dialed provider connection. Format:
//
//	IOGRID-TUN/1 <attempt_id> <host>:<port>\n
//
// The trailing newline is REQUIRED — the forwarder's bufio reader uses
// it as the framing delimiter (see workloads-svc forwarder.go
// readPreamble). The target host:port is informational; the daemon can
// also pick the destination from the workload context.
//
// A short write deadline guards against a wedged forwarder accepting
// the TCP socket but never reading. timeout 0 disables the deadline.
//
// Refs iogrid#279.
func writeForwarderPreamble(c net.Conn, attemptID, host string, port uint32, timeout time.Duration) error {
	if timeout > 0 {
		_ = c.SetWriteDeadline(time.Now().Add(timeout))
		defer func() { _ = c.SetWriteDeadline(time.Time{}) }()
	}
	target := ""
	if host != "" && port > 0 {
		target = fmt.Sprintf(" %s:%d", host, port)
	}
	line := fmt.Sprintf("IOGRID-TUN/1 %s%s\n", attemptID, target)
	_, err := c.Write([]byte(line))
	return err
}

// runRelay runs the bidirectional copy + metering.
func (s *Server) runRelay(ctx context.Context, customer, provider net.Conn, c *auth.Customer, asg *dispatch.Assignment, sessionID string) relay.Counters {
	counters, _ := relay.Run(ctx, customer, provider, relay.Options{
		MeterEvery:  s.Config.MeterBytesEvery,
		IdleTimeout: s.Config.IdleTimeout,
		Meter: func(ctx context.Context, bytesIn, bytesOut uint64) error {
			return s.emitBilling(ctx, audit.BillingEvent{
				CustomerID:  c.CustomerID,
				WorkspaceID: c.WorkspaceID,
				ProviderID:  asg.ProviderID,
				BytesIn:     bytesIn,
				BytesOut:    bytesOut,
				SessionID:   sessionID,
				WorkloadID:  asg.WorkloadID,
			})
		},
	})
	return counters
}

func (s *Server) dialTimeout() time.Duration {
	if s.Config.DialTimeout > 0 {
		return s.Config.DialTimeout
	}
	return 10 * time.Second
}

func (s *Server) emitAudit(ctx context.Context, ev audit.AuditEvent) {
	if s.Emitter == nil {
		return
	}
	_ = s.Emitter.EmitAudit(ctx, ev)
}

func (s *Server) emitBilling(ctx context.Context, ev audit.BillingEvent) error {
	if s.Emitter == nil {
		return nil
	}
	return s.Emitter.EmitBilling(ctx, ev)
}

// newSessionID returns a 16-hex-char random id.
func newSessionID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func authReason(err error) string {
	switch {
	case errors.Is(err, auth.ErrInvalidKey):
		return "invalid_api_key"
	case errors.Is(err, auth.ErrSuspended):
		return "workspace_suspended"
	}
	return "auth_failed"
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func firstCategory(c *auth.Customer) string {
	if c == nil || len(c.AllowedCategories) == 0 {
		return ""
	}
	return strings.ToLower(c.AllowedCategories[0])
}

// readerWriter adapts a (Reader, Writer) pair to io.ReadWriter so the
// SOCKS5 functions can consume from a bufio.Reader while writing directly
// to the underlying net.Conn.
type readerWriter struct {
	r interface{ Read(p []byte) (int, error) }
	w interface{ Write(p []byte) (int, error) }
}

func (rw *readerWriter) Read(p []byte) (int, error)  { return rw.r.Read(p) }
func (rw *readerWriter) Write(p []byte) (int, error) { return rw.w.Write(p) }
