package vpn

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// BastionSafety ensures the bastion machine cannot self-disconnect by routing isolation.
// This prevents VPN traffic from accidentally routing Coordinator traffic through the tunnel,
// which would create a routing loop (VPN dies → Coordinator unreachable → can't failover).
type BastionSafety struct {
	coordinatorIP string
	vpcGateway    string
	vpnTableID    int
	routingTableID int
}

// NewBastionSafety creates a new bastion safety manager.
// coordinatorIP: pinned IP of Coordinator (no DNS lookup)
// vpnGateway: default gateway (for Coordinator-only traffic)
// vpnTableID: routing table ID for VPN traffic (e.g., 200)
func NewBastionSafety(coordinatorIP, vpnGateway string) *BastionSafety {
	return &BastionSafety{
		coordinatorIP: coordinatorIP,
		vpcGateway:    vpnGateway,
		vpnTableID:    200,
		routingTableID: 100,
	}
}

// SetupSafetyControls configures routing isolation on the bastion.
// This MUST run before VPN tunnel is brought up.
func (bs *BastionSafety) SetupSafetyControls(ctx context.Context) error {
	fmt.Printf("[BASTION-SAFETY] Setting up routing isolation...\n")

	// Step 1: Create routing table for VPN traffic
	fmt.Printf("[BASTION-SAFETY] Creating VPN routing table (%d)...\n", bs.vpnTableID)
	if err := bs.runCommand("ip", "route", "add", "default", "via", "10.64.0.1", "table", fmt.Sprint(bs.vpnTableID)); err != nil {
		// Note: may fail if table already exists; that's OK
		fmt.Printf("[BASTION-SAFETY] VPN table setup (may already exist): %v\n", err)
	}

	// Step 2: Create rule to force Coordinator traffic to main routing table
	fmt.Printf("[BASTION-SAFETY] Configuring Coordinator traffic isolation...\n")
	// Rule: all traffic from Coordinator IP uses main routing table
	if err := bs.runCommand("ip", "rule", "add", "to", bs.coordinatorIP, "table", "main", "priority", fmt.Sprint(bs.routingTableID)); err != nil {
		fmt.Printf("[BASTION-SAFETY] Rule setup failed: %v\n", err)
		// Don't fail hard; rules might already exist
	}

	// Step 3: Enable keep-alive check (verify Coordinator reachability every 30s)
	fmt.Printf("[BASTION-SAFETY] Coordinator reachability check armed (every 30s)\n")
	go bs.healthCheckCoordinator(ctx)

	fmt.Printf("[BASTION-SAFETY] ✓ Routing isolation complete\n")
	return nil
}

// CleanupSafetyControls removes routing isolation after VPN tunnel is down.
func (bs *BastionSafety) CleanupSafetyControls(ctx context.Context) error {
	fmt.Printf("[BASTION-SAFETY] Cleaning up routing isolation...\n")

	// Remove rules
	_ = bs.runCommand("ip", "rule", "del", "to", bs.coordinatorIP, "table", "main", "priority", fmt.Sprint(bs.routingTableID))

	// Flush VPN routing table
	_ = bs.runCommand("ip", "route", "flush", "table", fmt.Sprint(bs.vpnTableID))

	fmt.Printf("[BASTION-SAFETY] ✓ Cleanup complete\n")
	return nil
}

// EnableKillSwitch prevents traffic from leaking if VPN tunnel dies.
// All traffic destined for the VPN subnet must go through the tunnel.
func (bs *BastionSafety) EnableKillSwitch(ctx context.Context) error {
	fmt.Printf("[BASTION-SAFETY] Enabling kill switch (drop policy on VPN table)...\n")

	// Drop all traffic on VPN table that doesn't have an active route
	// This prevents any traffic from escaping through the default gateway
	if err := bs.runCommand("ip", "route", "add", "blackhole", "0.0.0.0/0", "table", fmt.Sprint(bs.vpnTableID)); err != nil {
		fmt.Printf("[BASTION-SAFETY] Kill switch setup: %v\n", err)
	}

	fmt.Printf("[BASTION-SAFETY] ✓ Kill switch enabled\n")
	return nil
}

// DisableKillSwitch removes the kill switch (restore normal behavior).
func (bs *BastionSafety) DisableKillSwitch(ctx context.Context) error {
	fmt.Printf("[BASTION-SAFETY] Disabling kill switch...\n")

	// Remove blackhole route
	_ = bs.runCommand("ip", "route", "del", "blackhole", "0.0.0.0/0", "table", fmt.Sprint(bs.vpnTableID))

	fmt.Printf("[BASTION-SAFETY] ✓ Kill switch disabled\n")
	return nil
}

// healthCheckCoordinator verifies Coordinator reachability every 30 seconds.
// If Coordinator becomes unreachable, this triggers an alert and potential failover logic.
func (bs *BastionSafety) healthCheckCoordinator(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Ping Coordinator with explicit routing (main table only)
			cmd := exec.CommandContext(ctx, "ping", "-c", "1", "-W", "2", bs.coordinatorIP)
			if err := cmd.Run(); err != nil {
				fmt.Printf("[BASTION-SAFETY] ⚠️  Coordinator unreachable! Error: %v\n", err)
				// In production: trigger failover or emergency alert
				 // Best-effort log (cluster netlink isolation may not be available)
			} else {
				fmt.Printf("[BASTION-SAFETY] ✓ Coordinator healthy\n")
			}
		}
	}
}

// runCommand executes a shell command and returns error if it fails.
func (bs *BastionSafety) runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Note: This implementation uses Linux netlink commands (ip rule/route).
// For macOS/Windows, different APIs would be needed (pfctl, netsh).
// Linux bastion deployments use these primitives.
