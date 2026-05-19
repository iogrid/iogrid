package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestExtractProfile_CanonicalSSR exercises the og:title + og:description
// path against the fixture HTML the production page ships in 2026-05.
func TestExtractProfile_CanonicalSSR(t *testing.T) {
	r := ExtractProfile(fixtureHTML)
	if r.Name != "Satya Nadella" {
		t.Fatalf("name: got %q want %q", r.Name, "Satya Nadella")
	}
	if r.Title != "Chairman and CEO at Microsoft" {
		t.Fatalf("title: got %q", r.Title)
	}
	if r.Company != "Microsoft" {
		t.Fatalf("company: got %q", r.Company)
	}
}

// TestExtractProfile_TwoSegmentTitle covers the "Name - Title at Company"
// SSR variant LinkedIn occasionally falls back to (no explicit company
// segment in og:title). The extractor derives company by splitting on
// " at " in the title.
func TestExtractProfile_TwoSegmentTitle(t *testing.T) {
	html := `<!DOCTYPE html><html><head>
<meta property="og:title" content="Jane Doe - VP Engineering at Acme Robotics | LinkedIn">
</head><body></body></html>`
	r := ExtractProfile(html)
	if r.Name != "Jane Doe" {
		t.Fatalf("name: got %q", r.Name)
	}
	if r.Title != "VP Engineering at Acme Robotics" {
		t.Fatalf("title: got %q", r.Title)
	}
	if r.Company != "Acme Robotics" {
		t.Fatalf("company: got %q", r.Company)
	}
}

// TestExtractProfile_NoMetaFallbackToTitle covers the case where the
// rendered page omits og:* tags. The <title> element is still authoritative
// for the name. Title/company stay empty — the caller should retry or fall
// back to a richer extractor.
func TestExtractProfile_NoMetaFallbackToTitle(t *testing.T) {
	html := `<!DOCTYPE html><html><head>
<title>Tim Cook | LinkedIn</title>
</head><body></body></html>`
	r := ExtractProfile(html)
	if r.Name != "Tim Cook" {
		t.Fatalf("name: got %q", r.Name)
	}
	if r.Title != "" || r.Company != "" {
		t.Fatalf("expected empty title/company on title-only fallback, got %+v", r)
	}
}

// TestExtractProfile_NoSignal handles a totally-broken page (e.g.
// LinkedIn's "Sign in to continue" stub). Extractor must not panic and
// must return an empty result.
func TestExtractProfile_NoSignal(t *testing.T) {
	html := `<!DOCTYPE html><html><head><title></title></head><body><p>Sign in</p></body></html>`
	r := ExtractProfile(html)
	if r.Name != "" || r.Title != "" || r.Company != "" {
		t.Fatalf("expected empty result on signal-less page, got %+v", r)
	}
}

// TestSplitMetaTitle_StripsLinkedInSuffix asserts the " | LinkedIn"
// suffix is dropped before the dash-split. (Without this we'd surface
// "Microsoft | LinkedIn" as the company.)
func TestSplitMetaTitle_StripsLinkedInSuffix(t *testing.T) {
	parts := splitMetaTitle("A - B - C | LinkedIn")
	if len(parts) != 3 || parts[2] != "C" {
		t.Fatalf("got %#v, want [A B C]", parts)
	}
}

// TestEnrichVanity_DryRun verifies the -dry-run code path returns a
// deterministic result without making any network calls. This is what
// CI uses to exercise the binary without actually scraping LinkedIn.
func TestEnrichVanity_DryRun(t *testing.T) {
	cfg := &Config{Vanity: "satyanadella", DryRun: true}
	r, err := EnrichVanity(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("dry-run enrich: %v", err)
	}
	if r.Name != "Satya Nadella" {
		t.Fatalf("name: %q", r.Name)
	}
	if r.ProxyUsed {
		t.Fatalf("dry-run must not claim proxy_used=true")
	}
}

// TestLoadConfig_RequiresAPIKey asserts that wet-run invocations fail
// loudly without IOGRID_API_KEY / IOGRID_WORKSPACE — we don't want a
// silent fall-through that hits LinkedIn directly from the caller's IP.
func TestLoadConfig_RequiresAPIKey(t *testing.T) {
	t.Setenv("IOGRID_API_KEY", "")
	t.Setenv("IOGRID_WORKSPACE", "")
	_, err := loadConfig([]string{"-vanity", "satyanadella"})
	if err == nil {
		t.Fatal("expected loadConfig to refuse wet-run without IOGRID_API_KEY")
	}
	if !strings.Contains(err.Error(), "IOGRID_API_KEY") {
		t.Fatalf("error should mention IOGRID_API_KEY, got: %v", err)
	}
}

