package vpn

import (
	"context"
	"testing"
	"time"
)

func TestBastionSafety_New(t *testing.T) {
	bs := NewBastionSafety("203.0.113.10", "192.168.1.1")
	if bs.coordinatorIP != "203.0.113.10" {
		t.Errorf("coordinatorIP = %q, want 203.0.113.10", bs.coordinatorIP)
	}
	if bs.vpcGateway != "192.168.1.1" {
		t.Errorf("vpcGateway = %q, want 192.168.1.1", bs.vpcGateway)
	}
	if bs.vpnTableID != 200 {
		t.Errorf("vpnTableID = %d, want 200", bs.vpnTableID)
	}
	if bs.routingTableID != 100 {
		t.Errorf("routingTableID = %d, want 100", bs.routingTableID)
	}
}

// TestBastionSafety_HealthCheckCoordinator_ContextCancellation verifies
// that the background coordinator-reachability loop exits cleanly when
// the parent context is cancelled. Critical safety property: a stuck
// goroutine here means the safety system never cleans up.
func TestBastionSafety_HealthCheckCoordinator_ContextCancellation(t *testing.T) {
	bs := NewBastionSafety("127.0.0.1", "127.0.0.1")
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		bs.healthCheckCoordinator(ctx)
		close(done)
	}()

	// Cancel immediately; loop should exit on next select iteration
	cancel()

	select {
	case <-done:
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("healthCheckCoordinator did not exit within 2s of ctx cancellation")
	}
}

// TestBastionSafety_KillSwitchSemantics is a smoke test for the
// kill-switch contract: Enable + Disable must each return without
// blocking the caller. Actual netlink ops are validated by integration
// tests on Linux hosts with CAP_NET_ADMIN.
func TestBastionSafety_KillSwitchSemantics(t *testing.T) {
	bs := NewBastionSafety("203.0.113.10", "192.168.1.1")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// In CI we don't have CAP_NET_ADMIN, so the ip commands will fail
	// internally but Enable/Disable must still return without error
	// (they log + swallow per the safety controller's "best effort" contract).
	if err := bs.EnableKillSwitch(ctx); err != nil {
		t.Errorf("EnableKillSwitch returned unexpected error: %v", err)
	}
	if err := bs.DisableKillSwitch(ctx); err != nil {
		t.Errorf("DisableKillSwitch returned unexpected error: %v", err)
	}
}

// TestBastionSafety_SetupCleanupSemantics: lifecycle Setup → Cleanup
// must be idempotent — repeated calls must not panic or hang.
func TestBastionSafety_SetupCleanupSemantics(t *testing.T) {
	bs := NewBastionSafety("203.0.113.10", "192.168.1.1")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Setup → Cleanup → Setup → Cleanup must all succeed (no leaks)
	for i := 0; i < 2; i++ {
		if err := bs.SetupSafetyControls(ctx); err != nil {
			t.Errorf("iteration %d: SetupSafetyControls error: %v", i, err)
		}
		if err := bs.CleanupSafetyControls(ctx); err != nil {
			t.Errorf("iteration %d: CleanupSafetyControls error: %v", i, err)
		}
	}
}

// TestBastionSafety_RoutingTablesDistinct: VPN table ID and Coordinator
// rule priority must never collide — that's the entire point of routing
// isolation. If both used the same table, the kernel would route VPN
// traffic and Coordinator traffic the same way, defeating safety.
func TestBastionSafety_RoutingTablesDistinct(t *testing.T) {
	bs := NewBastionSafety("203.0.113.10", "192.168.1.1")
	if bs.vpnTableID == bs.routingTableID {
		t.Errorf("vpnTableID (%d) collides with routingTableID (%d) — safety isolation broken",
			bs.vpnTableID, bs.routingTableID)
	}
}

// TestBastionSafety_CoordinatorIPPreserved: the coordinator IP is the
// safety invariant — once set in NewBastionSafety, it must remain that
// exact value through Setup/Cleanup/Enable/Disable cycles. If it
// silently changes (e.g. via DNS resolution), the entire safety
// guarantee is voided.
func TestBastionSafety_CoordinatorIPPreserved(t *testing.T) {
	const pinnedIP = "203.0.113.42"
	bs := NewBastionSafety(pinnedIP, "192.168.1.1")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_ = bs.SetupSafetyControls(ctx)
	if bs.coordinatorIP != pinnedIP {
		t.Errorf("coordinatorIP drifted after Setup: %q != %q", bs.coordinatorIP, pinnedIP)
	}
	_ = bs.EnableKillSwitch(ctx)
	if bs.coordinatorIP != pinnedIP {
		t.Errorf("coordinatorIP drifted after EnableKillSwitch: %q != %q", bs.coordinatorIP, pinnedIP)
	}
	_ = bs.DisableKillSwitch(ctx)
	_ = bs.CleanupSafetyControls(ctx)
	if bs.coordinatorIP != pinnedIP {
		t.Errorf("coordinatorIP drifted after Cleanup: %q != %q", bs.coordinatorIP, pinnedIP)
	}
}
