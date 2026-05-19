package forwarder

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/dispatcher"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/store"
)

// fakeDaemon is an in-process stand-in for the bidi-stream side of the
// daemon. Captures TunnelData / TunnelClose frames sent by the forwarder
// and exposes Deliver* helpers so tests can simulate daemon-side bytes
// flowing back.
type fakeDaemon struct {
	mu sync.Mutex
	// recv* are the channels the test reads to assert what the daemon
	// observed.
	openCh  chan tunnelOpenEvent
	dataCh  chan []byte
	closeCh chan string
	disp    *dispatcher.D
	connRef *dispatcher.Connection
}

type tunnelOpenEvent struct {
	attemptID, targetHostPort string
}

func newFakeDaemon(d *dispatcher.D, providerID string) *fakeDaemon {
	fd := &fakeDaemon{
		openCh:  make(chan tunnelOpenEvent, 4),
		dataCh:  make(chan []byte, 32),
		closeCh: make(chan string, 4),
		disp:    d,
	}
	fd.connRef = &dispatcher.Connection{
		ProviderID:   providerID,
		EndpointHint: "test-forwarder:0",
		SendTunnelOpen: func(attemptID, targetHostPort string) error {
			fd.openCh <- tunnelOpenEvent{attemptID, targetHostPort}
			return nil
		},
		SendTunnelData: func(_ string, payload []byte) error {
			fd.dataCh <- append([]byte(nil), payload...)
			return nil
		},
		SendTunnelClose: func(_ string, reason string) error {
			fd.closeCh <- reason
			return nil
		},
	}
	d.Register(fd.connRef)
	return fd
}

// pushDaemonBytes simulates the daemon writing bytes back to the
// proxy-gateway: it routes through the dispatcher just like the real
// dispatch handler's GetTunnelData branch would.
func (f *fakeDaemon) pushDaemonBytes(attemptID string, b []byte) bool {
	return f.disp.DeliverTunnelData(attemptID, b)
}

// closeDaemonSide simulates the daemon sending a TunnelClose.
func (f *fakeDaemon) closeDaemonSide(attemptID, reason string) bool {
	return f.disp.DeliverTunnelClose(attemptID, reason)
}

// setupForwarder builds an in-memory dispatcher + a forwarder bound to
// :0, returns the forwarder, the dispatcher, and a teardown func.
func setupForwarder(t *testing.T) (*Forwarder, *dispatcher.D, store.Store, func()) {
	t.Helper()
	s := store.NewInMemory()
	d := dispatcher.New(s, nil)
	f := New(Options{
		ListenAddr: "127.0.0.1:0",
		Dispatcher: d,
	})
	addr, err := f.Start(context.Background())
	if err != nil {
		t.Fatalf("forwarder start: %v", err)
	}
	// Stash the resolved addr where tests can grab it via ListenerAddr.
	f.opts.ListenAddr = addr.String()
	return f, d, s, func() { _ = f.Close() }
}

// seedAssignment creates a workload + assignment so the forwarder's
// LookupAssignmentProvider returns the provider id we registered.
func seedAssignment(t *testing.T, s store.Store, attemptID, workloadID, providerID string) {
	t.Helper()
	ctx := context.Background()
	if err := s.CreateWorkload(ctx, &store.Workload{
		ID:     workloadID,
		Type:   "bandwidth",
		Status: store.StatusDispatched,
	}); err != nil {
		t.Fatalf("seed workload: %v", err)
	}
	if err := s.CreateAssignment(ctx, &store.Assignment{
		ID:           attemptID,
		WorkloadID:   workloadID,
		ProviderID:   providerID,
		CreatedAt:    time.Now().UTC(),
		Deadline:     time.Now().Add(time.Minute),
		LatestStatus: store.StatusDispatched,
	}); err != nil {
		t.Fatalf("seed assignment: %v", err)
	}
}

func dialAndPreamble(t *testing.T, addr, attemptID, target string) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	preamble := fmt.Sprintf("%s %s %s\n", PreambleVersion, attemptID, target)
	if _, err := c.Write([]byte(preamble)); err != nil {
		t.Fatalf("write preamble: %v", err)
	}
	return c
}

