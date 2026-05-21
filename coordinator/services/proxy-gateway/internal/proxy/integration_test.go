package proxy_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/abuse"
	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/audit"
	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/auth"
	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/config"
	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/dispatch"
	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/proxy"
	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/sessions"
)

// startEchoServer spins up a TCP echo server that returns the bytes the
// client sends, prefixed with a marker so the test can distinguish
// origin. Returns the bound address and a cleanup func.
func startEchoServer(t *testing.T, marker string) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	stop := make(chan struct{})
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = c.Write([]byte(marker))
				_, _ = io.Copy(c, c)
			}(c)
		}
	}()
	cleanup := func() {
		close(stop)
		_ = ln.Close()
	}
	return ln.Addr().String(), cleanup
}

// The audit.Emitter doesn't expose hooks for in-test event capture, so
// the integration tests below focus on observable wire effects (relay
// byte counts, sticky-session pinning, failover behaviour, error
// status codes) and rely on the unit tests in internal/audit + internal/relay
// for emitter coverage.

func buildServer(t *testing.T, pool *dispatch.StaticPool, filter abuse.Filter) (*proxy.Server, net.Listener) {
	t.Helper()
	cfg := config.Defaults()
	cfg.ListenAddr = "127.0.0.1:0"
	cfg.MeterBytesEvery = 1024
	cfg.IdleTimeout = 0
	cfg.DialTimeout = 2 * time.Second
	cfg.MaxFailoverAttempts = 3

	srv := proxy.New(cfg, nil)
	srv.Validator = auth.NewStatic(map[string]auth.Customer{
		"sk_live_abc": {WorkspaceID: "ws-1", CustomerID: "cust-1", Tier: "starter"},
	})
	srv.Filter = filter
	srv.Dispatcher = pool
	srv.Sessions = sessions.NewMemory(time.Minute)
	srv.Emitter = audit.New(context.Background(), audit.Options{}) // slog fallback

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return srv, ln
}

// TestSocks5_EndToEnd verifies the SOCKS5 path with auth + relay.
func TestSocks5_EndToEnd(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t, "GREET/")
	defer stopEcho()

	pool := dispatch.NewStaticPool([]dispatch.ProviderEntry{
		{ID: "prov-1", Endpoint: echoAddr, Online: true},
	})
	srv, ln := buildServer(t, pool, &abuse.StaticFilter{Verdict: abuse.Verdict{Decision: abuse.DecisionAllow}})
	defer ln.Close()
	go srv.Serve(context.Background(), ln) //nolint:errcheck

	c, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer c.Close()

	// 1. Greeting: VER=5, NMETHODS=1, AuthUserPass.
	if _, err := c.Write([]byte{0x05, 0x01, 0x02}); err != nil {
		t.Fatalf("write greet: %v", err)
	}
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(c, hdr); err != nil {
		t.Fatalf("read greet reply: %v", err)
	}
	if hdr[0] != 0x05 || hdr[1] != 0x02 {
		t.Fatalf("greet reply = %v", hdr)
	}

	// 2. RFC 1929 user/pass: user="ws", pass="sk_live_abc".
	user := []byte("ws")
	pass := []byte("sk_live_abc")
	authReq := []byte{0x01, byte(len(user))}
	authReq = append(authReq, user...)
	authReq = append(authReq, byte(len(pass)))
	authReq = append(authReq, pass...)
	if _, err := c.Write(authReq); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	authResp := make([]byte, 2)
	if _, err := io.ReadFull(c, authResp); err != nil {
		t.Fatalf("read auth resp: %v", err)
	}
	if authResp[0] != 0x01 || authResp[1] != 0x00 {
		t.Fatalf("auth resp = %v", authResp)
	}

	// 3. CONNECT to a domain.
	host := "echoserver.test"
	connReq := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	connReq = append(connReq, []byte(host)...)
	connReq = append(connReq, 0x01, 0xbb) // port 443
	if _, err := c.Write(connReq); err != nil {
		t.Fatalf("write connect: %v", err)
	}
	// Reply: VER REP RSV ATYP+addr+port.
	reply := make([]byte, 10)
	if _, err := io.ReadFull(c, reply); err != nil {
		t.Fatalf("read connect reply: %v", err)
	}
	if reply[0] != 0x05 || reply[1] != 0x00 {
		t.Fatalf("connect reply = %v", reply)
	}

	// 4. Relay: send some bytes; expect echo back (prefixed by "GREET/" marker).
	if _, err := c.Write([]byte("ping-1")); err != nil {
		t.Fatalf("relay write: %v", err)
	}
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 64)
	n, err := c.Read(buf)
	if err != nil {
		t.Fatalf("relay read: %v", err)
	}
	got := string(buf[:n])
	if !strings.Contains(got, "GREET/") {
		t.Fatalf("expected greeting marker; got %q", got)
	}
	// drain echo
	c.SetReadDeadline(time.Now().Add(time.Second))
	_, _ = c.Read(buf)
}

