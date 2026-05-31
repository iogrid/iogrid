// iogrid — customer-facing CLI for the iogrid P2P VPN.
//
// Customers download the binary (releases.iogrid.org), then:
//
//	iogrid login                      # opens browser, magic-link auth
//	iogrid vpn connect --region us-east-1   # bring tunnel up
//	iogrid vpn status                       # show current session
//	iogrid vpn disconnect                   # bring tunnel down
//	iogrid logout
//
// All state lives in ~/.config/iogrid/. The binary talks to
// https://api.iogrid.org by default; --coordinator overrides for dev.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/iogrid/iogrid/sdks/go/vpn"
)

const (
	defaultCoordinator = "https://api.iogrid.org"
	configDirName      = "iogrid"
	credsFileName      = "credentials.json"
)

type credentials struct {
	CustomerID  string `json:"customer_id"`
	APIKey      string `json:"api_key"`
	Coordinator string `json:"coordinator"`
}

type sessionState struct {
	SessionID  string    `json:"session_id"`
	Region     string    `json:"region"`
	InterfaceName string `json:"interface_name"`
	ConnectedAt time.Time `json:"connected_at"`
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "login":
		cmdLogin(os.Args[2:])
	case "logout":
		cmdLogout(os.Args[2:])
	case "vpn":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: iogrid vpn <connect|disconnect|status>")
			os.Exit(1)
		}
		switch os.Args[2] {
		case "connect":
			cmdVPNConnect(os.Args[3:])
		case "disconnect":
			cmdVPNDisconnect(os.Args[3:])
		case "status":
			cmdVPNStatus(os.Args[3:])
		case "doctor":
			cmdVPNDoctor(os.Args[3:])
		case "regions":
			cmdVPNRegions(os.Args[3:])
		case "run":
			cmdVPNRun(os.Args[3:])
		default:
			fmt.Fprintf(os.Stderr, "unknown vpn subcommand: %s\n", os.Args[2])
			os.Exit(1)
		}
	case "version":
		fmt.Println("iogrid CLI — P2P VPN for residential exit IPs")
		fmt.Println("Coordinator:", coordinatorFromEnvOrDefault())
	case "--help", "-h", "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `iogrid — P2P VPN for residential exit IPs

Usage:
  iogrid login [--api-key=KEY] [--customer-id=ID]
  iogrid vpn connect --region <r> [--coordinator=URL]
  iogrid vpn run --region <r>     # connect + heartbeat-loop (recommended)
  iogrid vpn regions              # list available regions
  iogrid vpn disconnect
  iogrid vpn status
  iogrid vpn doctor       # connectivity self-check
  iogrid logout
  iogrid version
  iogrid help