// TestLoadConfig_DryRunSkipsCredentialCheck makes sure CI can run the
// dry-run path with no env set (it's the whole point of -dry-run).
func TestLoadConfig_DryRunSkipsCredentialCheck(t *testing.T) {
	t.Setenv("IOGRID_API_KEY", "")
	t.Setenv("IOGRID_WORKSPACE", "")
	cfg, err := loadConfig([]string{"-dry-run"})
	if err != nil {
		t.Fatalf("dry-run config should not require creds: %v", err)
	}
	if !cfg.DryRun {
		t.Fatal("DryRun flag not set")
	}
}

// TestEnrichVanity_HTTPRoundTrip exercises the real client.Do path
// against an in-process mock server pretending to be LinkedIn. This
// verifies that header injection, body cap, and provider-country
// surfacing work — no SOCKS5 here (covered in the e2e smoke).
func TestEnrichVanity_HTTPRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/in/satyanadella") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("missing UA")
		}
		w.Header().Set("X-Iogrid-Provider-Country", "US")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(fixtureHTML))
	}))
	defer srv.Close()

	cfg := &Config{Vanity: "satyanadella", UserAgent: "test/1.0"}
	// Override target via a custom client that rewrites the request URL.
	hc := &http.Client{
		Transport: rewriteTransport{base: http.DefaultTransport, rewriteTo: srv.URL},
	}
	r, err := EnrichVanity(context.Background(), cfg, hc)
	if err != nil {
		t.Fatalf("enrich: %v", err)
	}
	if r.Name != "Satya Nadella" {
		t.Fatalf("name: %q", r.Name)
	}
	if !r.ProxyUsed {
		t.Fatal("wet-run path must set proxy_used=true")
	}
	if r.ProviderCountry != "US" {
		t.Fatalf("provider_country: %q", r.ProviderCountry)
	}
}

// rewriteTransport routes every request to a fixed base URL, preserving
// path/query. Used only by tests so we don't need to hit real LinkedIn.
type rewriteTransport struct {
	base      http.RoundTripper
	rewriteTo string
}

func (rt rewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	// Build a new URL = rewriteTo + original path.
	r2 := r.Clone(r.Context())
	u := r2.URL
	// Strip scheme/host, prepend the test server's URL.
	target := rt.rewriteTo + u.RequestURI()
	req2, err := http.NewRequestWithContext(r2.Context(), r2.Method, target, r2.Body)
	if err != nil {
		return nil, err
	}
	req2.Header = r2.Header
	return rt.base.RoundTrip(req2)
}

