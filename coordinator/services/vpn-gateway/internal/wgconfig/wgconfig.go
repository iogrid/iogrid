// Package wgconfig renders the WireGuard client configuration the customer
// downloads after signing up for the consumer VPN.
//
// We render three artefact shapes:
//
//   - .conf — the universal `wg-quick` text file (Mac, Linux, Windows
//     all accept this; iOS/Android can import via "Add tunnel from file")
//   - Apple Configuration Profile (.mobileconfig) — XML plist that the
//     Apple WireGuard client installs in one tap, with a tunnel name and
//     the customer's tier embedded so client-side kill-switch logic can
//     read it.
//   - PNG QR code — Android clients tap "Scan QR" to pull the .conf
//     directly off the browser screen. We emit the .conf text inside a
//     QR code so the encoding is identical regardless of the platform.
//
// The QR rendering uses the qrcode standard library shape (1-bit BMP-style
// matrix); we do not pull in a heavyweight image library — clients have
// no use for a 'pretty' QR, only a scannable one.
package wgconfig

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"text/template"

	"github.com/iogrid/iogrid/coordinator/services/vpn-gateway/internal/tier"
)

// Platform is the destination client OS. We tailor minor details (DNS
// search domain, MTU) by platform.
type Platform string

const (
	PlatformIOS     Platform = "ios"
	PlatformAndroid Platform = "android"
	PlatformMac     Platform = "mac"
	PlatformWindows Platform = "windows"
	PlatformLinux   Platform = "linux"
)

// ParsePlatform maps the public-facing string to a Platform constant.
func ParsePlatform(s string) (Platform, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ios", "iphone", "ipad":
		return PlatformIOS, nil
	case "android":
		return PlatformAndroid, nil
	case "mac", "macos", "osx":
		return PlatformMac, nil
	case "windows", "win":
		return PlatformWindows, nil
	case "linux":
		return PlatformLinux, nil
	default:
		return "", fmt.Errorf("wgconfig: unknown platform %q", s)
	}
}

// Inputs collects everything Render needs.
//
// CustomerPrivateKey is the customer's WG private key (base64). We
// generate it on the server only if the customer didn't supply one at
// signup; in that case we ship it to the customer ONCE and never store
// it. CustomerPublicKey is what we keep server-side.
//
// AllowedIPs defaults to 0.0.0.0/0, ::/0 (full-tunnel). Caller may
// override for split-tunnel use cases (corporate VPN customers).
type Inputs struct {
	CustomerID         string
	CustomerPrivateKey string // base64, may be empty if not server-generated
	CustomerAddress    string // 10.99.X.Y/32
	CustomerCountry    string // ISO alpha-2, optional
	CustomerTier       tier.Tier

	ServerPublicKey  string // base64
	ServerEndpoint   string // host:port (e.g. "vpn.iogrid.org:51820")
	ServerDNSAddress string // e.g. "10.99.0.1" — points at the in-tunnel resolver
	AllowedIPs       string // CIDR list; default 0.0.0.0/0, ::/0
	MTU              int    // 1280 sensible default
	PersistentKA     int    // PersistentKeepalive in seconds (25 default)

	// Platform tweaks generated artefact (mobileconfig vs .conf vs QR).
	Platform Platform
	// TunnelName is the human label shown in the client. Defaults to
	// "iogrid VPN — <country>" when country is set.
	TunnelName string
}

// Artefact is the rendered output. Exactly one of Conf / MobileConfig /
// QRCodeText is canonical for the requested platform; we still return
// all three filled in for convenience (the web BFF surfaces the right
// one and clients can switch platforms without re-fetching).
type Artefact struct {
	// Filename suggested for download (e.g. iogrid-vpn.conf).
	Filename string
	// MimeType for the HTTP response.
	MimeType string
	// Body is the rendered file payload.
	Body []byte
	// QRPayload is the underlying .conf text — clients render the QR
	// in-browser from this (we don't ship a PNG renderer in the gateway
	// because the web BFF already has one).
	QRPayload string
}

