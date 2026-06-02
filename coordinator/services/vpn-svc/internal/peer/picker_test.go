package peer

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/iogrid/iogrid/coordinator/services/vpn-svc/internal/store"
)

func newMemoryWithProvider(t *testing.T, region, status string) (store.Store, uuid.UUID) {
	t.Helper()
	st := store.NewMemory()
	mem, ok := st.(*store.Memory)
	if !ok {
		t.Fatalf("memory store assertion failed")
	}
	pid := uuid.New()
	mem.SeedProvider(pid, region, status)
	return st, pid
}

func TestPicker_RegionSpecific(t *testing.T) {
	st, want := newMemoryWithProvider(t, "us-east-1", "healthy")
	p := NewPicker(st)
	got, region, err := p.Pick(context.Background(), "us-east-1", "")
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if got != want {
		t.Errorf("provider id: want %s got %s", want, got)
	}
	if region != "us-east-1" {
		t.Errorf("region: want us-east-1 got %s", region)
	}
}

func TestPicker_AutoFallsBackToCrossRegion(t *testing.T) {
	st, _ := newMemoryWithProvider(t, "eu-west-1", "healthy")
	p := NewPicker(st)
	got, region, err := p.Pick(context.Background(), "auto", "")
	if err != nil {
		t.Fatalf("pick auto: %v", err)
	}
	if got == uuid.Nil {
		t.Error("expected non-nil provider id from auto pick")
	}
	if region != "eu-west-1" {
		t.Errorf("auto pick region: want eu-west-1 got %s", region)
	}
}

func TestPicker_EmptyRegionCoercesToAuto(t *testing.T) {
	st, _ := newMemoryWithProvider(t, "ap-south-1", "healthy")
	p := NewPicker(st)
	got, region, err := p.Pick(context.Background(), "", "203.0.113.1")
	if err != nil {
		t.Fatalf("pick empty: %v", err)
	}
	if got == uuid.Nil {
		t.Error("expected non-nil provider id from empty-region pick")
	}
	if region != "ap-south-1" {
		t.Errorf("empty region: want ap-south-1 got %s", region)
	}
}

func TestPicker_NoHealthyReturnsErrNoPeer(t *testing.T) {
	st, _ := newMemoryWithProvider(t, "us-east-1", "offline")
	p := NewPicker(st)
	_, _, err := p.Pick(context.Background(), "us-east-1", "")
	if !errors.Is(err, ErrNoPeer) {
		t.Fatalf("want ErrNoPeer got %v", err)
	}
	if code := HTTPStatusForPickError(err); code != http.StatusServiceUnavailable {
		t.Errorf("HTTP status: want 503 got %d", code)
	}
}

func TestPicker_AutoNoPeerAnywhere(t *testing.T) {
	st := store.NewMemory()
	p := NewPicker(st)
	_, _, err := p.Pick(context.Background(), "auto", "")
	if !errors.Is(err, ErrNoPeer) {
		t.Fatalf("want ErrNoPeer got %v", err)
	}
}
