package providerwallet

import (
	"context"
	"errors"
	"testing"

	connect "connectrpc.com/connect"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	providersv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1"
)

// fakeProviders is a providerGetter that returns a fixed owner (or error).
type fakeProviders struct {
	owner    string
	nilOwner bool // return a Provider with no owner_user_id
	err      error
	gotID    string
}

func (f *fakeProviders) GetProvider(_ context.Context, req *connect.Request[providersv1.GetProviderRequest]) (*connect.Response[providersv1.GetProviderResponse], error) {
	f.gotID = req.Msg.GetProviderId().GetValue()
	if f.err != nil {
		return nil, f.err
	}
	p := &providersv1.Provider{}
	if !f.nilOwner {
		p.OwnerUserId = &commonv1.UUID{Value: f.owner}
	}
	return connect.NewResponse(&providersv1.GetProviderResponse{Provider: p}), nil
}

// fakeWallets maps an owner user id to a bound wallet.
type fakeWallets map[string]string

func (f fakeWallets) ResolveWallet(_ context.Context, userID string) (string, error) {
	return f[userID], nil
}

func TestResolveProviderWallet_HappyPath(t *testing.T) {
	prov := &fakeProviders{owner: "owner-1"}
	r := &Resolver{Providers: prov, Wallets: fakeWallets{"owner-1": "WALLET_ABC"}}

	got, err := r.ResolveProviderWallet(context.Background(), "prov-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "WALLET_ABC" {
		t.Fatalf("wallet = %q, want WALLET_ABC", got)
	}
	if prov.gotID != "prov-1" {
		t.Fatalf("GetProvider called with %q, want prov-1", prov.gotID)
	}
}

func TestResolveProviderWallet_EmptyProviderID(t *testing.T) {
	prov := &fakeProviders{owner: "owner-1"}
	r := &Resolver{Providers: prov, Wallets: fakeWallets{"owner-1": "WALLET_ABC"}}

	got, err := r.ResolveProviderWallet(context.Background(), "")
	if err != nil || got != "" {
		t.Fatalf("empty providerID: got (%q,%v), want (\"\",nil)", got, err)
	}
	if prov.gotID != "" {
		t.Fatalf("GetProvider should not be called for empty providerID")
	}
}

func TestResolveProviderWallet_UnboundOwner(t *testing.T) {
	// Provider resolves but the owner has no bound wallet → "" no error.
	r := &Resolver{Providers: &fakeProviders{owner: "owner-x"}, Wallets: fakeWallets{}}
	got, err := r.ResolveProviderWallet(context.Background(), "prov-1")
	if err != nil || got != "" {
		t.Fatalf("unbound owner: got (%q,%v), want (\"\",nil)", got, err)
	}
}

func TestResolveProviderWallet_NoOwnerOnProvider(t *testing.T) {
	r := &Resolver{Providers: &fakeProviders{nilOwner: true}, Wallets: fakeWallets{"": "SHOULD_NOT_USE"}}
	got, err := r.ResolveProviderWallet(context.Background(), "prov-1")
	if err != nil || got != "" {
		t.Fatalf("no owner: got (%q,%v), want (\"\",nil)", got, err)
	}
}

func TestResolveProviderWallet_GetProviderError(t *testing.T) {
	r := &Resolver{Providers: &fakeProviders{err: errors.New("boom")}, Wallets: fakeWallets{}}
	_, err := r.ResolveProviderWallet(context.Background(), "prov-1")
	if err == nil {
		t.Fatalf("expected GetProvider error to propagate")
	}
}

func TestResolveProviderWallet_NilDeps(t *testing.T) {
	// Missing dependencies degrade to a no-op, never panic.
	r := &Resolver{}
	got, err := r.ResolveProviderWallet(context.Background(), "prov-1")
	if err != nil || got != "" {
		t.Fatalf("nil deps: got (%q,%v), want (\"\",nil)", got, err)
	}
}