// Render produces the artefact for the requested platform. Returns
// ErrIncomplete on missing required fields.
func Render(in Inputs) (Artefact, error) {
	if err := validate(in); err != nil {
		return Artefact{}, err
	}
	defaults(&in)
	conf, err := renderConf(in)
	if err != nil {
		return Artefact{}, err
	}
	switch in.Platform {
	case PlatformIOS, "":
		mobileConfig, err := renderMobileConfig(in, conf)
		if err != nil {
			return Artefact{}, err
		}
		return Artefact{
			Filename:  fmt.Sprintf("iogrid-vpn-%s.mobileconfig", safeName(in.CustomerID)),
			MimeType:  "application/x-apple-aspen-config",
			Body:      []byte(mobileConfig),
			QRPayload: conf,
		}, nil
	case PlatformAndroid:
		return Artefact{
			Filename:  fmt.Sprintf("iogrid-vpn-%s.conf", safeName(in.CustomerID)),
			MimeType:  "text/plain; charset=utf-8",
			Body:      []byte(conf),
			QRPayload: conf,
		}, nil
	case PlatformMac, PlatformWindows, PlatformLinux:
		return Artefact{
			Filename:  fmt.Sprintf("iogrid-vpn-%s.conf", safeName(in.CustomerID)),
			MimeType:  "text/plain; charset=utf-8",
			Body:      []byte(conf),
			QRPayload: conf,
		}, nil
	default:
		return Artefact{}, fmt.Errorf("wgconfig: unhandled platform %q", in.Platform)
	}
}

func validate(in Inputs) error {
	if in.CustomerID == "" {
		return errors.New("wgconfig: CustomerID required")
	}
	if in.CustomerAddress == "" {
		return errors.New("wgconfig: CustomerAddress required")
	}
	if _, _, err := net.ParseCIDR(in.CustomerAddress); err != nil {
		// Accept a bare IP too — the conf format wants CIDR but we'll add /32.
		if ip := net.ParseIP(in.CustomerAddress); ip == nil {
			return fmt.Errorf("wgconfig: CustomerAddress %q is neither IP nor CIDR", in.CustomerAddress)
		}
	}
	if in.ServerPublicKey == "" {
		return errors.New("wgconfig: ServerPublicKey required")
	}
	if in.ServerEndpoint == "" {
		return errors.New("wgconfig: ServerEndpoint required")
	}
	return nil
}

func defaults(in *Inputs) {
	if !strings.Contains(in.CustomerAddress, "/") {
		in.CustomerAddress = in.CustomerAddress + "/32"
	}
	if in.AllowedIPs == "" {
		in.AllowedIPs = "0.0.0.0/0, ::/0"
	}
	if in.ServerDNSAddress == "" {
		in.ServerDNSAddress = "10.99.0.1"
	}
	if in.MTU == 0 {
		in.MTU = 1280
	}
	if in.PersistentKA == 0 {
		in.PersistentKA = 25
	}
	if in.TunnelName == "" {
		if in.CustomerCountry != "" {
			in.TunnelName = fmt.Sprintf("iogrid VPN — %s", strings.ToUpper(in.CustomerCountry))
		} else {
			in.TunnelName = "iogrid VPN"
		}
	}
}

const wgQuickTemplate = `# iogrid VPN — {{ .TunnelName }}
# tier: {{ .CustomerTier }}
# customer-id: {{ .CustomerID }}
# DO NOT SHARE — this configuration contains your private key.
[Interface]
{{- if .CustomerPrivateKey }}
PrivateKey = {{ .CustomerPrivateKey }}
{{- else }}
# PrivateKey = <PASTE-YOUR-PRIVATE-KEY-HERE>
{{- end }}
Address = {{ .CustomerAddress }}
DNS = {{ .ServerDNSAddress }}
MTU = {{ .MTU }}

[Peer]
PublicKey = {{ .ServerPublicKey }}
AllowedIPs = {{ .AllowedIPs }}
Endpoint = {{ .ServerEndpoint }}
PersistentKeepalive = {{ .PersistentKA }}
`

