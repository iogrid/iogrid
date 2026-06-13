// Package providerwallet resolves a build provider's on-chain $GRID payout
// wallet by chaining two existing lookups (#748):
//
//	provider_id --providers-svc GetProvider--> owner_user_id
//	owner_user_id --identity-svc wallet--> bound $GRID wallet
//
// The build Service calls it at terminal status so a finished build's
// grid_build_settlement row carries a non-empty provider_wallet. Without it
// the settlement-worker (which only drains rows WHERE provider_wallet <> ”)
// never pays the provider on-chain — the provider half of the G3 money path.
package providerwallet

import (
	"context"

	connect "connectrpc.com/connect"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	providersv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/gridsettle"
)

// providerGetter is the slice of providers-svc's registration client this
// resolver needs — just GetProvider. Narrowed to an interface so the resolver
// is unit-testable with a fake (the real impl is
// providersv1connect.ProviderRegistrationServiceClient).
type providerGetter interface {
	GetProvider(context.Context, *connect.Request[providersv1.GetProviderRequest]) (*connect.Response[providersv1.GetProviderResponse], error)
}

// Resolver implements gridsettle.ProviderWalletResolver.
type Resolver struct {
	// Providers calls providers-svc to map a provider id to its owner user.
	Providers providerGetter
	// Wallets maps that owner user id to their bound $GRID wallet (reuses the
	// same identity-svc resolver the customer side uses).
	Wallets gridsettle.WalletResolver
}

// ResolveProviderWallet returns the provider owner's bound $GRID wallet, or ""
// (no error) when anything along the chain is unresolved — an unbound owner,
// an unknown provider id, or unconfigured dependencies. Settlement degrades to
// a no-op rather than failing the build's terminal transition.
func (r *Resolver) ResolveProviderWallet(ctx context.Context, providerID string) (string, error) {
	if r == nil || r.Providers == nil || r.Wallets == nil || providerID == "" {
		return "", nil
	}
	resp, err := r.Providers.GetProvider(ctx, connect.NewRequest(&providersv1.GetProviderRequest{
		ProviderId: &commonv1.UUID{Value: providerID},
	}))
	if err != nil {
		return "", err
	}
	owner := resp.Msg.GetProvider().GetOwnerUserId().GetValue()
	if owner == "" {
		return "", nil
	}
	return r.Wallets.ResolveWallet(ctx, owner)
}
