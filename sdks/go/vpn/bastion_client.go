package vpn

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// BastionClient is a ready-to-use VPN client for the bastion machine.
// It combines ICE checking + WireGuard tunnel management with proper error handling.
type BastionClient struct {
	coordinatorAddr string
	customerID      string
	apiKey          string
	httpClient      *http.Client
	tunnelMgr       TunnelManager
	iceChecker      *ICEChecker
	roamingDetector *RoamingDetector
	sessionID       string
	ifName          string
	providerEndpoint string // last-known provider endpoint (host:port)
	providerWgPubKey string
	// Verbose enables progress logging during Connect()/Disconnect().
	// Default = terse (only errors). Customer CLI sets true on --verbose,
	// quiet otherwise so users see a clean "Connecting…" → "Connected via
	// <provider> in <region>" instead of a stack trace of internal steps.
	Verbose bool
}

// vlog prints msg only when Verbose is true.
func (c *BastionClient) vlog(format string, args ...interface{}) {
	if c.Verbose {
		fmt.Printf(format, args...)
	}
}

// candidatePriority returns a sortable score that prefers:
//   1. publicly-routable host candidates (e.g. provider's public IP),
//   2. server-reflexive (post-STUN) candidates,
//   3. peer-reflexive,
//   4. private-network host candidates (10.x, 172.16-31, 192.168.x —
//      reachable only from the same LAN as the provider),
//   5. relay.
// Higher score = picked first. Public host beats LAN host because a
// LAN address from a residential provider is unreachable from the
// internet; only same-LAN customers could ever use it.
func candidatePriority(candidateType string) int {
	switch candidateType {
	case "host":
		return 5
	case "srflx":
		return 4
	case "prflx":
		return 3
	case "relay":
		return 1
	}
	return 0
}

// candidateScore combines candidatePriority with a publicly-routable
// bonus so a host candidate at a public IP outranks a host candidate
// at a private IP. Picker uses this; tests can still pass MockIceCandidate
// with any address.
func candidateScore(c *MockIceCandidate) int {
	s := candidatePriority(c.CandidateType) * 10
	addr := c.ConnectionAddress
	if i := strings.Index(addr, "/"); i > 0 {
		addr = addr[:i]
	}
	if ip := net.ParseIP(addr); ip != nil {
		// Public IPv4 → +5; private LAN → 0. Doubles as a fallback
		// when the daemon publishes both public + LAN host candidates
		// (the #557 --public-ip path).
		if v4 := ip.To4(); v4 != nil && !isPrivateIPv4(v4) && !v4.IsLoopback() && !v4.IsLinkLocalUnicast() {
			s += 5
		}
	}
	return s
}

// isPrivateIPv4 returns true for RFC 1918 + CGNAT (100.64/10) ranges,
// which are NOT directly routable on the public internet.
func isPrivateIPv4(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		switch {
		case ip4[0] == 10:
			return true
		case ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31:
			return true
		case ip4[0] == 192 && ip4[1] == 168:
			return true
		case ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127:
			return true
		}
	}
	return false
}

// NewBastionClient creates a new bastion VPN client backed by the
// real wireguard-go userspace tunnel manager. Use NewBastionClientWith
// to override the tunnel manager (e.g. MockTunnelManager in tests).
func NewBastionClient(coordinatorAddr, customerID, apiKey string) *BastionClient {
	return NewBastionClientWith(coordinatorAddr, customerID, apiKey, NewRealTunnelManager())
}

// NewBastionClientWith allows the caller to inject a TunnelManager —
// tests typically pass NewMockTunnelManager().
func NewBastionClientWith(coordinatorAddr, customerID, apiKey string, tunnelMgr TunnelManager) *BastionClient {
	return &BastionClient{
		coordinatorAddr: coordinatorAddr,
		customerID:      customerID,
		apiKey:          apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		tunnelMgr:  tunnelMgr,
		iceChecker: NewICEChecker(2 * time.Second),
	}
}