Run "iogrid login" first to store credentials, then "iogrid vpn connect --region us-east-1".`)
}

// --- login / logout ---------------------------------------------------------

func cmdLogin(args []string) {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	apiKey := fs.String("api-key", "", "iov_live_* API key (mint from iogrid.org/vpn)")
	customerID := fs.String("customer-id", "", "your customer UUID (from iogrid.org/account)")
	coordinator := fs.String("coordinator", defaultCoordinator, "Coordinator base URL")
	_ = fs.Parse(args)

	if *apiKey == "" || *customerID == "" {
		fmt.Fprintln(os.Stderr, "ERROR: --api-key and --customer-id are required.")
		fmt.Fprintln(os.Stderr, "       Get them at https://iogrid.org/vpn")
		fmt.Fprintln(os.Stderr, "       (Interactive browser login coming via #531.)")
		os.Exit(1)
	}

	creds := credentials{
		CustomerID:  *customerID,
		APIKey:      *apiKey,
		Coordinator: *coordinator,
	}
	if err := writeCredentials(creds); err != nil {
		die("write credentials: %v", err)
	}
	fmt.Printf("✓ Logged in as %s (coordinator: %s)\n", *customerID, *coordinator)
	fmt.Println("Now run: iogrid vpn connect --region us-east-1")
}

func cmdLogout(args []string) {
	path := credsPath()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		die("logout: %v", err)
	}
	fmt.Println("✓ Logged out — credentials removed from", path)
}

// --- vpn connect / disconnect / status -------------------------------------

func cmdVPNConnect(args []string) {
	fs := flag.NewFlagSet("connect", flag.ExitOnError)
	region := fs.String("region", "us-east-1", "Region to connect to (us-east-1, eu-west-1, ap-south-1, ...)")
	coordinatorOverride := fs.String("coordinator", "", "Override coordinator URL (debug only)")
	_ = fs.Parse(args)

	creds, err := readCredentials()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: not logged in. Run `iogrid login` first.")
		os.Exit(1)
	}

	coordinator := creds.Coordinator
	if *coordinatorOverride != "" {
		coordinator = *coordinatorOverride
	}

	// Sanity-check the coordinator is reachable before starting tunnel setup
	if err := pingCoordinator(coordinator); err != nil {
		die("coordinator unreachable at %s: %v", coordinator, err)
	}

	fmt.Printf("Connecting to iogrid VPN (region=%s, coordinator=%s)...\n", *region, coordinator)

	client := vpn.NewBastionClient(coordinator, creds.CustomerID, creds.APIKey)
	if os.Getenv("IOGRID_VERBOSE") != "" {
		client.Verbose = true
	}
	ctx, cancel := signalContext()
	defer cancel()

	if err := client.Connect(ctx, *region); err != nil {
		die("vpn connect failed: %v", err)
	}

	// Persist session state so `iogrid vpn status` + `iogrid vpn disconnect` work
	// (the SDK exposes sessionID/ifName as private fields — we store what we
	// know from the Connect log output for now; future revision should expose
	// them via getters on BastionClient).
	state := sessionState{
		Region:      *region,
		ConnectedAt: time.Now(),
	}
	_ = writeSessionState(state)

	fmt.Println()
	fmt.Println("✓ VPN tunnel established. Verify your exit IP:")
	fmt.Println("    curl ifconfig.me")
	fmt.Println()
	fmt.Println("Run `iogrid vpn disconnect` when done.")
}

func cmdVPNDisconnect(args []string) {
	state, err := readSessionState()
	if err != nil {
		fmt.Fprintln(os.Stderr, "no active VPN session — `iogrid vpn status` shows nothing.")
		os.Exit(0)
	}

	creds, err := readCredentials()
	if err != nil {
		die("not logged in")
	}

	fmt.Printf("Disconnecting VPN session (region=%s)...\n", state.Region)

	client := vpn.NewBastionClient(creds.Coordinator, creds.CustomerID, creds.APIKey)
	ctx, cancel := signalContext()
	defer cancel()

	if err := client.Disconnect(ctx); err != nil {
		// Even on failure, remove the local state so we don't get stuck
		_ = os.Remove(sessionStatePath())
		die("disconnect: %v", err)
	}
	_ = os.Remove(sessionStatePath())
	fmt.Println("✓ VPN tunnel closed.")
}

// cmdVPNRun is connect + heartbeat-every-30s + clean Disconnect on SIGINT/SIGTERM.
// The user-facing equivalent of running the daemon — keeps the session alive
// past the coordinator's idle-cleanup window (default 5 min).
func cmdVPNRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	region := fs.String("region", "us-east-1", "Region to connect to")
	refreshInterval := fs.Duration("refresh-interval", 30*time.Second, "Heartbeat interval")
	_ = fs.Parse(args)

	creds, err := readCredentials()
	if err != nil {
		die("not logged in — run: iogrid login")
	}

	client := vpn.NewBastionClient(creds.Coordinator, creds.CustomerID, creds.APIKey)
	if os.Getenv("IOGRID_VERBOSE") != "" {
		client.Verbose = true
	}
	ctx, cancel := signalContext()
	defer cancel()

	fmt.Printf("Connecting to region=%s...\n", *region)
	if err := client.Connect(ctx, *region); err != nil {
		die("connect: %v", err)
	}
	state := sessionState{Region: *region, ConnectedAt: time.Now()}
	_ = writeSessionState(state)
	fmt.Printf("✓ VPN tunnel established. Refreshing every %s. Ctrl-C to disconnect.\n", refreshInterval)

	ticker := time.NewTicker(*refreshInterval)
	defer ticker.Stop()
	var bytesIn, bytesOut uint64
	for {
		select {
		case <-ctx.Done():
			fmt.Println("\nDisconnecting...")
			if err := client.Disconnect(context.Background()); err != nil {
				fmt.Fprintf(os.Stderr, "disconnect: %v\n", err)
			}
			_ = os.Remove(sessionStatePath())
			fmt.Println("✓ Tunnel closed.")
			return
		case <-ticker.C:
			if err := client.RefreshMetrics(ctx, bytesIn, bytesOut); err != nil {
				fmt.Fprintf(os.Stderr, "refresh: %v (will retry next tick)\n", err)
			}
		}
	}
}

// cmdVPNRegions lists every region with at least one healthy provider
// + provider counts. Customers use it to pick --region for vpn connect.
func cmdVPNRegions(args []string) {
	coordinator := coordinatorFromEnvOrDefault()
	c := &http.Client{Timeout: 5 * time.Second}
	resp, err := c.Get(coordinator + "/v1/vpn/regions")
	if err != nil {
		die("coordinator unreachable: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		die("status %d from coordinator", resp.StatusCode)
	}
	var out struct {
		Regions []struct {
			Region           string `json:"region"`
			HealthyProviders int    `json:"healthy_providers"`
			TotalProviders   int    `json:"total_providers"`
		} `json:"regions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		die("decode response: %v", err)
	}
	if len(out.Regions) == 0 {
		fmt.Println("no regions with healthy providers right now — try again in a few minutes")
		return
	}
	fmt.Printf("%-20s %-10s %-10s\n", "REGION", "HEALTHY", "TOTAL")
	for _, r := range out.Regions {
		fmt.Printf("%-20s %-10d %-10d\n", r.Region, r.HealthyProviders, r.TotalProviders)
	}
}