// TestSocks5_AuthRejected verifies that a bad password closes the
// connection after sub-auth-status=0x01.
func TestSocks5_AuthRejected(t *testing.T) {
	pool := dispatch.NewStaticPool(nil)
	srv, ln := buildServer(t, pool, &abuse.StaticFilter{Verdict: abuse.Verdict{Decision: abuse.DecisionAllow}})
	defer ln.Close()
	go srv.Serve(context.Background(), ln) //nolint:errcheck

	c, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	_, _ = c.Write([]byte{0x05, 0x01, 0x02})
	io.ReadFull(c, make([]byte, 2)) //nolint:errcheck

	user := []byte("x")
	pass := []byte("wrong-key")
	req := []byte{0x01, byte(len(user))}
	req = append(req, user...)
	req = append(req, byte(len(pass)))
	req = append(req, pass...)
	_, _ = c.Write(req)
	resp := make([]byte, 2)
	io.ReadFull(c, resp) //nolint:errcheck
	if resp[1] != 0x01 {
		t.Fatalf("expected auth denied byte 0x01, got 0x%02x", resp[1])
	}
}

// TestSocks5_BlockedDestination — antiabuse returns BLOCK; proxy must
// emit ReplyConnNotAllowed (0x02) and close.
func TestSocks5_BlockedDestination(t *testing.T) {
	pool := dispatch.NewStaticPool([]dispatch.ProviderEntry{
		{ID: "prov-1", Endpoint: "127.0.0.1:1", Online: true},
	})
	srv, ln := buildServer(t, pool, &abuse.StaticFilter{Verdict: abuse.Verdict{Decision: abuse.DecisionBlock, Reason: "phishtank_listed"}})
	defer ln.Close()
	go srv.Serve(context.Background(), ln) //nolint:errcheck

	c, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	_, _ = c.Write([]byte{0x05, 0x01, 0x02})
	io.ReadFull(c, make([]byte, 2)) //nolint:errcheck
	// auth
	req := []byte{0x01, 0x02, 'w', 's', byte(len("sk_live_abc"))}
	req = append(req, []byte("sk_live_abc")...)
	_, _ = c.Write(req)
	io.ReadFull(c, make([]byte, 2)) //nolint:errcheck
	// CONNECT
	host := "bad.example"
	connReq := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	connReq = append(connReq, []byte(host)...)
	connReq = append(connReq, 0x01, 0xbb)
	_, _ = c.Write(connReq)
	reply := make([]byte, 10)
	io.ReadFull(c, reply) //nolint:errcheck
	// Issue #360: antiabuse-flagged destinations get
	// ReplyGeneralFailure (0x01), distinct from port-block 0x02 —
	// so customer-side libraries can tell "policy denial" from
	// "antiabuse kill switch fired".
	if reply[1] != 0x01 {
		t.Fatalf("expected GeneralFailure (0x01); got 0x%02x", reply[1])
	}
}

