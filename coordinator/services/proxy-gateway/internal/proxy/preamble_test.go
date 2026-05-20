package proxy_test

// Tests for the IOGRID-TUN/1 forwarder preamble (issue #222 wire spec,
// fixed in iogrid#279). The preamble line is written on every freshly
// dialed provider connection BEFORE the customer's raw bytes start
// flowing, so workloads-svc's forwarder can resolve the attempt id and
// open a TunnelOpen on the daemon's bidi stream.

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/abuse"
	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/dispatch"
)

// startPreambleEchoServer accepts one connection, reads the first line
// (the expected preamble), echoes that line back to the caller, then
// pipes the remaining bytes through io.Copy so the relay still sees a
// data plane it can pump.
//
// Returns the bound address, a *string that receives the captured
// preamble after the first accept, and a cleanup func.
func startPreambleEchoServer(t *testing.T) (addr string, captured *string, cleanup func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var mu sync.Mutex
	var line string
	captured = &line
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		br := bufio.NewReader(c)
		l, err := br.ReadString('\n')
		if err != nil {
			return
		}
		mu.Lock()
		*captured = l
		mu.Unlock()
		// echo the preamble back so the customer can assert reception
		_, _ = c.Write([]byte("ACK:" + l))
		// pump remaining bytes
		_, _ = io.Copy(c, br)
	}()
	cleanup = func() { _ = ln.Close() }
	return ln.Addr().String(), captured, cleanup
}

// TestSocks5_PreambleWrittenWhenEnabled verifies that with
// EnableForwarderPreamble flipped on, the proxy-gateway writes the
// IOGRID-TUN/1 preamble line before the relay starts pumping bytes.
func TestSocks5_PreambleWrittenWhenEnabled(t *testing.T) {
	addr, captured, cleanup := startPreambleEchoServer(t)
	defer cleanup()

	pool := dispatch.NewStaticPool([]dispatch.ProviderEntry{
		{ID: "prov-1", Endpoint: addr, Online: true},
	})
	srv, ln := buildServer(t, pool, &abuse.StaticFilter{Verdict: abuse.Verdict{Decision: abuse.DecisionAllow}})
	srv.EnableForwarderPreamble = true
	defer ln.Close()
	go srv.Serve(context.Background(), ln) //nolint:errcheck

	c, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer c.Close()

	// SOCKS5 USERPASS handshake (copied from TestSocks5_EndToEnd).
	if _, err := c.Write([]byte{0x05, 0x01, 0x02}); err != nil {
		t.Fatalf("write greet: %v", err)
	}
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(c, hdr); err != nil {
		t.Fatalf("read greet reply: %v", err)
	}
	user := []byte("ws")
	pass := []byte("sk_live_abc")
	authReq := append([]byte{0x01, byte(len(user))}, user...)
	authReq = append(authReq, byte(len(pass)))
	authReq = append(authReq, pass...)
	if _, err := c.Write(authReq); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	io.ReadFull(c, make([]byte, 2)) //nolint:errcheck

	// CONNECT www.linkedin.com:443 — the host:port that should land in
	// the preamble.
	host := "www.linkedin.com"
	connReq := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	connReq = append(connReq, []byte(host)...)
	connReq = append(connReq, 0x01, 0xbb) // 0x01bb = 443
	if _, err := c.Write(connReq); err != nil {
		t.Fatalf("write connect: %v", err)
	}
	reply := make([]byte, 10)
	if _, err := io.ReadFull(c, reply); err != nil {
		t.Fatalf("read connect reply: %v", err)
	}
	if reply[0] != 0x05 || reply[1] != 0x00 {
		t.Fatalf("connect reply = %v", reply)
	}

	// Write a TLS-ClientHello-shaped byte sequence — the same bytes
	// that triggered "malformed preamble" before the fix.
	if _, err := c.Write([]byte{0x16, 0x03, 0x01, 0x00, 0xfd, 0x01}); err != nil {
		t.Fatalf("relay write: %v", err)
	}

	// Wait for the echo server to capture + ACK.
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	if _, err := c.Read(buf); err != nil {
		t.Fatalf("relay read: %v", err)
	}

	got := *captured
	if !strings.HasPrefix(got, "IOGRID-TUN/1 ") {
		t.Fatalf("expected IOGRID-TUN/1 preamble; got %q", got)
	}
	if !strings.Contains(got, "att-prov-1-") {
		t.Fatalf("expected attempt_id in preamble (att-prov-1-...); got %q", got)
	}
	if !strings.Contains(got, "www.linkedin.com:443") {
		t.Fatalf("expected target host:port in preamble; got %q", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("expected trailing newline in preamble; got %q", got)
	}
}

// TestSocks5_NoPreambleWhenDisabled verifies that with the flag OFF
// (the default), the relay starts pumping immediately without a wire
// preamble — the local-dev / unit-test contract.
func TestSocks5_NoPreambleWhenDisabled(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t, "GREET/")
	defer stopEcho()

	pool := dispatch.NewStaticPool([]dispatch.ProviderEntry{
		{ID: "prov-1", Endpoint: echoAddr, Online: true},
	})
	srv, ln := buildServer(t, pool, &abuse.StaticFilter{Verdict: abuse.Verdict{Decision: abuse.DecisionAllow}})
	// EnableForwarderPreamble defaults to false — assert it stays off.
	if srv.EnableForwarderPreamble {
		t.Fatalf("EnableForwarderPreamble default = true; want false")
	}
	defer ln.Close()
	go srv.Serve(context.Background(), ln) //nolint:errcheck

	c, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer c.Close()

	_, _ = c.Write([]byte{0x05, 0x01, 0x02})
	io.ReadFull(c, make([]byte, 2)) //nolint:errcheck
	user := []byte("ws")
	pass := []byte("sk_live_abc")
	authReq := append([]byte{0x01, byte(len(user))}, user...)
	authReq = append(authReq, byte(len(pass)))
	authReq = append(authReq, pass...)
	_, _ = c.Write(authReq)
	io.ReadFull(c, make([]byte, 2)) //nolint:errcheck

	host := "echo.test"
	connReq := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	connReq = append(connReq, []byte(host)...)
	connReq = append(connReq, 0x01, 0xbb)
	_, _ = c.Write(connReq)
	reply := make([]byte, 10)
	io.ReadFull(c, reply) //nolint:errcheck

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
		t.Fatalf("expected raw greeting marker (no preamble); got %q", got)
	}
	// The first bytes coming OUT must be the marker, NOT an IOGRID-TUN
	// prefix bouncing off the echo server.
	if strings.HasPrefix(got, "IOGRID-TUN/") {
		t.Fatalf("preamble leaked when disabled; got %q", got)
	}
}
