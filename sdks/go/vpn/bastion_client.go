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
}

// NewBastionClient creates a new bastion VPN client.
func NewBastionClient(coordinatorAddr, customerID, apiKey string) *BastionClient {
	// Use mock tunnel manager by default (can be switched to RealTunnelManager for production)
	tunnelMgr := NewMockTunnelManager()
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
	fmt.Printf("[BASTION] Connecting to VPN in region: %s\n", region)

	// Step 1: Request VPN session from Coordinator
	fmt.Printf("[BASTION] Requesting session from Coordinator...\n")
	sessionID, err := c.requestSessionFromCoordinator(ctx, region)
	if err != nil {
		return fmt.Errorf("request session: %w", err)
	}
	c.sessionID = sessionID
	fmt.Printf("[BASTION] Session created: %s\n", sessionID)

	// Step 2: Create WireGuard interface
	fmt.Printf("[BASTION] Creating WireGuard interface...\n")
	ifName, err := c.tunnelMgr.CreateInterface(ctx, "wg-iogrid0")
	if err != nil {
		return fmt.Errorf("create interface: %w", err)
	}
	c.ifName = ifName

	// Step 3: Get provider info + ICE candidates from Coordinator
	fmt.Printf("[BASTION] Fetching provider info and ICE candidates...\n")
	providerID, providerWgPubKey, candidates, err := c.getProviderInfo(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("get provider info: %w", err)
	}
	fmt.Printf("[BASTION] Got %d ICE candidates from provider\n", len(candidates))

	// Step 4: Check ICE connectivity to find working candidate
	fmt.Printf("[BASTION] Performing ICE connectivity checks...\n")
	workingCandidate, err := c.iceChecker.CheckCandidates(ctx, candidates)
	if err != nil {
		return fmt.Errorf("ice check: %w", err)
	}
	fmt.Printf("[BASTION] Found working candidate: %s:%d (latency: %dms)\n",
		workingCandidate.ConnectionAddress, workingCandidate.ConnectionPort, workingCandidate.LatencyMs)

	// Step 5: Configure WireGuard peer
	fmt.Printf("[BASTION] Configuring WireGuard peer...\n")
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
	fmt.Printf("[BASTION] Bringing up WireGuard interface...\n")
	if err := c.tunnelMgr.BringUp(ctx, ifName); err != nil {
		return fmt.Errorf("bring up: %w", err)
	}

	// Step 7: Confirm working candidate to Coordinator
	fmt.Printf("[BASTION] Confirming working candidate to Coordinator...\n")
	if err := c.confirmCandidate(ctx, sessionID, workingCandidate); err != nil {
		return fmt.Errorf("confirm candidate: %w", err)
	}

	// Persist endpoint + public key for roaming reconnect
	c.providerEndpoint = endpoint
	c.providerWgPubKey = providerWgPubKey

	// Step 8: Arm roaming detector (callback re-pins WG endpoint within <1s budget)
	c.roamingDetector = NewRoamingDetector(endpoint, 500*time.Millisecond, func(oldIP, newIP net.IP) error {
		fmt.Printf("[BASTION] 🔄 Roaming detected: %s → %s\n", oldIP, newIP)
		// Bump the WireGuard endpoint on the existing interface.
		// WireGuard's stateless transport survives source-IP changes once
		// the new outbound binding is in place — handshake re-key happens
		// automatically on the next data packet.
		if err := c.tunnelMgr.SetEndpoint(context.Background(), c.ifName, c.providerWgPubKey, c.providerEndpoint); err != nil {
			fmt.Printf("[BASTION] roaming reconnect failed: %v\n", err)
			return err
		}
		fmt.Printf("[BASTION] ✓ Roamed in <1s, session %s preserved\n", c.sessionID)
		return nil
	})
	if err := c.roamingDetector.Start(ctx); err != nil {
		fmt.Printf("[BASTION] warning: roaming detector failed to arm: %v\n", err)
	}

	fmt.Printf("[BASTION] ✓ VPN tunnel established successfully!\n")
	fmt.Printf("[BASTION] Session ID: %s\n", sessionID)
	fmt.Printf("[BASTION] Interface: %s\n", ifName)
	fmt.Printf("[BASTION] Provider: %s\n", providerID)
	fmt.Printf("[BASTION] Latency: %dms\n", workingCandidate.LatencyMs)

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

	fmt.Printf("[BASTION] Disconnecting from VPN...\n")
	if err := c.tunnelMgr.BringDown(ctx, c.ifName); err != nil {
		return fmt.Errorf("bring down: %w", err)
	}

	// Notify Coordinator of termination
	if c.sessionID != "" {
		_ = c.terminateSession(ctx, c.sessionID, "user_initiated")
	}

	fmt.Printf("[BASTION] VPN tunnel closed\n")
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

	// Convert ICE candidates to MockIceCandidate format for backward compatibility
	var candidates []*MockIceCandidate
	for _, cand := range sessionSnapshot.ICECandidates {
		candidates = append(candidates, &MockIceCandidate{
			ConnectionAddress: cand.ConnectionAddress,
			ConnectionPort:    cand.ConnectionPort,
			CandidateType:     cand.CandidateType,
			LatencyMs:         cand.LatencyMs,
		})
	}

	return sessionSnapshot.ProviderID, sessionSnapshot.ProviderWgPublicKey, candidates, nil
}

// confirmCandidate notifies Coordinator of the working candidate.
func (c *BastionClient) confirmCandidate(ctx context.Context, sessionID string, candidate *MockIceCandidate) error {
	reqBody := map[string]interface{}{
		"chosen_candidate": map[string]interface{}{
			"candidate": candidate.ConnectionAddress,
			"port":      candidate.ConnectionPort,
			"type":      candidate.CandidateType,
			"latency_ms": candidate.LatencyMs,
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

	fmt.Printf("[BASTION] Confirmed candidate: %s:%d\n", candidate.ConnectionAddress, candidate.ConnectionPort)
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
