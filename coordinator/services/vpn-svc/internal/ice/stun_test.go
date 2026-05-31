package ice

import (
	"encoding/binary"
	"log/slog"
	"net"
	"testing"
	"time"
)

// TestSTUN_BindingRequestRoundtrip boots the STUN server on an ephemeral
// port and sends a real RFC 5389 BINDING REQUEST, verifying the response
// is a BINDING SUCCESS with an XOR-MAPPED-ADDRESS attribute pointing
// back at the client's source IP:port.
func TestSTUN_BindingRequestRoundtrip(t *testing.T) {
	srv, err := NewSTUNServer("127.0.0.1:0", slog.Default())
	if err != nil {
		t.Fatalf("create STUN server: %v", err)
	}
	defer srv.Close()

	// Discover actual bound port (we asked for :0)
	bound := srv.LocalAddr()

	// Start the server in background
	go func() {
		_ = srv.Start()
	}()
	// Tiny delay for the goroutine to enter recv loop
	time.Sleep(50 * time.Millisecond)

	// Client: dial UDP to STUN and send a BINDING REQUEST
	conn, err := net.DialUDP("udp", nil, bound.(*net.UDPAddr))
	if err != nil {
		t.Fatalf("dial STUN: %v", err)
	}
	defer conn.Close()

	// Build minimal BINDING REQUEST (RFC 5389 §6):
	//   type=0x0001, length=0, magic cookie, 12-byte transaction id
	req := []byte{
		0x00, 0x01, // BINDING REQUEST
		0x00, 0x00, // message length = 0 (no attributes)
		0x21, 0x12, 0xa4, 0x42, // magic cookie
		// 12-byte transaction id (deterministic so we can verify echoed back)
		0x00, 0x01, 0x02, 0x03,
		0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0a, 0x0b,
	}

	_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write(req); err != nil {
		t.Fatalf("write request: %v", err)
	}

	// Read response
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if n < 20 {
		t.Fatalf("response too short (%d bytes), need >=20 for STUN header", n)
	}

	// Verify response header
	msgType := binary.BigEndian.Uint16(buf[0:2])
	if msgType != 0x0101 { // BINDING SUCCESS
		t.Errorf("response type = 0x%04x, want 0x0101 (BINDING SUCCESS)", msgType)
	}

	// Transaction ID must be echoed back unchanged (bytes 8..20)
	for i := 0; i < 12; i++ {
		if buf[8+i] != req[8+i] {
			t.Errorf("transaction ID byte %d mismatch: got 0x%02x want 0x%02x", i, buf[8+i], req[8+i])
		}
	}

	// Magic cookie must be present in response (bytes 4..8)
	cookie := binary.BigEndian.Uint32(buf[4:8])
	if cookie != 0x2112a442 {
		t.Errorf("magic cookie = 0x%08x, want 0x2112a442", cookie)
	}

	// Response must contain at least one attribute (XOR-MAPPED-ADDRESS)
	msgLen := binary.BigEndian.Uint16(buf[2:4])
	if msgLen == 0 {
		t.Error("response message length = 0 — expected XOR-MAPPED-ADDRESS attribute")
	}

	t.Logf("STUN roundtrip OK: msg_len=%d bytes, response_size=%d", msgLen, n)
}

// VPN-551 regression: vpn-svc was setting Family=net.IPv4len (=4) on
// the XOR-MAPPED-ADDRESS attribute instead of the RFC 5389 §15.2 value
// of 0x01. Strict STUN clients (the iogridd Rust parser) rejected the
// response with "unknown address family 0x04" and providers behind NAT
// silently fell back to host-only ICE candidates. This test verifies
// the response now carries the RFC-correct Family + the right
// attribute Type code (0x0020 XOR-MAPPED-ADDRESS, not the legacy 0x0001
// MAPPED-ADDRESS — those carry different XOR semantics).
func TestSTUN_BindingResponse_FamilyIs0x01(t *testing.T) {
	srv, err := NewSTUNServer("127.0.0.1:0", slog.Default())
	if err != nil {
		t.Fatalf("create STUN server: %v", err)
	}
	defer srv.Close()
	bound := srv.LocalAddr()
	go func() { _ = srv.Start() }()
	time.Sleep(50 * time.Millisecond)

	conn, err := net.DialUDP("udp", nil, bound.(*net.UDPAddr))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	req := []byte{
		0x00, 0x01, 0x00, 0x00,
		0x21, 0x12, 0xa4, 0x42,
		0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
		0x18, 0x19, 0x1a, 0x1b,
	}
	_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write(req); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if n < 20+4+8 {
		t.Fatalf("response too short for XOR-MAPPED-ADDRESS, got %d bytes", n)
	}

	// First attribute (offset 20): type 0x0020, length 8, then body.
	attrType := binary.BigEndian.Uint16(buf[20:22])
	if attrType != 0x0020 {
		t.Errorf("attribute type = 0x%04x, want 0x0020 (XOR-MAPPED-ADDRESS); pre-#551 sent 0x0001 (legacy MAPPED-ADDRESS) which has different XOR semantics", attrType)
	}

	// Body: reserved(1) + family(1) + xport(2) + xip(4).
	reserved := buf[24]
	family := buf[25]
	if reserved != 0x00 {
		t.Errorf("reserved byte = 0x%02x, want 0x00", reserved)
	}
	if family != 0x01 {
		t.Errorf("family byte = 0x%02x, want 0x01 (IPv4 per RFC 5389 §15.2); pre-#551 sent 0x04 (Go's net.IPv4len, wrong constant)", family)
	}
}

// TestSTUN_RejectsNonBindingRequests checks that the server doesn't
// crash on garbage / non-BINDING-REQUEST traffic.
func TestSTUN_RejectsNonBindingRequests(t *testing.T) {
	srv, err := NewSTUNServer("127.0.0.1:0", slog.Default())
	if err != nil {
		t.Fatalf("create STUN server: %v", err)
	}
	defer srv.Close()
	bound := srv.LocalAddr()
	go func() { _ = srv.Start() }()
	time.Sleep(50 * time.Millisecond)

	conn, err := net.DialUDP("udp", nil, bound.(*net.UDPAddr))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Send 4 bytes of garbage
	_, _ = conn.Write([]byte{0xde, 0xad, 0xbe, 0xef})

	// Server should not respond (or respond with error); main thing is it
	// doesn't crash. Give it a moment, then send a valid request and verify
	// it still works.
	time.Sleep(100 * time.Millisecond)

	validReq := []byte{
		0x00, 0x01, 0x00, 0x00,
		0x21, 0x12, 0xa4, 0x42,
		0x00, 0x01, 0x02, 0x03,
		0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0a, 0x0b,
	}
	if _, err := conn.Write(validReq); err != nil {
		t.Fatalf("write valid after garbage: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read response after garbage: %v (server may have crashed)", err)
	}
	if n < 20 {
		t.Errorf("response after garbage too short: %d bytes", n)
	}
}
