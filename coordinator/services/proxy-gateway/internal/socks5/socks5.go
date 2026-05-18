// Package socks5 implements the SOCKS5 (RFC 1928) protocol parser and
// the RFC 1929 username/password sub-negotiation, just enough to:
//
//   - greet the client
//   - require username/password authentication
//   - accept a single CONNECT request
//   - reply with success or one of the canonical failure codes
//
// We do NOT use github.com/things-go/go-socks5 here because:
//
//   1. The library wants to own dial-out itself; we need to do anti-abuse
//      pre-flight + workloads-svc dispatch + sticky-session lookup BEFORE
//      we know which provider to forward to.
//   2. The library can't easily route the SAME accept() to either a
//      SOCKS5 or HTTP CONNECT handler — we want a single TCP listener.
//   3. The protocol surface we need is ~150 lines; pulling 4 kloc of
//      transitive deps is not worth it.
//
// Where the library would be useful (BIND, UDP ASSOCIATE) is explicitly
// out of scope for the customer-facing bandwidth proxy.
package socks5

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// SOCKS5 version constant.
const Version5 = 0x05

// AuthMethod codes (RFC 1928 §3).
const (
	AuthNoneRequired   = 0x00
	AuthGSSAPI         = 0x01
	AuthUserPass       = 0x02
	AuthNoAcceptable   = 0xFF
)

// Command codes (RFC 1928 §4).
const (
	CmdConnect      = 0x01
	CmdBind         = 0x02
	CmdUDPAssociate = 0x03
)

// Address type codes (RFC 1928 §4).
const (
	AtypIPv4   = 0x01
	AtypDomain = 0x03
	AtypIPv6   = 0x04
)

// Reply codes (RFC 1928 §6).
const (
	ReplySucceeded               = 0x00
	ReplyGeneralFailure          = 0x01
	ReplyConnNotAllowed          = 0x02
	ReplyNetworkUnreachable      = 0x03
	ReplyHostUnreachable         = 0x04
	ReplyConnRefused             = 0x05
	ReplyTTLExpired              = 0x06
	ReplyCmdNotSupported         = 0x07
	ReplyAddrTypeNotSupported    = 0x08
)

// SubAuthVersion is the RFC 1929 sub-negotiation version byte.
const SubAuthVersion = 0x01

// SubAuthStatus codes.
const (
	SubAuthOK     = 0x00
	SubAuthDenied = 0x01
)

// Credentials is the parsed RFC 1929 user/pass pair.
type Credentials struct {
	Username string
	Password string
}

// ConnectRequest is the parsed CONNECT command.
type ConnectRequest struct {
	// Atyp is the SOCKS5 address-type byte (AtypIPv4, AtypDomain, AtypIPv6).
	Atyp byte
	// Host is the parsed destination: IP literal for IPv4/IPv6,
	// domain name for AtypDomain.
	Host string
	// Port is the parsed destination port.
	Port uint16
}

// String returns "host:port" suitable for net.Dial.
func (r ConnectRequest) String() string {
	if r.Atyp == AtypIPv6 {
		return "[" + r.Host + "]:" + strconv.Itoa(int(r.Port))
	}
	return r.Host + ":" + strconv.Itoa(int(r.Port))
}

// Greet performs the SOCKS5 handshake step where the client offers a
// list of auth methods. We always require AuthUserPass; if the client
// doesn't list it we reply AuthNoAcceptable and the caller closes the
// connection.
//
// Returns the chosen method on success.
func Greet(rw io.ReadWriter) (byte, error) {
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(rw, hdr); err != nil {
		return 0, fmt.Errorf("socks5: read greeting header: %w", err)
	}
	if hdr[0] != Version5 {
		return 0, fmt.Errorf("socks5: unsupported version 0x%02x", hdr[0])
	}
	nMethods := int(hdr[1])
	if nMethods == 0 {
		return 0, errors.New("socks5: client offered no auth methods")
	}
	methods := make([]byte, nMethods)
	if _, err := io.ReadFull(rw, methods); err != nil {
		return 0, fmt.Errorf("socks5: read auth methods: %w", err)
	}
	chosen := byte(AuthNoAcceptable)
	for _, m := range methods {
		if m == AuthUserPass {
			chosen = AuthUserPass
			break
		}
	}
	if _, err := rw.Write([]byte{Version5, chosen}); err != nil {
		return 0, fmt.Errorf("socks5: write greeting reply: %w", err)
	}
	if chosen == AuthNoAcceptable {
		return chosen, errors.New("socks5: no acceptable auth method (need username/password)")
	}
	return chosen, nil
}