// TestProxyClient_TLSWrappedSOCKS5 exercises the full layering that
// issue #265 mandates: tls.Dial → SOCKS5 greet → USERPASS auth →
// CONNECT → end-to-end HTTP through the established tunnel. The fake
// "gateway" speaks SOCKS5 directly on the TLS-terminated socket — same
// shape as proxy.iogrid.org:443 behind Traefik.
func TestProxyClient_TLSWrappedSOCKS5(t *testing.T) {
	const sni = "proxy.iogrid.org"
	cert, caPool := newSelfSignedCertForTest(t, sni)

	// 1. Build a destination HTTP server pretending to be LinkedIn.
	//    SOCKS5 CONNECT will tunnel to its address.
	dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/in/satyanadella") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("X-Iogrid-Provider-Country", "DE")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(fixtureHTML))
	}))
	defer dest.Close()
	destAddr := strings.TrimPrefix(dest.URL, "http://")

	// 2. Build the fake gateway: TLS-terminating listener that speaks
	//    SOCKS5 and splices to the destination.
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
	if err != nil {
		t.Fatalf("tls.Listen: %v", err)
	}
	defer ln.Close()
	gateAddr := ln.Addr().String()

	var wg sync.WaitGroup
	done := make(chan struct{}, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func(c net.Conn) {
				defer wg.Done()
				defer c.Close()
				if err := fakeSocks5Serve(c, "workspace", "secret-key", destAddr); err != nil {
					t.Logf("fake socks5 server error: %v", err)
				}
				select {
				case done <- struct{}{}:
				default:
				}
			}(c)
		}
	}()

	// 3. Wire the production client at our fake gateway, with the test
	//    CA injected via a custom RootCAs pool on the TLS dial.
	cfg := &Config{
		APIKey:        "secret-key",
		Workspace:     "workspace",
		ProxyHostPort: gateAddr,
		Vanity:        "satyanadella",
		Timeout:       5 * time.Second,
		UserAgent:     "test/1.0",
	}

	// Override the TLS dial to trust our test CA + force SNI = sni so the
	// production code path (everything else) runs verbatim. The dialer is
	// hidden inside the http.Transport so we re-implement newProxyClient
	// here with one tweak — RootCAs / ServerName.
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := &tls.Dialer{Config: &tls.Config{ServerName: sni, RootCAs: caPool}}
			rawConn, err := d.DialContext(ctx, "tcp", cfg.ProxyHostPort)
			if err != nil {
				return nil, err
			}
			conn := rawConn.(*tls.Conn)
			if err := socks5Handshake(conn, cfg.Workspace, cfg.APIKey, addr); err != nil {
				_ = conn.Close()
				return nil, err
			}
			return conn, nil
		},
		ForceAttemptHTTP2: false,
	}

	// 4. Rewrite the LinkedIn URL to the destination server (we want
	//    SOCKS5 + tunnelling exercised, but the URL host has to match
	//    what the CONNECT request targets).
	cfg2 := *cfg
	cfg2.Vanity = "satyanadella"
	// Patch: instead of asking the http.Client to dial linkedin.com, ask
	// it to dial dest.URL — the CONNECT addr the client sends becomes
	// destAddr, which is what fakeSocks5Serve accepts.
	hcRewritten := &http.Client{Timeout: cfg.Timeout, Transport: rewriteTransport{base: tr, rewriteTo: "http://" + destAddr}}
	r, err := EnrichVanity(context.Background(), &cfg2, hcRewritten)
	if err != nil {
		t.Fatalf("enrich: %v", err)
	}
	if r.Name != "Satya Nadella" {
		t.Fatalf("name: %q", r.Name)
	}
	if !r.ProxyUsed {
		t.Fatal("proxy_used must be true on wet-run path")
	}
	if r.ProviderCountry != "DE" {
		t.Fatalf("provider_country: %q", r.ProviderCountry)
	}
	// Ensure the fake gateway actually completed at least one session.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("fake gateway never observed a completed session")
	}
}

// TestSOCKS5Handshake_AuthFailure asserts that a USERPASS reject from
// the gateway surfaces as a clear error (not a hung connection).
func TestSOCKS5Handshake_AuthFailure(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		// VER + 1 method
		var greet [3]byte
		if _, err := io.ReadFull(server, greet[:]); err != nil {
			return
		}
		// Select USERPASS
		_, _ = server.Write([]byte{0x05, 0x02})
		// Read the userpass packet (just drain — we know the layout).
		var head [2]byte
		_, _ = io.ReadFull(server, head[:])
		ulen := int(head[1])
		_, _ = io.ReadFull(server, make([]byte, ulen))
		var plenBuf [1]byte
		_, _ = io.ReadFull(server, plenBuf[:])
		_, _ = io.ReadFull(server, make([]byte, int(plenBuf[0])))
		// Reject.
		_, _ = server.Write([]byte{0x01, 0xff})
	}()

	err := socks5Handshake(client, "u", "wrong", "host:80")
	if err == nil {
		t.Fatal("expected auth failure, got nil")
	}
	if !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("expected auth-failure error, got %v", err)
	}
}

// TestSOCKS5Handshake_AllMethodsRejected asserts the 0xff method-select
// reject path produces a clear error.
func TestSOCKS5Handshake_AllMethodsRejected(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		var greet [3]byte
		_, _ = io.ReadFull(server, greet[:])
		_, _ = server.Write([]byte{0x05, 0xff})
	}()

	err := socks5Handshake(client, "u", "p", "host:80")
	if err == nil || !strings.Contains(err.Error(), "rejected all offered auth methods") {
		t.Fatalf("expected method-select rejection, got %v", err)
	}
}

