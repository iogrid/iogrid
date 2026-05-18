package socks5

import (
	"bytes"
	"io"
	"net"
	"strings"
	"testing"
)

func TestGreet_AcceptsUserPass(t *testing.T) {
	// Client offers AuthNoneRequired + AuthUserPass; server must pick UserPass.
	client := bytes.NewReader([]byte{Version5, 0x02, AuthNoneRequired, AuthUserPass})
	server := &bytes.Buffer{}
	rw := &fakeRW{r: client, w: server}

	got, err := Greet(rw)
	if err != nil {
		t.Fatalf("Greet returned err: %v", err)
	}
	if got != AuthUserPass {
		t.Fatalf("chose method 0x%02x, want 0x%02x", got, AuthUserPass)
	}
	want := []byte{Version5, AuthUserPass}
	if !bytes.Equal(server.Bytes(), want) {
		t.Fatalf("server reply = %v, want %v", server.Bytes(), want)
	}
}

func TestGreet_RejectsWhenNoUserPass(t *testing.T) {
	client := bytes.NewReader([]byte{Version5, 0x01, AuthNoneRequired})
	server := &bytes.Buffer{}
	rw := &fakeRW{r: client, w: server}

	_, err := Greet(rw)
	if err == nil {
		t.Fatalf("Greet should fail when client offers no UserPass")
	}
	if server.Bytes()[1] != AuthNoAcceptable {
		t.Fatalf("server should reply NoAcceptable; got 0x%02x", server.Bytes()[1])
	}
}

func TestGreet_RejectsBadVersion(t *testing.T) {
	rw := &fakeRW{r: bytes.NewReader([]byte{0x04, 0x01, AuthUserPass}), w: &bytes.Buffer{}}
	if _, err := Greet(rw); err == nil {
		t.Fatal("Greet should reject non-5 version")
	}
}

func TestReadCredentials(t *testing.T) {
	// version=1, ulen=3, "ABC", plen=4, "1234"
	payload := []byte{0x01, 0x03, 'A', 'B', 'C', 0x04, '1', '2', '3', '4'}
	creds, err := ReadCredentials(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("ReadCredentials err: %v", err)
	}
	if creds.Username != "ABC" || creds.Password != "1234" {
		t.Fatalf("creds = %+v", creds)
	}
}

func TestReadCredentials_BadVersion(t *testing.T) {
	payload := []byte{0x02, 0x00, 0x00}
	if _, err := ReadCredentials(bytes.NewReader(payload)); err == nil {
		t.Fatal("expected error on sub-auth-version != 0x01")
	}
}

func TestReadConnectRequest_IPv4(t *testing.T) {
	// VER=5 CMD=1 RSV=0 ATYP=1 IP=1.2.3.4 PORT=443
	payload := []byte{Version5, CmdConnect, 0x00, AtypIPv4, 1, 2, 3, 4, 0x01, 0xbb}
	req, code, err := ReadConnectRequest(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if code != ReplySucceeded {
		t.Fatalf("code = 0x%02x, want succeeded", code)
	}
	if req.Host != "1.2.3.4" || req.Port != 443 {
		t.Fatalf("req = %+v", req)
	}
	if got := req.String(); got != "1.2.3.4:443" {
		t.Fatalf("req.String() = %q", got)
	}
}

func TestReadConnectRequest_Domain(t *testing.T) {
	// "example.com" len=11
	host := "example.com"
	body := []byte{Version5, CmdConnect, 0x00, AtypDomain, byte(len(host))}
	body = append(body, []byte(host)...)
	body = append(body, 0x01, 0xbb)
	req, code, err := ReadConnectRequest(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if code != ReplySucceeded {
		t.Fatalf("code 0x%02x", code)
	}
	if req.Host != host || req.Port != 443 {
		t.Fatalf("req = %+v", req)
	}
}

func TestReadConnectRequest_RejectsBadCommand(t *testing.T) {
	// Use UDP_ASSOCIATE which we don't support.
	payload := []byte{Version5, CmdUDPAssociate, 0x00, AtypIPv4, 1, 2, 3, 4, 0x01, 0xbb}
	_, code, err := ReadConnectRequest(bytes.NewReader(payload))
	if err == nil {
		t.Fatal("expected error for unsupported command")
	}
	if code != ReplyCmdNotSupported {
		t.Fatalf("code = 0x%02x, want CmdNotSupported", code)
	}
}

func TestReadConnectRequest_ZeroPortInvalid(t *testing.T) {
	payload := []byte{Version5, CmdConnect, 0x00, AtypIPv4, 1, 2, 3, 4, 0x00, 0x00}
	_, _, err := ReadConnectRequest(bytes.NewReader(payload))
	if err == nil {
		t.Fatal("expected error on port 0")
	}
}

func TestWriteReply_DefaultBound(t *testing.T) {
	buf := &bytes.Buffer{}
	if err := WriteReply(buf, ReplySucceeded, nil); err != nil {
		t.Fatalf("err: %v", err)
	}
	// Expect VER REP RSV ATYP=IPv4 0.0.0.0 :0 = 10 bytes
	if buf.Len() != 10 {
		t.Fatalf("reply length = %d, want 10", buf.Len())
	}
	if buf.Bytes()[0] != Version5 || buf.Bytes()[1] != ReplySucceeded || buf.Bytes()[3] != AtypIPv4 {
		t.Fatalf("reply = %v", buf.Bytes())
	}
}

func TestWriteReply_WithBoundAddr(t *testing.T) {
	buf := &bytes.Buffer{}
	addr := &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 8443}
	if err := WriteReply(buf, ReplySucceeded, addr); err != nil {
		t.Fatalf("err: %v", err)
	}
	out := buf.Bytes()
	if out[3] != AtypIPv4 {
		t.Fatalf("ATYP = 0x%02x", out[3])
	}
	if out[4] != 192 || out[5] != 0 || out[6] != 2 || out[7] != 1 {
		t.Fatalf("bound IP wrong: %v", out[4:8])
	}
	port := uint16(out[8])<<8 | uint16(out[9])
	if port != 8443 {
		t.Fatalf("bound port = %d", port)
	}
}

func TestConnectRequestString_IPv6(t *testing.T) {
	r := ConnectRequest{Atyp: AtypIPv6, Host: "::1", Port: 443}
	if got := r.String(); !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]:443") {
		t.Fatalf("ipv6 string = %q", got)
	}
}

// fakeRW adapts a Reader + Writer to io.ReadWriter for tests.
type fakeRW struct {
	r io.Reader
	w io.Writer
}

func (f *fakeRW) Read(p []byte) (int, error)  { return f.r.Read(p) }
func (f *fakeRW) Write(p []byte) (int, error) { return f.w.Write(p) }