// TestSocks5_AntiabuseUnavailable_FailsClosed — when the antiabuse
// filter returns DecisionError (RPC unreachable), the default policy
// is fail-closed: the proxy must emit ReplyGeneralFailure (0x01) and
// audit-tag the reason as antiabuse_unavailable. This is the legal-
// defence kill switch invariant from docs/LEGAL.md (issue #360).
func TestSocks5_AntiabuseUnavailable_FailsClosed(t *testing.T) {
	pool := dispatch.NewStaticPool([]dispatch.ProviderEntry{
		{ID: "prov-1", Endpoint: "127.0.0.1:1", Online: true},
	})
	srv, ln := buildServer(t, pool, &abuse.StaticFilter{
		Verdict: abuse.Verdict{Decision: abuse.DecisionError, Reason: "antiabuse_unavailable"},
		Err:     errors.New("rpc timeout"),
	})
	// Explicit default: AntiabuseFailOpen is false.
	defer ln.Close()
	go srv.Serve(context.Background(), ln) //nolint:errcheck

	c, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	_, _ = c.Write([]byte{0x05, 0x01, 0x02})
	io.ReadFull(c, make([]byte, 2)) //nolint:errcheck
	r := []byte{0x01, 0x02, 'w', 's', byte(len("sk_live_abc"))}
	r = append(r, []byte("sk_live_abc")...)
	_, _ = c.Write(r)
	io.ReadFull(c, make([]byte, 2)) //nolint:errcheck
	host := "any.example"
	connReq := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	connReq = append(connReq, []byte(host)...)
	connReq = append(connReq, 0x01, 0xbb)
	_, _ = c.Write(connReq)
	reply := make([]byte, 10)
	io.ReadFull(c, reply) //nolint:errcheck
	if reply[1] != 0x01 {
		t.Fatalf("fail-closed: expected GeneralFailure (0x01); got 0x%02x", reply[1])
	}
}

// TestSocks5_AntiabuseUnavailable_FailsOpenWhenConfigured — operators
// can flip ANTIABUSE_FAIL_OPEN=true to keep the data plane flowing
// during a declared antiabuse-svc incident. The proxy then relays
// the request to the provider even when the filter is unreachable.
// docs/LEGAL.md acknowledges this opt-in escape valve; every fail-
// open should land in the audit log so the gap is recorded.
func TestSocks5_AntiabuseUnavailable_FailsOpenWhenConfigured(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t, "OPEN/")
	defer stopEcho()
	pool := dispatch.NewStaticPool([]dispatch.ProviderEntry{
		{ID: "prov-1", Endpoint: echoAddr, Online: true},
	})
	srv, ln := buildServer(t, pool, &abuse.StaticFilter{
		Verdict: abuse.Verdict{Decision: abuse.DecisionError, Reason: "antiabuse_unavailable"},
	})
	// FLIP fail-open ON for this test.
	srv.Config.AntiabuseFailOpen = true
	defer ln.Close()
	go srv.Serve(context.Background(), ln) //nolint:errcheck

	c, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	_, _ = c.Write([]byte{0x05, 0x01, 0x02})
	io.ReadFull(c, make([]byte, 2)) //nolint:errcheck
	r := []byte{0x01, 0x02, 'w', 's', byte(len("sk_live_abc"))}
	r = append(r, []byte("sk_live_abc")...)
	_, _ = c.Write(r)
	io.ReadFull(c, make([]byte, 2)) //nolint:errcheck

	host := "any.example"
	connReq := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	connReq = append(connReq, []byte(host)...)
	connReq = append(connReq, 0x01, 0xbb)
	_, _ = c.Write(connReq)
	reply := make([]byte, 10)
	io.ReadFull(c, reply) //nolint:errcheck
	if reply[1] != 0x00 {
		t.Fatalf("fail-open: expected Succeeded (0x00); got 0x%02x", reply[1])
	}
}

// TestSocks5_PortBlocked — port 25 is in the docs/LEGAL.md outbound
// block list.
func TestSocks5_PortBlocked(t *testing.T) {
	pool := dispatch.NewStaticPool([]dispatch.ProviderEntry{
		{ID: "prov-1", Endpoint: "127.0.0.1:1", Online: true},
	})
	srv, ln := buildServer(t, pool, &abuse.StaticFilter{Verdict: abuse.Verdict{Decision: abuse.DecisionAllow}})
	defer ln.Close()
	go srv.Serve(context.Background(), ln) //nolint:errcheck

	c, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	_, _ = c.Write([]byte{0x05, 0x01, 0x02})
	io.ReadFull(c, make([]byte, 2)) //nolint:errcheck
	r := []byte{0x01, 0x02, 'w', 's', byte(len("sk_live_abc"))}
	r = append(r, []byte("sk_live_abc")...)
	_, _ = c.Write(r)
	io.ReadFull(c, make([]byte, 2)) //nolint:errcheck
	// SMTP port 25.
	host := "mail.example"
	connReq := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	connReq = append(connReq, []byte(host)...)
	connReq = append(connReq, 0x00, 0x19) // port 25
	_, _ = c.Write(connReq)
	reply := make([]byte, 10)
	io.ReadFull(c, reply) //nolint:errcheck
	if reply[1] != 0x02 {
		t.Fatalf("expected ConnNotAllowed for SMTP port; got 0x%02x", reply[1])
	}
}