// ReadCredentials parses the RFC 1929 sub-negotiation and returns the
// user/pass pair. The caller must subsequently call WriteAuthStatus to
// accept or reject the credentials.
//
// Returns a generic error on malformed input.
func ReadCredentials(r io.Reader) (Credentials, error) {
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return Credentials{}, fmt.Errorf("socks5: read auth header: %w", err)
	}
	if hdr[0] != SubAuthVersion {
		return Credentials{}, fmt.Errorf("socks5: unsupported sub-auth version 0x%02x", hdr[0])
	}
	ulen := int(hdr[1])
	user := make([]byte, ulen)
	if ulen > 0 {
		if _, err := io.ReadFull(r, user); err != nil {
			return Credentials{}, fmt.Errorf("socks5: read username: %w", err)
		}
	}
	plenBuf := make([]byte, 1)
	if _, err := io.ReadFull(r, plenBuf); err != nil {
		return Credentials{}, fmt.Errorf("socks5: read password length: %w", err)
	}
	plen := int(plenBuf[0])
	pass := make([]byte, plen)
	if plen > 0 {
		if _, err := io.ReadFull(r, pass); err != nil {
			return Credentials{}, fmt.Errorf("socks5: read password: %w", err)
		}
	}
	return Credentials{Username: string(user), Password: string(pass)}, nil
}

// WriteAuthStatus emits the RFC 1929 acceptance / denial byte pair.
func WriteAuthStatus(w io.Writer, ok bool) error {
	status := byte(SubAuthOK)
	if !ok {
		status = SubAuthDenied
	}
	_, err := w.Write([]byte{SubAuthVersion, status})
	return err
}

// ReadConnectRequest parses the CONNECT command. UDP_ASSOCIATE and BIND
// return ReplyCmdNotSupported via the returned reply code (zero on success).
//
// The caller is expected to validate the destination (port allowlist /
// anti-abuse / dispatch) before forwarding bytes, then call
// WriteReply(ReplySucceeded, boundAddr) to finalise the handshake.
func ReadConnectRequest(r io.Reader) (ConnectRequest, byte, error) {
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return ConnectRequest{}, ReplyGeneralFailure, fmt.Errorf("socks5: read request header: %w", err)
	}
	if hdr[0] != Version5 {
		return ConnectRequest{}, ReplyGeneralFailure, fmt.Errorf("socks5: unsupported version 0x%02x", hdr[0])
	}
	if hdr[1] != CmdConnect {
		return ConnectRequest{}, ReplyCmdNotSupported, fmt.Errorf("socks5: command 0x%02x not supported", hdr[1])
	}
	atyp := hdr[3]
	req := ConnectRequest{Atyp: atyp}
	switch atyp {
	case AtypIPv4:
		buf := make([]byte, 4)
		if _, err := io.ReadFull(r, buf); err != nil {
			return req, ReplyGeneralFailure, fmt.Errorf("socks5: read ipv4: %w", err)
		}
		req.Host = net.IP(buf).String()
	case AtypIPv6:
		buf := make([]byte, 16)
		if _, err := io.ReadFull(r, buf); err != nil {
			return req, ReplyGeneralFailure, fmt.Errorf("socks5: read ipv6: %w", err)
		}
		req.Host = net.IP(buf).String()
	case AtypDomain:
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(r, lenBuf); err != nil {
			return req, ReplyGeneralFailure, fmt.Errorf("socks5: read domain length: %w", err)
		}
		dn := make([]byte, lenBuf[0])
		if _, err := io.ReadFull(r, dn); err != nil {
			return req, ReplyGeneralFailure, fmt.Errorf("socks5: read domain: %w", err)
		}
		req.Host = strings.TrimRight(string(dn), ".")
	default:
		return req, ReplyAddrTypeNotSupported, fmt.Errorf("socks5: address type 0x%02x not supported", atyp)
	}
	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(r, portBuf); err != nil {
		return req, ReplyGeneralFailure, fmt.Errorf("socks5: read port: %w", err)
	}
	req.Port = binary.BigEndian.Uint16(portBuf)
	if req.Port == 0 {
		return req, ReplyGeneralFailure, errors.New("socks5: destination port 0 is invalid")
	}
	return req, ReplySucceeded, nil
}

// WriteReply emits the CONNECT reply with a bound-address envelope. The
// bound address should be the proxy-side socket the client can use for
// subsequent associated traffic — for CONNECT this is typically the
// proxy's outbound IP. We emit IPv4 0.0.0.0:0 by default since the
// SOCKS5 client never re-uses the field for CONNECT.
func WriteReply(w io.Writer, code byte, boundAddr net.Addr) error {
	hdr := []byte{Version5, code, 0x00}
	// Default bound = IPv4 0.0.0.0:0.
	atyp := byte(AtypIPv4)
	ipBytes := []byte{0, 0, 0, 0}
	port := uint16(0)
	if boundAddr != nil {
		if tcp, ok := boundAddr.(*net.TCPAddr); ok {
			if v4 := tcp.IP.To4(); v4 != nil {
				ipBytes = v4
			} else if v6 := tcp.IP.To16(); v6 != nil {
				atyp = AtypIPv6
				ipBytes = v6
			}
			port = uint16(tcp.Port)
		}
	}
	out := append([]byte{}, hdr...)
	out = append(out, atyp)
	out = append(out, ipBytes...)
	portBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(portBuf, port)
	out = append(out, portBuf...)
	_, err := w.Write(out)
	return err
}

// ReadDeadline is the recommended per-step deadline for handshake reads.
// We bias toward "fast fail" — well-behaved clients complete the
// handshake in <100ms over LAN, <500ms over WAN.
const ReadDeadline = 10 * time.Second