// cmdVPNDoctor runs end-to-end self-checks the customer can paste into
// a support thread when "vpn connect" misbehaves. Outputs one line per
// check with ✓/✗ + remediation hint.
func cmdVPNDoctor(args []string) {
	fmt.Println("iogrid vpn doctor — connectivity self-check")
	coordinator := coordinatorFromEnvOrDefault()
	creds, credsErr := readCredentials()
	_ = creds

	check := func(name string, ok bool, hint string) {
		if ok {
			fmt.Printf("  ✓ %s\n", name)
		} else {
			fmt.Printf("  ✗ %s\n      → %s\n", name, hint)
		}
	}

	// 1. credentials present
	check("local credentials", credsErr == nil,
		"run: iogrid login --api-key=KEY --customer-id=ID")

	// 2. coordinator reachable
	c := &http.Client{Timeout: 5 * time.Second}
	hresp, herr := c.Get(coordinator + "/healthz")
	if hresp != nil {
		hresp.Body.Close()
	}
	check(fmt.Sprintf("coordinator reachable (%s)", coordinator),
		herr == nil && hresp != nil && hresp.StatusCode == 200,
		"check internet connectivity + DNS resolution for api.iogrid.org")

	// 3. coordinator returns providers for default region
	region := "us-east-1"
	presp, perr := c.Get(coordinator + "/v1/vpn/regions/" + region + "/providers")
	providerCount := -1
	if perr == nil && presp != nil {
		body, _ := io.ReadAll(presp.Body)
		presp.Body.Close()
		var p map[string]interface{}
		_ = json.Unmarshal(body, &p)
		if cnt, ok := p["count"].(float64); ok {
			providerCount = int(cnt)
		}
	}
	check(fmt.Sprintf("providers available in %s (count=%d)", region, providerCount),
		providerCount > 0,
		"no providers online in this region — try a different --region or wait")

	// 4. STUN reachable (UDP)
	conn, sterr := net.DialTimeout("udp", "stun.iogrid.org:3478", 3*time.Second)
	if conn != nil {
		conn.Close()
	}
	check("STUN server reachable (stun.iogrid.org:3478/udp)", sterr == nil,
		"UDP egress to :3478 may be blocked by your firewall")

	// 5. existing session state
	if state, err := readSessionState(); err == nil && state.SessionID != "" {
		fmt.Printf("  ⓘ  local session_id: %s (region=%s)\n", state.SessionID, state.Region)
	}

	fmt.Println()
	fmt.Println("If a check failed, paste the full output to https://github.com/iogrid/iogrid/issues")
}