// TestSocks5_Failover — first provider fails dial, second succeeds.
func TestSocks5_Failover(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t, "BACKUP/")
	defer stopEcho()

	// First provider points at a dead port; second works.
	pool := dispatch.NewStaticPool([]dispatch.ProviderEntry{
		{ID: "dead", Endpoint: "127.0.0.1:1", Online: true},
		{ID: "alive", Endpoint: echoAddr, Online: true},
	})

	srv, ln := buildServer(t, pool, &abuse.StaticFilter{Verdict: abuse.Verdict{Decision: abuse.DecisionAllow}})
	defer ln.Close()
	go srv.Serve(context.Background(), ln) //nolint:errcheck

	c, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	_, _ = c.Write([]byte{0x05, 0x01, 0x02})
	io.ReadFull(c, make([]byte, 2)) //nolint:errcheck
	r := []byte{0x01, 0x02, 'w', 's', byte(len("sk_live_abc"))}
	r = append(r, []byte("sk_live_abc")...)
	_, _ = c.Write(r)
	io.ReadFull(c, make([]byte, 2)) //nolint:errcheck

	host := "anywhere.example"
	connReq := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	connReq = append(connReq, []byte(host)...)
	connReq = append(connReq, 0x01, 0xbb)
	_, _ = c.Write(connReq)
	reply := make([]byte, 10)
	if _, err := io.ReadFull(c, reply); err != nil {
		t.Fatalf("read connect reply: %v", err)
	}
	if reply[1] != 0x00 {
		t.Fatalf("expected success after failover; reply = %v", reply)
	}

	// Receive echo marker.
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 64)
	n, _ := c.Read(buf)
	if !strings.Contains(string(buf[:n]), "BACKUP/") {
		t.Fatalf("expected BACKUP/ marker; got %q", buf[:n])
	}
}

// TestSocks5_StickySession — repeated CONNECTs to the same destination
// reuse the same provider id.
func TestSocks5_StickySession(t *testing.T) {
	echoAddr1, stop1 := startEchoServer(t, "A/")
	defer stop1()
	echoAddr2, stop2 := startEchoServer(t, "B/")
	defer stop2()

	pool := dispatch.NewStaticPool([]dispatch.ProviderEntry{
		{ID: "prov-a", Endpoint: echoAddr1, Online: true},
		{ID: "prov-b", Endpoint: echoAddr2, Online: true},
	})

	srv, ln := buildServer(t, pool, &abuse.StaticFilter{Verdict: abuse.Verdict{Decision: abuse.DecisionAllow}})
	defer ln.Close()
	go srv.Serve(context.Background(), ln) //nolint:errcheck

	host := "sticky.example"
	port := []byte{0x01, 0xbb}

	markers := []string{}
	for i := 0; i < 4; i++ {
		c, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		_, _ = c.Write([]byte{0x05, 0x01, 0x02})
		io.ReadFull(c, make([]byte, 2)) //nolint:errcheck
		r := []byte{0x01, 0x02, 'w', 's', byte(len("sk_live_abc"))}
		r = append(r, []byte("sk_live_abc")...)
		_, _ = c.Write(r)
		io.ReadFull(c, make([]byte, 2)) //nolint:errcheck

		connReq := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
		connReq = append(connReq, []byte(host)...)
		connReq = append(connReq, port...)
		_, _ = c.Write(connReq)
		reply := make([]byte, 10)
		io.ReadFull(c, reply) //nolint:errcheck
		if reply[1] != 0x00 {
			t.Fatalf("connect failed: %v", reply)
		}
		c.SetReadDeadline(time.Now().Add(time.Second))
		buf := make([]byte, 8)
		n, _ := c.Read(buf)
		markers = append(markers, string(buf[:n]))
		c.Close()
	}
	// All four iterations must hit the SAME provider (same marker).
	for _, m := range markers[1:] {
		if m != markers[0] {
			t.Fatalf("sticky session broken: markers = %v", markers)
		}
	}
}