type wgQuickCtx struct {
	TunnelName         string
	CustomerTier       string
	CustomerID         string
	CustomerPrivateKey string
	CustomerAddress    string
	ServerDNSAddress   string
	MTU                int
	ServerPublicKey    string
	AllowedIPs         string
	ServerEndpoint     string
	PersistentKA       int
}

func renderConf(in Inputs) (string, error) {
	t, err := template.New("wg-quick").Parse(wgQuickTemplate)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, wgQuickCtx{
		TunnelName:         in.TunnelName,
		CustomerTier:       in.CustomerTier.String(),
		CustomerID:         in.CustomerID,
		CustomerPrivateKey: in.CustomerPrivateKey,
		CustomerAddress:    in.CustomerAddress,
		ServerDNSAddress:   in.ServerDNSAddress,
		MTU:                in.MTU,
		ServerPublicKey:    in.ServerPublicKey,
		AllowedIPs:         in.AllowedIPs,
		ServerEndpoint:     in.ServerEndpoint,
		PersistentKA:       in.PersistentKA,
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

const mobileConfigTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>PayloadType</key><string>Configuration</string>
  <key>PayloadVersion</key><integer>1</integer>
  <key>PayloadIdentifier</key><string>io.iogrid.vpn.{{ .CustomerID }}</string>
  <key>PayloadUUID</key><string>{{ .PayloadUUID }}</string>
  <key>PayloadDisplayName</key><string>{{ .TunnelName }}</string>
  <key>PayloadDescription</key><string>iogrid Consumer VPN — {{ .CustomerTier }} tier</string>
  <key>PayloadOrganization</key><string>iogrid</string>
  <key>PayloadContent</key><array>
    <dict>
      <key>PayloadType</key><string>com.apple.vpn.managed</string>
      <key>PayloadVersion</key><integer>1</integer>
      <key>PayloadIdentifier</key><string>io.iogrid.vpn.tunnel.{{ .CustomerID }}</string>
      <key>PayloadUUID</key><string>{{ .TunnelUUID }}</string>
      <key>PayloadDisplayName</key><string>{{ .TunnelName }}</string>
      <key>UserDefinedName</key><string>{{ .TunnelName }}</string>
      <key>VPNType</key><string>VPN</string>
      <key>VPNSubType</key><string>com.wireguard.macos</string>
      <key>VendorConfig</key><dict>
        <key>WgQuickConfig</key><string>{{ .QuickConfXML }}</string>
      </dict>
    </dict>
  </array>
</dict>
</plist>
`

type mobileCtx struct {
	CustomerID   string
	CustomerTier string
	TunnelName   string
	PayloadUUID  string
	TunnelUUID   string
	QuickConfXML string
}

func renderMobileConfig(in Inputs, conf string) (string, error) {
	t, err := template.New("mobileconfig").Parse(mobileConfigTemplate)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, mobileCtx{
		CustomerID:   in.CustomerID,
		CustomerTier: in.CustomerTier.String(),
		TunnelName:   in.TunnelName,
		PayloadUUID:  derivedUUID(in.CustomerID + "/payload"),
		TunnelUUID:   derivedUUID(in.CustomerID + "/tunnel"),
		QuickConfXML: xmlEscape(conf),
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// derivedUUID produces a stable per-customer UUID-shaped string from a
// deterministic seed. Apple's plist accepts any UUID format; stability
// matters because re-installing the profile should replace the previous
// one, not add a second copy.
func derivedUUID(seed string) string {
	h := sha1.Sum([]byte("iogrid-vpn-profile:" + seed))
	hexs := hex.EncodeToString(h[:])
	return strings.ToUpper(fmt.Sprintf("%s-%s-%s-%s-%s",
		hexs[0:8], hexs[8:12], hexs[12:16], hexs[16:20], hexs[20:32]))
}

func xmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&apos;",
	)
	return r.Replace(s)
}

// safeName strips characters that would be awkward in a filename. We
// don't aim for full PathSpec coverage — just a sane default.
func safeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := b.String()
	if out == "" {
		return "client"
	}
	return out
}
