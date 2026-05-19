package offramp_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/offramp"
)

// stubProvider is a minimal Provider used only for registry tests.
type stubProvider struct{ name string }

func (s *stubProvider) Name() string                                            { return s.name }
func (s *stubProvider) BuildRedirectURL(offramp.OffRampRequest) (string, error) { return "", nil }
func (s *stubProvider) VerifyWebhookSignature([]byte, string) bool              { return true }
func (s *stubProvider) ParseWebhook([]byte) (*offramp.OffRampStatus, error)     { return nil, nil }

func TestRegistry_RegisterAndGetProvider(t *testing.T) {
	r := offramp.NewRegistry()
	if err := r.Register(&stubProvider{name: "moonpay"}); err != nil {
		t.Fatalf("Register moonpay: %v", err)
	}
	if err := r.Register(&stubProvider{name: "sociable-cash"}); err != nil {
		t.Fatalf("Register sociable-cash: %v", err)
	}

	got, err := r.GetProvider("moonpay")
	if err != nil {
		t.Fatalf("GetProvider moonpay: %v", err)
	}
	if got.Name() != "moonpay" {
		t.Fatalf("GetProvider returned wrong adapter: %s", got.Name())
	}
}

func TestRegistry_DuplicateRegistrationFails(t *testing.T) {
	r := offramp.NewRegistry()
	if err := r.Register(&stubProvider{name: "moonpay"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	err := r.Register(&stubProvider{name: "moonpay"})
	if err == nil {
		t.Fatalf("expected duplicate registration to fail")
	}
}

func TestRegistry_NilProviderRefused(t *testing.T) {
	r := offramp.NewRegistry()
	if err := r.Register(nil); err == nil {
		t.Fatalf("expected nil provider to be refused")
	}
}

func TestRegistry_EmptyNameRefused(t *testing.T) {
	r := offramp.NewRegistry()
	if err := r.Register(&stubProvider{name: ""}); err == nil {
		t.Fatalf("expected empty name to be refused")
	}
}

func TestRegistry_GetProviderUnknown(t *testing.T) {
	r := offramp.NewRegistry()
	_, err := r.GetProvider("bogus")
	if !errors.Is(err, offramp.ErrUnknownProvider) {
		t.Fatalf("expected ErrUnknownProvider, got %v", err)
	}
}

func TestRegistry_ListAvailablePreservesInsertionOrder(t *testing.T) {
	r := offramp.NewRegistry()
	for _, n := range []string{"moonpay", "sociable-cash", "coinbase"} {
		if err := r.Register(&stubProvider{name: n}); err != nil {
			t.Fatalf("Register %s: %v", n, err)
		}
	}
	got := r.ListAvailable()
	want := []string{"moonpay", "sociable-cash", "coinbase"}
	if len(got) != len(want) {
		t.Fatalf("ListAvailable len=%d, want %d", len(got), len(want))
	}
	for i, p := range got {
		if p.Name() != want[i] {
			t.Fatalf("ListAvailable[%d]=%q, want %q", i, p.Name(), want[i])
		}
	}
}

func TestRegistry_NamesSorted(t *testing.T) {
	r := offramp.NewRegistry()
	for _, n := range []string{"sociable-cash", "coinbase", "moonpay"} {
		_ = r.Register(&stubProvider{name: n})
	}
	want := []string{"coinbase", "moonpay", "sociable-cash"}
	if !reflect.DeepEqual(r.Names(), want) {
		t.Fatalf("Names()=%v, want %v", r.Names(), want)
	}
}

func TestParseProvidersEnv(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"moonpay", []string{"moonpay"}},
		{"moonpay,sociable-cash", []string{"moonpay", "sociable-cash"}},
		{"moonpay, sociable-cash , Coinbase", []string{"moonpay", "sociable-cash", "coinbase"}},
		{"moonpay,,sociable-cash", []string{"moonpay", "sociable-cash"}},
	}
	for _, c := range cases {
		got := offramp.ParseProvidersEnv(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseProvidersEnv(%q)=%v, want %v", c.in, got, c.want)
		}
	}
}