// TestHTTPConnect_EndToEnd verifies the HTTP CONNECT path with relay.
func TestHTTPConnect_EndToEnd(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t, "HC/")
	defer stopEcho()

	pool := dispatch.NewStaticPool([]dispatch.ProviderEntry{
		{ID: "prov-1", Endpoint: echoAddr, Online: true},
	})

	srv, ln := buildServer(t, pool, &abuse.StaticFilter{Verdict: abuse.Verdict{Decision: abuse.DecisionAllow}})
	defer ln.Close()
	go srv.Serve(context.Background(), ln) //nolint:errcheck

	c, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	creds := "ws:sk_live_abc"
	enc := base64.StdEncoding.EncodeToString([]byte(creds))
	req := "CONNECT example.com:443 HTTP/1.1\r\n" +
		"Host: example.com:443\r\n" +
		"Proxy-Authorization: Basic " + enc + "\r\n" +
		"\r\n"
	if _, err := c.Write([]byte(req)); err != nil {
		t.Fatalf("write: %v", err)
	}
	br := bufio.NewReader(c)
	line, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("read status line: %v", err)
	}
	if !strings.HasPrefix(line, "HTTP/1.1 200") {
		t.Fatalf("expected 200; got %q", line)
	}
	// Drain headers until blank line.
	for {
		l, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read header: %v", err)
		}
		if l == "\r\n" || l == "\n" {
			break
		}
	}
	// Read marker from echo server.
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 8)
	n, _ := br.Read(buf)
	if !strings.Contains(string(buf[:n]), "HC/") {
		t.Fatalf("expected HC/ marker; got %q", buf[:n])
	}
}

// TestHTTPConnect_407WithoutAuth — proxy must require Proxy-Authorization.
func TestHTTPConnect_407WithoutAuth(t *testing.T) {
	pool := dispatch.NewStaticPool([]dispatch.ProviderEntry{
		{ID: "prov-1", Endpoint: "127.0.0.1:1", Online: true},
	})

	srv, ln := buildServer(t, pool, &abuse.StaticFilter{Verdict: abuse.Verdict{Decision: abuse.DecisionAllow}})
	defer ln.Close()
	go srv.Serve(context.Background(), ln) //nolint:errcheck

	c, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	req := "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"
	_, _ = c.Write([]byte(req))
	br := bufio.NewReader(c)
	line, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.HasPrefix(line, "HTTP/1.1 407") {
		t.Fatalf("expected 407; got %q", line)
	}
}

// TestHTTPConnect_403OnAbuseBlock — verifies 403 on antiabuse BLOCK.
func TestHTTPConnect_403OnAbuseBlock(t *testing.T) {
	pool := dispatch.NewStaticPool([]dispatch.ProviderEntry{
		{ID: "prov-1", Endpoint: "127.0.0.1:1", Online: true},
	})
	srv, ln := buildServer(t, pool, &abuse.StaticFilter{Verdict: abuse.Verdict{Decision: abuse.DecisionBlock, Reason: "csam_hash"}})
	defer ln.Close()
	go srv.Serve(context.Background(), ln) //nolint:errcheck

	c, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	creds := base64.StdEncoding.EncodeToString([]byte("ws:sk_live_abc"))
	req := "CONNECT bad.example:443 HTTP/1.1\r\nHost: bad.example:443\r\nProxy-Authorization: Basic " + creds + "\r\n\r\n"
	_, _ = c.Write([]byte(req))
	br := bufio.NewReader(c)
	line, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.HasPrefix(line, "HTTP/1.1 403") {
		t.Fatalf("expected 403; got %q", line)
	}
}