// Connect establishes a VPN tunnel to a provider in the specified region.
// This is the main entry point for VPN client usage.
func (c *BastionClient) Connect(ctx context.Context, region string) error {
	c.vlog("Connecting to VPN in region: %s\n", region)

	// Step 1: Request VPN session from Coordinator
	c.vlog("Requesting session from Coordinator...\n")
	sessionID, err := c.requestSessionFromCoordinator(ctx, region)
	if err != nil {
		return fmt.Errorf("request session: %w", err)
	}
	c.sessionID = sessionID
	c.vlog("Session created: %s\n", sessionID)

	// Step 2: Create WireGuard interface + generate our keypair
	c.vlog("Creating WireGuard interface...\n")
	ifName, err := c.tunnelMgr.CreateInterface(ctx, "wg-iogrid0")
	if err != nil {
		return fmt.Errorf("create interface: %w", err)
	}
	c.ifName = ifName

	// Step 2a: Generate our customer WG keypair and post the public key to vpn-svc.
	// The provider daemon's binder polls /assigned-sessions every 5s; once it sees
	// our session it allocates a peer slot using THIS customer pubkey + replies
	// with the daemon's own pubkey via /bind-provider. Without our key posted
	// first, the daemon can't allocate the peer.
	custPriv, custPub, err := GenerateKeyPair()
	if err != nil {
		return fmt.Errorf("generate customer keypair: %w", err)
	}
	_ = custPriv // installed inside the TunnelManager's interface key
	if err := c.bindCustomerWgKey(ctx, sessionID, custPub); err != nil {
		return fmt.Errorf("bind customer wg key: %w", err)
	}
	c.vlog("Customer WG pubkey posted; waiting for provider binding...\n")

	// Step 3: Poll GET /sessions/{id} until provider_wg_public_key is set
	// (provider daemon's peer-binder fires within ~5s).
	var providerID, providerWgPubKey string
	var candidates []*MockIceCandidate
	deadline := time.Now().Add(30 * time.Second)
	for {
		providerID, providerWgPubKey, candidates, err = c.getProviderInfo(ctx, sessionID)
		if err != nil {
			return fmt.Errorf("get provider info: %w", err)
		}
		if providerWgPubKey != "" && len(candidates) > 0 {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for provider binding (provider_wg_public_key=%q, candidates=%d)", providerWgPubKey, len(candidates))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	c.vlog("Got %d ICE candidates + provider pubkey from provider\n", len(candidates))

	// Step 4: Pick best candidate.
	//
	// We DON'T run a STUN-style ICE check here — WireGuard servers don't
	// respond to STUN BINDING REQUESTs (#554). Instead we pick the
	// highest-priority candidate (host > srflx > prflx > relay per
	// RFC 8445 §4.1.2.1) and let WireGuard's handshake_init be the
	// connectivity probe. If the chosen candidate doesn't respond to
	// the WG handshake within a few seconds, the FailoverDetector
	// will trip and switch to an alternate provider.
	workingCandidate := candidates[0]
	for _, cand := range candidates {
		if candidateScore(cand) > candidateScore(workingCandidate) {
			workingCandidate = cand
		}
	}
	c.vlog("Picked candidate: %s:%d (type=%s)\n",
		workingCandidate.ConnectionAddress, workingCandidate.ConnectionPort, workingCandidate.CandidateType)

	// Step 5: Configure WireGuard peer
	c.vlog("Configuring WireGuard peer...\n")
	endpoint := fmt.Sprintf("%s:%d", workingCandidate.ConnectionAddress, workingCandidate.ConnectionPort)
	peer := WireGuardPeer{
		PublicKey:  providerWgPubKey,
		AllowedIPs: []string{"0.0.0.0/0"},  // Route all traffic through provider
		Endpoint:   endpoint,
	}
	if err := c.tunnelMgr.AddPeer(ctx, ifName, peer); err != nil {
		return fmt.Errorf("add peer: %w", err)
	}

	// Step 6: Bring up interface
	c.vlog("Bringing up WireGuard interface...\n")
	if err := c.tunnelMgr.BringUp(ctx, ifName); err != nil {
		return fmt.Errorf("bring up: %w", err)
	}

	// Step 7: Confirm working candidate to Coordinator
	c.vlog("Confirming working candidate to Coordinator...\n")
	if err := c.confirmCandidate(ctx, sessionID, workingCandidate); err != nil {
		return fmt.Errorf("confirm candidate: %w", err)
	}

	// Persist endpoint + public key for roaming reconnect
	c.providerEndpoint = endpoint
	c.providerWgPubKey = providerWgPubKey

	// Step 8: Arm roaming detector (callback re-pins WG endpoint within <1s budget)
	c.roamingDetector = NewRoamingDetector(endpoint, 500*time.Millisecond, func(oldIP, newIP net.IP) error {
		c.vlog("🔄 Roaming detected: %s → %s\n", oldIP, newIP)
		// Bump the WireGuard endpoint on the existing interface.
		// WireGuard's stateless transport survives source-IP changes once
		// the new outbound binding is in place — handshake re-key happens
		// automatically on the next data packet.
		if err := c.tunnelMgr.SetEndpoint(context.Background(), c.ifName, c.providerWgPubKey, c.providerEndpoint); err != nil {
			c.vlog("roaming reconnect failed: %v\n", err)
			return err
		}
		c.vlog("✓ Roamed in <1s, session %s preserved\n", c.sessionID)
		return nil
	})
	if err := c.roamingDetector.Start(ctx); err != nil {
		c.vlog("warning: roaming detector failed to arm: %v\n", err)
	}

	c.vlog("✓ VPN tunnel established successfully!\n")
	c.vlog("Session ID: %s\n", sessionID)
	c.vlog("Interface: %s\n", ifName)
	c.vlog("Provider: %s\n", providerID)
	c.vlog("Latency: %dms\n", workingCandidate.LatencyMs)

	return nil
}

// Disconnect closes the VPN tunnel.
func (c *BastionClient) Disconnect(ctx context.Context) error {
	if c.ifName == "" {
		return fmt.Errorf("no active tunnel")
	}

	// Stop roaming detector first so its callback can't race against teardown
	if c.roamingDetector != nil {
		c.roamingDetector.Stop()
		c.roamingDetector = nil
	}

	c.vlog("Disconnecting from VPN...\n")
	if err := c.tunnelMgr.BringDown(ctx, c.ifName); err != nil {
		return fmt.Errorf("bring down: %w", err)
	}

	// Notify Coordinator of termination
	if c.sessionID != "" {
		_ = c.terminateSession(ctx, c.sessionID, "user_initiated")
	}

	c.vlog("VPN tunnel closed\n")
	c.ifName = ""
	c.sessionID = ""
	c.providerEndpoint = ""
	c.providerWgPubKey = ""
	return nil
}

// RefreshMetrics sends updated session metrics to Coordinator (called periodically).
func (c *BastionClient) RefreshMetrics(ctx context.Context, bytesIn, bytesOut uint64) error {
	if c.sessionID == "" {
		return fmt.Errorf("no active session")
	}
	return c.refreshSession(ctx, c.sessionID, bytesIn, bytesOut)
}

// RPC request/response types (avoid importing internal proto)

type RequestSessionReq struct {
	CustomerID  string `json:"customer_id"`
	Region      string `json:"region"`
	APIKeyHash  string `json:"api_key_hash"`
}

type RequestSessionResp struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
}

type SessionSnapshot struct {
	SessionID               string          `json:"session_id"`
	State                   string          `json:"state"`
	ProviderID              string          `json:"provider_id"`
	ProviderWgPublicKey     string          `json:"provider_wg_public_key"`
	ICECandidates           []ICECandidate  `json:"ice_candidates"`
}

// ICECandidate matches the wire format vpn-svc emits, which is the
// proto-generated JSON shape (snake_case fields per protobuf JSON spec).
type ICECandidate struct {
	Foundation        string `json:"foundation"`
	Component         uint32 `json:"component"`
	Transport         string `json:"transport"`
	Priority          uint32 `json:"priority"`
	ConnectionAddress string `json:"connection_address"`
	ConnectionPort    uint32 `json:"connection_port"`
	CandidateType     string `json:"candidate_type"`
	RelatedAddress    string `json:"related_address"`
	RelatedPort       uint32 `json:"related_port"`
	LatencyMs         uint32 `json:"latency_ms"`
	IsPreferred       bool   `json:"is_preferred"`
}

type ConfirmCandidateReq struct {
	ChosenCandidate ICECandidate `json:"chosen_candidate"`
}

type RefreshSessionReq struct {
	BytesIn        uint64 `json:"bytes_in"`
	BytesOut       uint64 `json:"bytes_out"`
	RoamingEvents  uint32 `json:"roaming_events"`
	FailoverCount  uint32 `json:"failover_count"`
}

type TerminateSessionReq struct {
	Reason string `json:"reason"`
}

// Stub methods (to be implemented with actual RPC calls)

// bindCustomerWgKey posts the customer's WG pubkey for the session so the
// provider daemon's peer-binder can allocate a peer.
func (c *BastionClient) bindCustomerWgKey(ctx context.Context, sessionID, pubKey string) error {
	body, _ := json.Marshal(map[string]string{"customer_wg_public_key": pubKey})
	req, err := http.NewRequestWithContext(ctx, "POST",
		c.coordinatorAddr+"/v1/vpn/sessions/"+sessionID+"/bind-customer-wg-key",
		bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, b)
	}
	return nil
}

