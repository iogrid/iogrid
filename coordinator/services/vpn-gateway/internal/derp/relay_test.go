package derp

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// TestRelay_BidirectionalForward — the core property: two peers
// register, send DATA frames in both directions, both arrive intact.
// Covers the registration → routing → encoded-frame readback path
// end-to-end.
func TestRelay_BidirectionalForward(t *testing.T) {
	r := New(nil)

	// Two connected pairs of net.Pipe — one half for AcceptConn, other
	// half for the test to read/write client frames.
	aliceSrv, aliceCli := net.Pipe()
	bobSrv, bobCli := net.Pipe()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); r.AcceptConn(ctx, aliceSrv) }()
	go func() { defer wg.Done(); r.AcceptConn(ctx, bobSrv) }()

	aliceKey := PeerKey{0xaa}
	bobKey := PeerKey{0xbb}

	// REGISTER both.
	mustWrite(t, aliceCli, encodeFrame(KindRegister, aliceKey, nil))
	mustWrite(t, bobCli, encodeFrame(KindRegister, bobKey, nil))

	// Spin until both peers visible in the registry — the registration
	// happens after the server-side reads the REGISTER frame, so a
	// race here would manifest as "peer not connected" on the first
	// DATA send.
	if err := waitFor(time.Second, func() bool {
		peers, _, _, _ := r.StatsSnapshot()
		return peers == 2
	}); err != nil {
		t.Fatalf("never reached 2 registered peers: %v", err)
	}

	// Alice → Bob.
	mustWrite(t, aliceCli, encodeFrame(KindData, bobKey, []byte("hello bob")))
	gotHdr, gotPayload := mustReadFrame(t, bobCli)
	if gotHdr.Kind != KindData {
		t.Fatalf("bob got kind %d, want DATA", gotHdr.Kind)
	}
	if gotHdr.Peer != bobKey {
		t.Errorf("bob got peer %v, want self %v", gotHdr.Peer, bobKey)
	}
	if string(gotPayload) != "hello bob" {
		t.Errorf("payload = %q, want %q", gotPayload, "hello bob")
	}

	// Bob → Alice (reverse direction; same wire).
	mustWrite(t, bobCli, encodeFrame(KindData, aliceKey, []byte("hi alice")))
	gotHdr, gotPayload = mustReadFrame(t, aliceCli)
	if gotHdr.Kind != KindData {
		t.Fatalf("alice got kind %d, want DATA", gotHdr.Kind)
	}
	if string(gotPayload) != "hi alice" {
		t.Errorf("payload = %q, want %q", gotPayload, "hi alice")
	}

	// Stats sanity.
	_, fwd, dropped, bytes := r.StatsSnapshot()
	if fwd != 2 {
		t.Errorf("FramesForwarded = %d, want 2", fwd)
	}
	if dropped != 0 {
		t.Errorf("FramesDropped = %d, want 0", dropped)
	}
	if bytes != uint64(len("hello bob")+len("hi alice")) {
		t.Errorf("BytesForwarded = %d, want %d", bytes, len("hello bob")+len("hi alice"))
	}

	aliceCli.Close()
	bobCli.Close()
	wg.Wait()
}

// TestRelay_PeerGoneOnUnknownDestination — sending DATA to a pubkey
// that hasn't registered must return a PEER_GONE frame to the sender,
// not silently drop. Lets the customer SDK fall back to "session
// unreachable" reporting instead of timing out.
func TestRelay_PeerGoneOnUnknownDestination(t *testing.T) {
	r := New(nil)
	aliceSrv, aliceCli := net.Pipe()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() { defer close(done); r.AcceptConn(ctx, aliceSrv) }()

	mustWrite(t, aliceCli, encodeFrame(KindRegister, PeerKey{0xaa}, nil))
	if err := waitFor(time.Second, func() bool {
		peers, _, _, _ := r.StatsSnapshot()
		return peers == 1
	}); err != nil {
		t.Fatalf("alice not registered: %v", err)
	}

	mustWrite(t, aliceCli, encodeFrame(KindData, PeerKey{0xcc}, []byte("noop")))
	hdr, _ := mustReadFrame(t, aliceCli)
	if hdr.Kind != KindPeerGone {
		t.Errorf("expected PEER_GONE, got kind %d", hdr.Kind)
	}
	if hdr.Peer != (PeerKey{0xcc}) {
		t.Errorf("PEER_GONE peer = %v, want 0xcc", hdr.Peer)
	}

	_, _, dropped, _ := r.StatsSnapshot()
	if dropped != 1 {
		t.Errorf("FramesDropped = %d, want 1", dropped)
	}

	aliceCli.Close()
	<-done
}