func cmdVPNStatus(args []string) {
	state, err := readSessionState()
	if err != nil {
		fmt.Println("status: no active VPN session")
		return
	}
	creds, err := readCredentials()
	if err != nil {
		die("not logged in")
	}

	// Hit the coordinator for the authoritative view (#538). Falls back to
	// local-file view if the server returns 404 (session terminated) or any
	// network error; that way the user can still see the local state when
	// disconnected from the internet.
	if state.SessionID == "" {
		fmt.Println("status: local session has no session_id (stale state)")
		_ = os.Remove(sessionStatePath())
		return
	}
	c := &http.Client{Timeout: 5 * time.Second}
	resp, err := c.Get(creds.Coordinator + "/v1/vpn/sessions/" + state.SessionID)
	if err != nil {
		fmt.Printf("status: local view only (coordinator unreachable: %v)\n", err)
		printLocalStatus(state)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		fmt.Println("status: session no longer exists on coordinator — cleaning local state")
		_ = os.Remove(sessionStatePath())
		return
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("status: coordinator returned %d: %s\n", resp.StatusCode, body)
		printLocalStatus(state)
		return
	}
	var server map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&server); err != nil {
		fmt.Printf("status: failed to decode coordinator response: %v\n", err)
		printLocalStatus(state)
		return
	}
	fmt.Println("status: connected")
	fmt.Printf("  session_id:   %s\n", server["session_id"])
	fmt.Printf("  state:        %s\n", server["state"])
	fmt.Printf("  region:       %s\n", server["region"])
	fmt.Printf("  provider_id:  %s\n", server["current_provider_id"])
	if pubkey, _ := server["provider_wg_public_key"].(string); pubkey != "" {
		fmt.Printf("  provider_wg:  %s…\n", truncate(pubkey, 12))
	}
	if bIn, ok := server["bytes_in"].(float64); ok {
		fmt.Printf("  bytes_in:     %d\n", int64(bIn))
	}
	if bOut, ok := server["bytes_out"].(float64); ok {
		fmt.Printf("  bytes_out:    %d\n", int64(bOut))
	}
	fmt.Printf("  connected:    %s ago (locally)\n", time.Since(state.ConnectedAt).Round(time.Second))
}

func printLocalStatus(state sessionState) {
	fmt.Printf("  region:     %s\n", state.Region)
	fmt.Printf("  session_id: %s\n", state.SessionID)
	fmt.Printf("  connected:  %s (%s ago)\n", state.ConnectedAt.Format(time.RFC3339), time.Since(state.ConnectedAt).Round(time.Second))
	fmt.Printf("  interface:  %s\n", state.InterfaceName)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// --- helpers ---------------------------------------------------------------

func coordinatorFromEnvOrDefault() string {
	if v := os.Getenv("IOGRID_COORDINATOR"); v != "" {
		return v
	}
	if creds, err := readCredentials(); err == nil && creds.Coordinator != "" {
		return creds.Coordinator
	}
	return defaultCoordinator
}

func pingCoordinator(base string) error {
	c := &http.Client{Timeout: 5 * time.Second}
	resp, err := c.Get(base + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "\nsignal received, cleaning up...")
		cancel()
	}()
	return ctx, cancel
}

func configDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, configDirName)
}

func credsPath() string        { return filepath.Join(configDir(), credsFileName) }
func sessionStatePath() string { return filepath.Join(configDir(), "session.json") }

func writeCredentials(c credentials) error {
	if err := os.MkdirAll(configDir(), 0700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(credsPath(), data, 0600)
}

func readCredentials() (credentials, error) {
	var c credentials
	data, err := os.ReadFile(credsPath())
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(data, &c)
}

func writeSessionState(s sessionState) error {
	if err := os.MkdirAll(configDir(), 0700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(s, "", "  ")
	return os.WriteFile(sessionStatePath(), data, 0600)
}

func readSessionState() (sessionState, error) {
	var s sessionState
	data, err := os.ReadFile(sessionStatePath())
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(data, &s)
}

func die(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(1)
}
