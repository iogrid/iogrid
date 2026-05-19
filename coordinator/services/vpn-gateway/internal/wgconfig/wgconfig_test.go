package wgconfig

import (
	"strings"
	"testing"

	"github.com/iogrid/iogrid/coordinator/services/vpn-gateway/internal/tier"
)

func validInputs() Inputs {
	return Inputs{
		CustomerID:         "user-123",
		CustomerPrivateKey: "uM5Hc/abc++basekey++++++++++++++++++++++++=",
		CustomerAddress:    "10.99.0.42/32",
		CustomerCountry:    "us",
		CustomerTier:       tier.TierPlus,
		ServerPublicKey:    "ServerPubKey+++++++++++++++++++++++++++++++=",
		ServerEndpoint:     "vpn.iogrid.org:51820",
	}
}

func TestParsePlatform(t *testing.T) {
	cases := map[string]Platform{
		"ios":     PlatformIOS,
		"IPHONE":  PlatformIOS,
		"android": PlatformAndroid,
		"mac":     PlatformMac,
		"macOS":   PlatformMac,
		"windows": PlatformWindows,
		"linux":   PlatformLinux,
	}
	for in, want := range cases {
		got, err := ParsePlatform(in)
		if err != nil {
			t.Errorf("ParsePlatform(%q): err %v", in, err)
		}
		if got != want {
			t.Errorf("ParsePlatform(%q) = %v, want %v", in, got, want)
		}
	}
	if _, err := ParsePlatform("blackberry"); err == nil {
		t.Error("blackberry should error")
	}
}

func TestRenderLinuxConf(t *testing.T) {
	in := validInputs()
	in.Platform = PlatformLinux
	a, err := Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	body := string(a.Body)
	if !strings.Contains(body, "[Interface]") {
		t.Error("Linux conf must contain [Interface]")
	}
	if !strings.Contains(body, "[Peer]") {
		t.Error("Linux conf must contain [Peer]")
	}
	if !strings.Contains(body, "10.99.0.42/32") {
		t.Error("address missing")
	}
	if !strings.Contains(body, "vpn.iogrid.org:51820") {
		t.Error("endpoint missing")
	}
	if !strings.Contains(body, "PrivateKey =") {
		t.Error("private key block missing")
	}
	if !strings.Contains(body, "AllowedIPs = 0.0.0.0/0, ::/0") {
		t.Error("default AllowedIPs missing")
	}
	if a.MimeType != "text/plain; charset=utf-8" {
		t.Errorf("mime = %q", a.MimeType)
	}
	if !strings.HasSuffix(a.Filename, ".conf") {
		t.Errorf("filename = %q, want .conf suffix", a.Filename)
	}
}

func TestRenderIOSMobileConfig(t *testing.T) {
	in := validInputs()
	in.Platform = PlatformIOS
	a, err := Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	body := string(a.Body)
	if !strings.Contains(body, "<?xml") {
		t.Error("mobileconfig must be XML plist")
	}
	if !strings.Contains(body, "com.apple.vpn.managed") {
		t.Error("payload type missing")
	}
	if !strings.Contains(body, "com.wireguard.macos") {
		t.Error("subtype missing")
	}
	if !strings.Contains(body, "WgQuickConfig") {
		t.Error("wgquickconfig field missing")
	}
	if a.MimeType != "application/x-apple-aspen-config" {
		t.Errorf("mime = %q", a.MimeType)
	}
	if !strings.HasSuffix(a.Filename, ".mobileconfig") {
		t.Errorf("filename = %q, want .mobileconfig", a.Filename)
	}
	if a.QRPayload == "" {
		t.Error("QRPayload should be populated as the underlying .conf")
	}
}

func TestRenderAndroidConf(t *testing.T) {
	in := validInputs()
	in.Platform = PlatformAndroid
	a, err := Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if string(a.Body) != a.QRPayload {
		t.Error("Android body should equal QRPayload (same .conf)")
	}
}

func TestValidate(t *testing.T) {
	for _, mutate := range []func(*Inputs){
		func(i *Inputs) { i.CustomerID = "" },
		func(i *Inputs) { i.CustomerAddress = "" },
		func(i *Inputs) { i.CustomerAddress = "not-an-ip" },
		func(i *Inputs) { i.ServerPublicKey = "" },
		func(i *Inputs) { i.ServerEndpoint = "" },
	} {
		in := validInputs()
		mutate(&in)
		in.Platform = PlatformLinux
		if _, err := Render(in); err == nil {
			t.Errorf("expected error for input %+v", in)
		}
	}
}

func TestDefaultsApplied(t *testing.T) {
	in := validInputs()
	in.CustomerAddress = "10.99.0.5" // bare IP -> should get /32 appended
	in.Platform = PlatformMac
	in.AllowedIPs = ""
	in.MTU = 0
	a, err := Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	body := string(a.Body)
	if !strings.Contains(body, "10.99.0.5/32") {
		t.Error("bare IP should be promoted to /32")
	}
	if !strings.Contains(body, "MTU = 1280") {
		t.Error("default MTU missing")
	}
	if !strings.Contains(body, "PersistentKeepalive = 25") {
		t.Error("default keepalive missing")
	}
}

func TestTunnelNameWithCountry(t *testing.T) {
	in := validInputs()
	in.CustomerCountry = "de"
	in.Platform = PlatformLinux
	a, err := Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(string(a.Body), "iogrid VPN — DE") {
		t.Error("tunnel name should include uppercased country")
	}
}
