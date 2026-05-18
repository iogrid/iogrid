// Package httpconnect implements just enough of HTTP CONNECT (RFC 7231
// §4.3.6) for the customer-facing proxy seam:
//
//   - parse the request line + headers (no body, no chunking)
//   - decode the Proxy-Authorization: Basic credentials into apiKey
//   - validate the destination host:port
//   - write either 200 Connection Established or 4xx/5xx with reason
//
// Once the 200 is written the underlying TCP byte stream is handed off
// to the relay layer for transparent forwarding to the chosen provider.
//
// We intentionally do NOT use net/http.Server here; net/http expects a
// proper request-handling lifecycle, but CONNECT is fundamentally a TCP
// hijack flow and trying to hijack out of net/http after auth is
// awkward — easier to parse the request line ourselves and own the
// raw bufio.Reader for the post-CONNECT byte stream.
package httpconnect

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
)

// ErrMalformed is returned on any protocol error.
var ErrMalformed = errors.New("httpconnect: malformed request")

// Request is the parsed CONNECT request.
type Request struct {
	// Host is the destination host parsed from the request line.
	Host string
	// Port is the destination port parsed from the request line.
	Port uint16
	// Version is the HTTP version (e.g. "HTTP/1.1").
	Version string
	// Headers preserves the raw header lines for audit; lower-cased
	// header names map to a single concatenated value.
	Headers map[string]string
	// Username and APIKey are populated when a Proxy-Authorization:
	// Basic header is present.
	Username string
	APIKey   string
}

// Target returns "host:port" suitable for net.Dial.
func (r Request) Target() string {
	return r.Host + ":" + strconv.Itoa(int(r.Port))
}

// ReadRequest parses a CONNECT request line + headers from br. It returns
// the parsed request or ErrMalformed wrapped with detail.
//
// It does NOT consume bytes after the empty CRLF that terminates the
// header block; subsequent bytes belong to the tunnelled TCP stream.
func ReadRequest(br *bufio.Reader) (Request, error) {
	line, err := readCRLFLine(br, 8192)
	if err != nil {
		return Request{}, fmt.Errorf("%w: read request line: %v", ErrMalformed, err)
	}
	method, rest, ok := strings.Cut(line, " ")
	if !ok {
		return Request{}, fmt.Errorf("%w: malformed request line", ErrMalformed)
	}
	if !strings.EqualFold(method, "CONNECT") {
		return Request{}, fmt.Errorf("%w: only CONNECT supported (got %q)", ErrMalformed, method)
	}
	authority, version, ok := strings.Cut(rest, " ")
	if !ok {
		return Request{}, fmt.Errorf("%w: malformed request line (no version)", ErrMalformed)
	}
	host, portStr, err := net.SplitHostPort(authority)
	if err != nil {
		return Request{}, fmt.Errorf("%w: host:port parse: %v", ErrMalformed, err)
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil || port == 0 {
		return Request{}, fmt.Errorf("%w: bad port %q", ErrMalformed, portStr)
	}
	req := Request{
		Host:    strings.TrimRight(host, "."),
		Port:    uint16(port),
		Version: strings.TrimSpace(version),
		Headers: map[string]string{},
	}
	// Read headers until empty CRLF or hard cap (64 headers / 16KB total).
	totalHeaderBytes := 0
	for i := 0; i < 64; i++ {
		hl, err := readCRLFLine(br, 8192)
		if err != nil {
			return Request{}, fmt.Errorf("%w: read header: %v", ErrMalformed, err)
		}
		if hl == "" {
			break
		}
		totalHeaderBytes += len(hl)
		if totalHeaderBytes > 1<<14 {
			return Request{}, fmt.Errorf("%w: header block too large", ErrMalformed)
		}
		name, value, ok := strings.Cut(hl, ":")
		if !ok {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(name))
		val := strings.TrimSpace(value)
		if prior, exists := req.Headers[key]; exists {
			req.Headers[key] = prior + ", " + val
		} else {
			req.Headers[key] = val
		}
	}
	// Decode Proxy-Authorization: Basic if present.
	if pa := req.Headers["proxy-authorization"]; pa != "" {
		user, key, err := decodeBasic(pa)
		if err == nil {
			req.Username = user
			req.APIKey = key
		}
	}
	return req, nil
}

// WriteEstablished writes the canonical "200 Connection Established"
// response, after which the underlying connection is a raw byte pipe.
func WriteEstablished(w io.Writer) error {
	_, err := io.WriteString(w, "HTTP/1.1 200 Connection Established\r\n"+
		"Proxy-Agent: iogrid-proxy-gateway/0.1\r\n\r\n")
	return err
}

// WriteError writes an error response.
//
// For 407 Proxy Authentication Required we include a Proxy-Authenticate
// challenge so well-behaved clients re-attempt with credentials. The
// realm name is fixed to "iogrid"; customer SDKs use whatever they
// were configured with.
func WriteError(w io.Writer, statusCode int, reason string) error {
	statusLine := fmt.Sprintf("HTTP/1.1 %d %s\r\n", statusCode, reason)
	hdrs := "Proxy-Agent: iogrid-proxy-gateway/0.1\r\n" +
		"Content-Length: 0\r\n" +
		"Connection: close\r\n"
	if statusCode == 407 {
		hdrs += "Proxy-Authenticate: Basic realm=\"iogrid\"\r\n"
	}
	_, err := io.WriteString(w, statusLine+hdrs+"\r\n")
	return err
}

// readCRLFLine reads one CRLF-terminated line up to limit bytes (excl.
// the CRLF). It returns the line without the trailing CRLF, or an
// error if the limit is exceeded or EOF arrives mid-line.
func readCRLFLine(br *bufio.Reader, limit int) (string, error) {
	var sb strings.Builder
	for sb.Len() < limit {
		b, err := br.ReadByte()
		if err != nil {
			return "", err
		}
		if b != '\r' {
			sb.WriteByte(b)
			continue
		}
		// We saw CR; expect LF.
		nb, err := br.ReadByte()
		if err != nil {
			return "", err
		}
		if nb != '\n' {
			return "", errors.New("httpconnect: CR not followed by LF")
		}
		return sb.String(), nil
	}
	return "", errors.New("httpconnect: line too long")
}

// decodeBasic decodes a "Basic <base64>" Proxy-Authorization header into
// (user, pass). Anything other than Basic is returned as ErrMalformed.
func decodeBasic(header string) (string, string, error) {
	prefix := "basic "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", "", ErrMalformed
	}
	dec, err := base64.StdEncoding.DecodeString(strings.TrimSpace(header[len(prefix):]))
	if err != nil {
		return "", "", err
	}
	user, pass, ok := strings.Cut(string(dec), ":")
	if !ok {
		return string(dec), "", nil
	}
	return user, pass, nil
}
