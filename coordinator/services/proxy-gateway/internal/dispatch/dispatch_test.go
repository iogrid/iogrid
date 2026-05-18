package dispatch

import (
	"context"
	"errors"
	"testing"
)

func TestStaticPool_Dispatch_Empty(t *testing.T) {
	p := NewStaticPool(nil)
	_, err := p.Dispatch(context.Background(), Request{CustomerID: "c"})
	if !errors.Is(err, ErrNoEligibleProvider) {
		t.Fatalf("expected ErrNoEligibleProvider; got %v", err)
	}
}

func TestStaticPool_Dispatch_PicksOnline(t *testing.T) {
	p := NewStaticPool([]ProviderEntry{
		{ID: "off", Endpoint: "127.0.0.1:1", Online: false},
		{ID: "on", Endpoint: "127.0.0.1:2", Online: true},
	})
	asg, err := p.Dispatch(context.Background(), Request{CustomerID: "c", SessionID: "s"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if asg.ProviderID != "on" {
		t.Fatalf("provider = %q", asg.ProviderID)
	}
	if asg.WorkloadID == "" || asg.AttemptID == "" {
		t.Fatalf("ids not populated: %+v", asg)
	}
}

func TestStaticPool_Dispatch_Excludes(t *testing.T) {
	p := NewStaticPool([]ProviderEntry{
		{ID: "a", Endpoint: "127.0.0.1:1", Online: true},
		{ID: "b", Endpoint: "127.0.0.1:2", Online: true},
	})
	asg, err := p.Dispatch(context.Background(), Request{
		CustomerID: "c",
		SessionID:  "s",
		Excluded:   map[string]struct{}{"a": {}},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if asg.ProviderID != "b" {
		t.Fatalf("provider = %q (want b)", asg.ProviderID)
	}
}

func TestStaticPool_Dispatch_GeoFilter(t *testing.T) {
	p := NewStaticPool([]ProviderEntry{
		{ID: "us", Endpoint: "127.0.0.1:1", Online: true, Geo: "us-east"},
		{ID: "eu", Endpoint: "127.0.0.1:2", Online: true, Geo: "eu-west"},
	})
	asg, err := p.Dispatch(context.Background(), Request{
		CustomerID: "c",
		SessionID:  "s",
		GeoTarget:  "eu-west",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if asg.ProviderID != "eu" {
		t.Fatalf("provider = %q (want eu)", asg.ProviderID)
	}
}

func TestStaticPool_Dispatch_StickyPreferred(t *testing.T) {
	p := NewStaticPool([]ProviderEntry{
		{ID: "a", Endpoint: "127.0.0.1:1", Online: true},
		{ID: "b", Endpoint: "127.0.0.1:2", Online: true},
	})
	asg, err := p.Dispatch(context.Background(), Request{
		CustomerID:       "c",
		SessionID:        "s",
		StickyProviderID: "b",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if asg.ProviderID != "b" {
		t.Fatalf("sticky not honoured: %q", asg.ProviderID)
	}
}

func TestStaticPool_Dispatch_StickyFallbackOnOffline(t *testing.T) {
	p := NewStaticPool([]ProviderEntry{
		{ID: "a", Endpoint: "127.0.0.1:1", Online: true},
		{ID: "b", Endpoint: "127.0.0.1:2", Online: false},
	})
	asg, err := p.Dispatch(context.Background(), Request{
		CustomerID:       "c",
		SessionID:        "s",
		StickyProviderID: "b",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Sticky offline → fallback to next online.
	if asg.ProviderID != "a" {
		t.Fatalf("provider = %q (want a)", asg.ProviderID)
	}
}

func TestStaticPool_Dispatch_RoundRobin(t *testing.T) {
	p := NewStaticPool([]ProviderEntry{
		{ID: "a", Endpoint: "1", Online: true},
		{ID: "b", Endpoint: "2", Online: true},
		{ID: "c", Endpoint: "3", Online: true},
	})
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		asg, err := p.Dispatch(context.Background(), Request{CustomerID: "x", SessionID: "y"})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		seen[asg.ProviderID] = true
	}
	if len(seen) < 2 {
		t.Fatalf("round-robin only hit %d providers; expected ≥2", len(seen))
	}
}

func TestStaticPool_SetOnline(t *testing.T) {
	p := NewStaticPool([]ProviderEntry{
		{ID: "a", Endpoint: "1", Online: true},
	})
	p.SetOnline("a", false)
	if _, err := p.Dispatch(context.Background(), Request{CustomerID: "c", SessionID: "s"}); !errors.Is(err, ErrNoEligibleProvider) {
		t.Fatalf("expected ErrNoEligibleProvider after SetOnline(false); got %v", err)
	}
}
