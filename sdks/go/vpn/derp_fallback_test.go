package vpn

import (
	"context"
	"errors"
	"testing"
)

func TestDERPFallback_NoRelaysConfigured(t *testing.T) {
	d := NewDERPFallback("us-east-1", nil)
	_, err := d.Try(context.Background())
	if !errors.Is(err, ErrNoRelayConfigured) {
		t.Errorf("Try() = %v, want ErrNoRelayConfigured", err)
	}
	if d.IsAvailable() {
		t.Error("IsAvailable() = true with no relays, want false")
	}
}

func TestDERPFallback_NotYetImplemented(t *testing.T) {
	// Until issue #521 ships, even configured relays must fail loudly
	// so callers don't silently fall back to a non-existent relay.
	d := NewDERPFallback("us-east-1", []string{"derp1.iogrid.io:443"})
	endpoint, err := d.Try(context.Background())
	if err == nil {
		t.Errorf("Try() returned endpoint %q with no relay implemented", endpoint)
	}
	// IsAvailable should also be false in this MVP phase
	if d.IsAvailable() {
		t.Error("IsAvailable() = true before #521 ships, want false")
	}
}
