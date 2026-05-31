package vpn

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestRoamingDetector_BaselineEstablished(t *testing.T) {
	d := NewRoamingDetector("1.1.1.1:443", 500*time.Millisecond, nil)
	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer d.Stop()

	if d.CurrentSourceIP() == nil {
		t.Error("expected baseline source IP to be set after Start")
	}
}

func TestRoamingDetector_NoSpuriousFires(t *testing.T) {
	// When source IP doesn't change, callback must never fire.
	var fires int32
	d := NewRoamingDetector("1.1.1.1:443", 50*time.Millisecond, func(oldIP, newIP net.IP) error {
		atomic.AddInt32(&fires, 1)
		return nil
	})
	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer d.Stop()

	// Let it tick a few times — same network, no roaming
	time.Sleep(300 * time.Millisecond)

	if got := atomic.LoadInt32(&fires); got != 0 {
		t.Errorf("expected 0 callback fires on stable network, got %d", got)
	}
}

func TestRoamingDetector_FiresOnSimulatedRoam(t *testing.T) {
	// Inject a fake baseline so the next CheckOnce sees a change.
	var fires int32
	var capturedOld, capturedNew net.IP
	d := NewRoamingDetector("1.1.1.1:443", 1*time.Second, func(oldIP, newIP net.IP) error {
		atomic.AddInt32(&fires, 1)
		capturedOld = oldIP
		capturedNew = newIP
		return nil
	})
	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer d.Stop()

	// Forcibly overwrite lastIP to a wrong value so the next check triggers roam
	d.mu.Lock()
	originalBaseline := d.lastIP
	d.lastIP = net.ParseIP("192.0.2.99") // RFC 5737 documentation address
	d.mu.Unlock()

	changed, oldIP, newIP := d.CheckOnce()
	if !changed {
		t.Fatal("expected CheckOnce to report change after baseline injection")
	}
	if got := atomic.LoadInt32(&fires); got != 1 {
		t.Errorf("expected 1 callback fire, got %d", got)
	}
	if !oldIP.Equal(net.ParseIP("192.0.2.99")) {
		t.Errorf("oldIP = %v, want 192.0.2.99", oldIP)
	}
	if !newIP.Equal(originalBaseline) {
		t.Errorf("newIP = %v, want %v (the real baseline)", newIP, originalBaseline)
	}
	if capturedOld == nil || capturedNew == nil {
		t.Error("callback received nil IPs")
	}
}

func TestRoamingDetector_StopIsClean(t *testing.T) {
	d := NewRoamingDetector("1.1.1.1:443", 50*time.Millisecond, nil)
	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Stop must return without hanging
	done := make(chan struct{})
	go func() {
		d.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() did not return within 2s")
	}
}

func TestRoamingDetector_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	d := NewRoamingDetector("1.1.1.1:443", 50*time.Millisecond, nil)
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	cancel()
	// Give the loop a moment to notice
	time.Sleep(100 * time.Millisecond)

	// Stop should still be safe to call after ctx cancellation
	d.Stop()
}

func TestRoamingDetector_UnderOneSecondLatency(t *testing.T) {
	// DoD requirement: roaming → reconnect callback fires in <1s.
	// Simulate by setting PollInterval=200ms; callback should fire on next tick.
	fired := make(chan time.Time, 1)
	d := NewRoamingDetector("1.1.1.1:443", 200*time.Millisecond, func(oldIP, newIP net.IP) error {
		fired <- time.Now()
		return nil
	})
	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer d.Stop()

	// Inject a fake baseline; next poll (within 200ms) should fire callback
	d.mu.Lock()
	d.lastIP = net.ParseIP("192.0.2.99")
	d.mu.Unlock()
	injectedAt := time.Now()

	select {
	case t1 := <-fired:
		latency := t1.Sub(injectedAt)
		if latency > 1*time.Second {
			t.Errorf("roaming callback latency %v exceeds 1s DoD", latency)
		}
		t.Logf("roaming detected in %v (under 1s DoD)", latency)
	case <-time.After(1 * time.Second):
		t.Fatal("roaming callback did not fire within 1s")
	}
}