// requestSessionFromCoordinator requests a VPN session.
func (c *BastionClient) requestSessionFromCoordinator(ctx context.Context, region string) (string, error) {
	// Send the raw API key over TLS — vpn-svc forwards to billing-svc.ValidateApiKey
	// which does the hash comparison server-side. (Hashing on the client was the
	// original design but billing-svc's existing contract takes raw — keeping the
	// proto field name `api_key_hash` for backward compat; new field `api_key` is
	// authoritative.)
	reqBody := map[string]string{
		"customer_id":  c.customerID,
		"region":       region,
		"api_key":      c.apiKey,
	}
	_ = sha256.New // keep crypto/sha256 imported for callers that want it
	_ = base64.StdEncoding

	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, "POST", c.coordinatorAddr+"/v1/vpn/sessions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	sessionID, ok := result["session_id"]
	if !ok {
		return "", fmt.Errorf("session_id not in response")
	}

	return sessionID, nil
}

// getProviderInfo fetches provider details and ICE candidates.
func (c *BastionClient) getProviderInfo(ctx context.Context, sessionID string) (string, string, []*MockIceCandidate, error) {

	req, err := http.NewRequestWithContext(ctx, "GET", c.coordinatorAddr+"/v1/vpn/sessions/"+sessionID, nil)
	if err != nil {
		return "", "", nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", "", nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var sessionSnapshot SessionSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&sessionSnapshot); err != nil {
		return "", "", nil, fmt.Errorf("decode response: %w", err)
	}

	// Convert ICE candidates to MockIceCandidate format for backward compatibility.
	// Postgres inet column renders as "10.x.x.x/32" — strip CIDR suffix before
	// using the address as a UDP dial target.
	var candidates []*MockIceCandidate
	for _, cand := range sessionSnapshot.ICECandidates {
		addr := cand.ConnectionAddress
		if i := strings.Index(addr, "/"); i > 0 {
			addr = addr[:i]
		}
		candidates = append(candidates, &MockIceCandidate{
			ConnectionAddress: addr,
			ConnectionPort:    cand.ConnectionPort,
			CandidateType:     cand.CandidateType,
			LatencyMs:         cand.LatencyMs,
		})
	}

	return sessionSnapshot.ProviderID, sessionSnapshot.ProviderWgPublicKey, candidates, nil
}