// TestForwarder_RoundTrip — bytes pushed from proxy-gateway side land at
// daemon; bytes pushed from daemon side land back at proxy-gateway.
func TestForwarder_RoundTrip(t *testing.T) {
	f, d, s, teardown := setupForwarder(t)
	defer teardown()

	const (
		providerID = "p-mac"
		workloadID = "w-1"
		attemptID  = "att-rt"
	)
	fd := newFakeDaemon(d, providerID)
	seedAssignment(t, s, attemptID, workloadID, providerID)

	c := dialAndPreamble(t, f.opts.ListenAddr, attemptID, "127.0.0.1:7878")
	defer c.Close()

	// Wait for the TunnelOpen frame.
	select {
	case ev := <-fd.openCh:
		if ev.attemptID != attemptID {
			t.Fatalf("tunnel_open attempt_id=%q want %q", ev.attemptID, attemptID)
		}
		if ev.targetHostPort != "127.0.0.1:7878" {
			t.Fatalf("tunnel_open target=%q", ev.targetHostPort)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("tunnel_open not sent")
	}

	// Proxy-gateway → daemon.
	want := []byte("GET / HTTP/1.1\r\nHost: foo\r\n\r\n")
	if _, err := c.Write(want); err != nil {
		t.Fatalf("write: %v", err)
	}
	var got []byte
	deadline := time.Now().Add(2 * time.Second)
	for len(got) < len(want) && time.Now().Before(deadline) {
		select {
		case chunk := <-fd.dataCh:
			got = append(got, chunk...)
		case <-time.After(200 * time.Millisecond):
		}
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("daemon-side bytes mismatch: got %q want %q", got, want)
	}

	// Daemon → proxy-gateway.
	resp := []byte("HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nhello")
	if !fd.pushDaemonBytes(attemptID, resp) {
		t.Fatalf("DeliverTunnelData returned false (sink not registered)")
	}

	rb := make([]byte, len(resp))
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := io.ReadFull(c, rb)
	if err != nil {
		t.Fatalf("read echo: %v (n=%d)", err, n)
	}
	if !bytes.Equal(rb, resp) {
		t.Fatalf("proxy-side bytes mismatch: got %q want %q", rb, resp)
	}

	// Daemon-initiated close should propagate to the TCP socket.
	if !fd.closeDaemonSide(attemptID, "") {
		t.Fatalf("DeliverTunnelClose returned false")
	}
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	one := make([]byte, 1)
	_, readErr := c.Read(one)
	if readErr == nil {
		t.Fatalf("expected close, read succeeded")
	}
}

// TestForwarder_RejectsUnknownToken — preamble carries a token the
// dispatcher has no assignment for → forwarder closes the connection
// without firing TunnelOpen.
func TestForwarder_RejectsUnknownToken(t *testing.T) {
	f, d, _, teardown := setupForwarder(t)
	defer teardown()

	// Register a daemon but DON'T seed an assignment. The forwarder
	// must not call its SendTunnelOpen.
	fd := newFakeDaemon(d, "p-mac")

	c := dialAndPreamble(t, f.opts.ListenAddr, "unknown-token", "")
	defer c.Close()

	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	one := make([]byte, 1)
	_, err := c.Read(one)
	if err == nil {
		t.Fatalf("expected closed connection, read succeeded")
	}

	select {
	case ev := <-fd.openCh:
		t.Fatalf("unexpected TunnelOpen: %+v", ev)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestForwarder_RejectsMalformedPreamble — proxy-gateway sends a
// non-conforming first line → forwarder closes immediately.
func TestForwarder_RejectsMalformedPreamble(t *testing.T) {
	f, _, _, teardown := setupForwarder(t)
	defer teardown()

	c, err := net.Dial("tcp", f.opts.ListenAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("BOGUS preamble\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	one := make([]byte, 1)
	if _, err := c.Read(one); err == nil {
		t.Fatalf("expected closed connection")
	}
}

// TestForwarder_PreservesBufferedBytesPastPreamble — clients may send
// preamble + payload in a single TCP write. The bufio.Reader has to
// replay the buffered payload bytes into the daemon.
func TestForwarder_PreservesBufferedBytesPastPreamble(t *testing.T) {
	f, d, s, teardown := setupForwarder(t)
	defer teardown()

	const attemptID = "att-buf"
	fd := newFakeDaemon(d, "p-mac")
	seedAssignment(t, s, attemptID, "w-buf", "p-mac")

	c, err := net.Dial("tcp", f.opts.ListenAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	// Combine preamble + payload into one write.
	payload := []byte("piggyback-bytes")
	all := append([]byte(fmt.Sprintf("%s %s\n", PreambleVersion, attemptID)), payload...)
	if _, err := c.Write(all); err != nil {
		t.Fatalf("write: %v", err)
	}

	// First daemon-bound chunk should be the buffered payload bytes.
	var got []byte
	deadline := time.Now().Add(2 * time.Second)
	for len(got) < len(payload) && time.Now().Before(deadline) {
		select {
		case chunk := <-fd.dataCh:
			got = append(got, chunk...)
		case <-time.After(200 * time.Millisecond):
		}
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("buffered bytes mismatch: got %q want %q", got, payload)
	}
}