// fakeSocks5Serve is a minimal SOCKS5 server used by the unit test. It
// requires USERPASS auth equal to (user, pass), then on CONNECT to
// allowedAddr it splices to allowedAddr. Any other CONNECT target is
// rejected with REP=0x02 (rule denied). Not suitable for production.
func fakeSocks5Serve(c net.Conn, user, pass, allowedAddr string) error {
	br := bufio.NewReader(c)

	// Greet.
	var greet [2]byte
	if _, err := io.ReadFull(br, greet[:]); err != nil {
		return err
	}
	if greet[0] != 0x05 {
		return errBadVersion
	}
	nmethods := int(greet[1])
	methods := make([]byte, nmethods)
	if _, err := io.ReadFull(br, methods); err != nil {
		return err
	}
	hasUserpass := false
	for _, m := range methods {
		if m == 0x02 {
			hasUserpass = true
			break
		}
	}
	if !hasUserpass {
		_, _ = c.Write([]byte{0x05, 0xff})
		return errNoUserpass
	}
	if _, err := c.Write([]byte{0x05, 0x02}); err != nil {
		return err
	}

	// USERPASS.
	var auth [2]byte
	if _, err := io.ReadFull(br, auth[:]); err != nil {
		return err
	}
	if auth[0] != 0x01 {
		return errBadVersion
	}
	uname := make([]byte, int(auth[1]))
	if _, err := io.ReadFull(br, uname); err != nil {
		return err
	}
	var plenBuf [1]byte
	if _, err := io.ReadFull(br, plenBuf[:]); err != nil {
		return err
	}
	passwd := make([]byte, int(plenBuf[0]))
	if _, err := io.ReadFull(br, passwd); err != nil {
		return err
	}
	if string(uname) != user || string(passwd) != pass {
		_, _ = c.Write([]byte{0x01, 0x01})
		return errAuth
	}
	if _, err := c.Write([]byte{0x01, 0x00}); err != nil {
		return err
	}

	// CONNECT.
	var hdr [4]byte
	if _, err := io.ReadFull(br, hdr[:]); err != nil {
		return err
	}
	if hdr[0] != 0x05 || hdr[1] != 0x01 || hdr[2] != 0x00 {
		return errBadVersion
	}
	var dstHost string
	switch hdr[3] {
	case 0x01:
		var ip [4]byte
		if _, err := io.ReadFull(br, ip[:]); err != nil {
			return err
		}
		dstHost = net.IP(ip[:]).String()
	case 0x03:
		var l [1]byte
		if _, err := io.ReadFull(br, l[:]); err != nil {
			return err
		}
		name := make([]byte, int(l[0]))
		if _, err := io.ReadFull(br, name); err != nil {
			return err
		}
		dstHost = string(name)
	case 0x04:
		var ip [16]byte
		if _, err := io.ReadFull(br, ip[:]); err != nil {
			return err
		}
		dstHost = net.IP(ip[:]).String()
	default:
		return errBadVersion
	}
	var portBuf [2]byte
	if _, err := io.ReadFull(br, portBuf[:]); err != nil {
		return err
	}
	dstPort := binary.BigEndian.Uint16(portBuf[:])
	dst := net.JoinHostPort(dstHost, strconv.Itoa(int(dstPort)))
	if dst != allowedAddr {
		// REP=0x02 connection not allowed by ruleset.
		_, _ = c.Write([]byte{0x05, 0x02, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return errAuth
	}

	// Dial destination.
	upstream, err := net.Dial("tcp", dst)
	if err != nil {
		_, _ = c.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return err
	}
	defer upstream.Close()

	// Success reply.
	if _, err := c.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return err
	}

	// Splice. We need to flush any client bytes already buffered by bufio.
	if n := br.Buffered(); n > 0 {
		buf, _ := br.Peek(n)
		if _, err := upstream.Write(buf); err != nil {
			return err
		}
		_, _ = br.Discard(n)
	}
	var splice sync.WaitGroup
	splice.Add(2)
	go func() {
		defer splice.Done()
		_, _ = io.Copy(upstream, c)
		if tc, ok := upstream.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}()
	go func() {
		defer splice.Done()
		_, _ = io.Copy(c, upstream)
	}()
	splice.Wait()
	return nil
}

var (
	errBadVersion = newFakeErr("bad version")
	errNoUserpass = newFakeErr("no userpass")
	errAuth       = newFakeErr("auth")
)

type fakeErr struct{ s string }

func (e *fakeErr) Error() string { return e.s }

func newFakeErr(s string) error { return &fakeErr{s: s} }

// newSelfSignedCertForTest creates a 1-h ECDSA-equivalent (here RSA-2048
// — keeps the import set tighter, no curve to pick) self-signed cert for
// the named SNI. Returns the cert + a CertPool the client should trust.
func newSelfSignedCertForTest(t *testing.T, sni string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa generate: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: sni},
		NotBefore:    time.Now().Add(-1 * time.Minute),
		NotAfter:     time.Now().Add(1 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{sni, "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		IsCA:         true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(parsed)
	return cert, pool
}