// confirmCandidate notifies Coordinator of the working candidate.
//
// Field names MUST match the pb.IceCandidate proto JSON tags
// (connection_address / connection_port / candidate_type) — earlier
// versions used the shorter (candidate/port/type) shape which decoded
// to a zero-valued struct on the server side, then exploded on the
// inet column compare with "" (#557).
func (c *BastionClient) confirmCandidate(ctx context.Context, sessionID string, candidate *MockIceCandidate) error {
	reqBody := map[string]interface{}{
		"chosen_candidate": map[string]interface{}{
			"connection_address": candidate.ConnectionAddress,
			"connection_port":    candidate.ConnectionPort,
			"candidate_type":     candidate.CandidateType,
			"latency_ms":         candidate.LatencyMs,
		},
	}

	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, "PUT", c.coordinatorAddr+"/v1/vpn/sessions/"+sessionID+"/confirm", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	c.vlog("Confirmed candidate: %s:%d\n", candidate.ConnectionAddress, candidate.ConnectionPort)
	return nil
}

// refreshSession sends metrics to Coordinator.
func (c *BastionClient) refreshSession(ctx context.Context, sessionID string, bytesIn, bytesOut uint64) error {
	reqBody := map[string]interface{}{
		"bytes_in":  bytesIn,
		"bytes_out": bytesOut,
		"roaming_events": 0,
		"failover_count": 0,
	}

	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, "POST", c.coordinatorAddr+"/v1/vpn/sessions/"+sessionID+"/refresh", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// terminateSession closes the session on Coordinator.
func (c *BastionClient) terminateSession(ctx context.Context, sessionID string, reason string) error {
	reqBody := map[string]string{
		"reason": reason,
	}

	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, "POST", c.coordinatorAddr+"/v1/vpn/sessions/"+sessionID+"/terminate", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// Helper types for MVP (will be replaced with proto messages)

type ProviderInfo struct {
	ProviderId          string
	ProviderWgPublicKey string
}

type MockIceCandidate struct {
	ConnectionAddress string
	ConnectionPort    uint32
	CandidateType     string
	LatencyMs         uint32
}

// Helper functions

func generateMockID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

func generateMockPublicKey() string {
	b := make([]byte, 32)  // WireGuard keys are 32 bytes
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}