// TestRelay_RejectOversizeFrame — frames larger than MaxFrameBytes
// must cause the relay to drop the connection. Prevents memory
// exhaustion from a malformed client.
func TestRelay_RejectOversizeFrame(t *testing.T) {
	r := New(nil)
	srv, cli := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() { defer close(done); r.AcceptConn(ctx, srv) }()

	mustWrite(t, cli, encodeFrame(KindRegister, PeerKey{0x01}, nil))
	if err := waitFor(time.Second, func() bool {
		peers, _, _, _ := r.StatsSnapshot()
		return peers == 1
	}); err != nil {
		t.Fatalf("not registered: %v", err)
	}

	// Hand-craft an oversize frame: header says payload is MaxFrameBytes+1.
	bogus := make([]byte, HeaderBytes)
	bogus[0] = KindData
	bogus[33] = byte((MaxFrameBytes + 1) & 0xff)
	bogus[34] = byte((MaxFrameBytes + 1) >> 8)
	mustWrite(t, cli, bogus)

	// AcceptConn should return + close the connection. Read from cli
	// gets EOF.
	if _, err := cli.Read(make([]byte, 1)); err == nil {
		t.Errorf("expected EOF / closed pipe after oversize frame; got nil")
	} else if !errors.Is(err, io.EOF) && err.Error() != "io: read/write on closed pipe" {
		// net.Pipe's close error string is the canonical signal.
		t.Logf("close error (expected): %v", err)
	}
	cli.Close()
	<-done
}

// TestRelay_DuplicateRegisterRejected — same pubkey can't register
// twice. Keeps the routing table 1:1.
func TestRelay_DuplicateRegisterRejected(t *testing.T) {
	r := New(nil)
	srv1, cli1 := net.Pipe()
	srv2, cli2 := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done1 := make(chan struct{})
	done2 := make(chan struct{})
	go func() { defer close(done1); r.AcceptConn(ctx, srv1) }()
	go func() { defer close(done2); r.AcceptConn(ctx, srv2) }()

	k := PeerKey{0xaa}
	mustWrite(t, cli1, encodeFrame(KindRegister, k, nil))
	if err := waitFor(time.Second, func() bool {
		peers, _, _, _ := r.StatsSnapshot()
		return peers == 1
	}); err != nil {
		t.Fatalf("first peer not registered: %v", err)
	}

	mustWrite(t, cli2, encodeFrame(KindRegister, k, nil))
	// Second conn should be closed by the relay; reading on cli2
	// returns EOF.
	cli2.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := cli2.Read(make([]byte, 1)); err == nil {
		t.Errorf("expected duplicate REGISTER to close conn 2")
	}
	cli2.Close()
	<-done2

	// First peer still registered.
	peers, _, _, _ := r.StatsSnapshot()
	if peers != 1 {
		t.Errorf("PeersConnected = %d, want 1 after duplicate-reject", peers)
	}

	cli1.Close()
	<-done1
}

func mustWrite(t *testing.T, w io.Writer, b []byte) {
	t.Helper()
	if _, err := w.Write(b); err != nil {
		t.Fatalf("write %d bytes: %v", len(b), err)
	}
}

func mustReadFrame(t *testing.T, r net.Conn) (frameHeader, []byte) {
	t.Helper()
	r.SetReadDeadline(time.Now().Add(2 * time.Second))
	hdr, payload, err := readFrame(r)
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	return hdr, payload
}

// waitFor polls `pred` every 10ms until it returns true or `total`
// elapses. Returns nil if pred succeeded, an error otherwise.
func waitFor(total time.Duration, pred func() bool) error {
	deadline := time.Now().Add(total)
	for time.Now().Before(deadline) {
		if pred() {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return errors.New("waitFor: timeout")
}
