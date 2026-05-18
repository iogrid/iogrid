package relay

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// TestRun_BasicForwarding verifies bytes flow both directions and the
// counters reflect the totals.
func TestRun_BasicForwarding(t *testing.T) {
	custA, custB := net.Pipe()
	provA, provB := net.Pipe()
	// custA is the customer-facing side, custB is the test client.
	// provA is the provider-facing side, provB is the test "provider".
	defer custA.Close()
	defer provA.Close()
	defer custB.Close()
	defer provB.Close()

	go func() {
		_, _ = custB.Write([]byte("hello-provider"))
		// net.Pipe doesn't expose CloseWrite, so just close after
		// writing — the test only needs the EOF signal.
		_ = custB.Close()
	}()
	go func() {
		_, _ = provB.Write([]byte("hello-customer-bytes"))
		_ = provB.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	counters, _ := Run(ctx, custA, provA, Options{})
	if counters.BytesOut == 0 || counters.BytesIn == 0 {
		t.Fatalf("counters = %+v", counters)
	}
	if counters.Duration <= 0 {
		t.Fatalf("duration not positive: %v", counters.Duration)
	}
}

// TestRun_MeterFiresEveryThreshold verifies meter callback cadence.
func TestRun_MeterFiresEveryThreshold(t *testing.T) {
	custA, custB := net.Pipe()
	provA, provB := net.Pipe()
	defer custA.Close()
	defer provA.Close()
	defer custB.Close()
	defer provB.Close()

	payload := make([]byte, 32*1024) // 32KB
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	go func() {
		for i := 0; i < 4; i++ {
			_, _ = custB.Write(payload)
		}
		_ = custB.Close()
	}()
	go func() {
		// Drain provider side.
		buf := make([]byte, 4096)
		for {
			if _, err := provB.Read(buf); err != nil {
				return
			}
		}
	}()

	var callCount atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	counters, _ := Run(ctx, custA, provA, Options{
		MeterEvery: 64 * 1024,
		Meter: func(_ context.Context, _, _ uint64) error {
			callCount.Add(1)
			return nil
		},
	})
	// We wrote 128 KB → expect at least 2 meter ticks + 1 terminal.
	if callCount.Load() < 2 {
		t.Fatalf("meter callback fired only %d times; expected ≥2", callCount.Load())
	}
	if counters.BytesOut < 100*1024 {
		t.Fatalf("bytes out = %d, want ≥100 KB", counters.BytesOut)
	}
}

// TestRun_CtxCancel terminates the relay when ctx is cancelled.
func TestRun_CtxCancel(t *testing.T) {
	custA, custB := net.Pipe()
	provA, _ := net.Pipe()
	defer custA.Close()
	defer custB.Close()
	defer provA.Close()

	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})
	go func() {
		Run(ctx, custA, provA, Options{}) //nolint:errcheck
		close(doneCh)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}