// TestHTTPConnect_429OnRateLimit — verifies 429 on antiabuse rate limit.
func TestHTTPConnect_429OnRateLimit(t *testing.T) {
	pool := dispatch.NewStaticPool([]dispatch.ProviderEntry{
		{ID: "prov-1", Endpoint: "127.0.0.1:1", Online: true},
	})
	srv, ln := buildServer(t, pool, &abuse.StaticFilter{Verdict: abuse.Verdict{Decision: abuse.DecisionRateLimit, Reason: "rate_limited"}})
	defer ln.Close()
	go srv.Serve(context.Background(), ln) //nolint:errcheck

	c, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	creds := base64.StdEncoding.EncodeToString([]byte("ws:sk_live_abc"))
	req := "CONNECT busy.example:443 HTTP/1.1\r\nHost: busy.example:443\r\nProxy-Authorization: Basic " + creds + "\r\n\r\n"
	_, _ = c.Write([]byte(req))
	br := bufio.NewReader(c)
	line, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.HasPrefix(line, "HTTP/1.1 429") {
		t.Fatalf("expected 429; got %q", line)
	}
}

// TestProtocolDetection — a TCP write of "GET / ..." (non-CONNECT,
// non-SOCKS5) should be rejected cleanly without crashing the loop.
func TestProtocolDetection(t *testing.T) {
	pool := dispatch.NewStaticPool(nil)
	srv, ln := buildServer(t, pool, &abuse.StaticFilter{Verdict: abuse.Verdict{Decision: abuse.DecisionAllow}})
	defer ln.Close()
	go srv.Serve(context.Background(), ln) //nolint:errcheck

	c, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	// Send some bytes that aren't SOCKS5 (0x05) or CONNECT.
	_, _ = c.Write([]byte("GET / HTTP/1.1\r\n\r\n"))
	c.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	if _, err := io.ReadAll(c); err != nil && !errors.Is(err, io.EOF) {
		// Either EOF or a deadline error is acceptable — what we care
		// about is that the server didn't crash and the listener still
		// accepts subsequent connections.
	}
	c.Close()
	// One more dial proves the server didn't crash.
	c2, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("listener died after bad proto: %v", err)
	}
	c2.Close()
}

// TestBillingMeteringRoundTrip verifies bytes round-trip through the
// full pipeline once a relay is up — the metering threshold itself is
// exercised by the unit test in internal/relay (which uses an injectable
// meter callback). This test is mostly a smoke-test that the proxy
// doesn't choke on multi-buffer payloads.
func TestBillingMeteringRoundTrip(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t, "M/")
	defer stopEcho()

	pool := dispatch.NewStaticPool([]dispatch.ProviderEntry{
		{ID: "prov-1", Endpoint: echoAddr, Online: true},
	})
	srv, ln := buildServer(t, pool, &abuse.StaticFilter{Verdict: abuse.Verdict{Decision: abuse.DecisionAllow}})
	defer ln.Close()

	go srv.Serve(context.Background(), ln) //nolint:errcheck

	c, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	_, _ = c.Write([]byte{0x05, 0x01, 0x02})
	io.ReadFull(c, make([]byte, 2)) //nolint:errcheck
	r := []byte{0x01, 0x02, 'w', 's', byte(len("sk_live_abc"))}
	r = append(r, []byte("sk_live_abc")...)
	_, _ = c.Write(r)
	io.ReadFull(c, make([]byte, 2)) //nolint:errcheck
	host := "echo.example"
	cr := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	cr = append(cr, []byte(host)...)
	cr = append(cr, 0x01, 0xbb)
	_, _ = c.Write(cr)
	io.ReadFull(c, make([]byte, 10)) //nolint:errcheck

	payload := bytes.Repeat([]byte{'X'}, 64*1024)
	for i := 0; i < 4; i++ {
		if _, err := c.Write(payload); err != nil {
			break
		}
	}
	c.SetReadDeadline(time.Now().Add(time.Second))
	totalRead := 0
	buf := make([]byte, 8192)
	for {
		n, err := c.Read(buf)
		totalRead += n
		if err != nil || totalRead > 8192 {
			break
		}
	}
	if totalRead == 0 {
		t.Fatalf("expected at least some bytes echoed back")
	}
}
