// earnings.go implements the Connect-RPC EarningsService — the
// headline-card surface for /provide/earnings (#324).
//
// Three RPCs:
//
//	GetEarningsSummary(provider_id) → totals + workload count + currency.
//	GetPayoutMethod(user_id)        → user's payout election (default UNSPECIFIED).
//	SetPayoutMethod(user_id, ...)   → persist election; last write wins.
//
// providers-svc keeps its own GetEarningsSummary for the transparency
// feed (window + breakdown by workload type); this surface answers the
// management-plane headline shape and lives next to the money state.
package server

import (
	"context"
	"errors"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	billingv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1/billingv1connect"
	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/store"
)

// EarningsHandler implements billingv1connect.EarningsServiceHandler.
type EarningsHandler struct {
	billingv1connect.UnimplementedEarningsServiceHandler
	Store *store.Store
	// Now lets the test layer pin time; defaults to time.Now.
	Now func() time.Time
}

// NewEarningsHandler wires the dependency.
func NewEarningsHandler(s *store.Store) *EarningsHandler {
	return &EarningsHandler{Store: s, Now: func() time.Time { return time.Now().UTC() }}
}

// GetEarningsSummary returns the five-figure headline aggregation. Zero
// metered events ⇒ zero totals (lifetime/last_30d/last_7d/pending) with
// currency "GRID" — that's the Phase-0 empty-state contract (#312/#315).
func (h *EarningsHandler) GetEarningsSummary(
	ctx context.Context,
	req *connect.Request[billingv1.GetEarningsSummaryRequest],
) (*connect.Response[billingv1.GetEarningsSummaryResponse], error) {
	pid, err := parseUUID(req.Msg.GetProviderId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	now := h.Now()
	t, err := h.Store.SumProviderEarnings(ctx, pid, now)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	currency := strings.TrimSpace(t.Currency)
	if currency == "" {
		currency = "GRID"
	}
	out := &billingv1.EarningsSummary{
		ProviderId:        &commonv1.UUID{Value: pid.String()},
		TotalEarned:       &commonv1.Money{Currency: currency, Micros: t.LifetimeMicros},
		Last_30D:          &commonv1.Money{Currency: currency, Micros: t.Last30DMicros},
		Last_7D:           &commonv1.Money{Currency: currency, Micros: t.Last7DMicros},
		PendingPayout:     &commonv1.Money{Currency: currency, Micros: t.PendingPayoutMicros},
		LifetimeWorkloads: t.LifetimeWorkloads,
		ComputedAt:        timestamppb.New(now),
		// On-chain settled $GRID half (#758): the real money that moved on
		// devnet for this provider's builds. SettledGrid is always $GRID
		// (the native settlement currency) regardless of the usage_event
		// currency default; SettledBuilds is the dashboard "builds" number.
		SettledBuilds: t.SettledBuilds,
		SettledGrid:   &commonv1.Money{Currency: "GRID", Micros: t.SettledGridMicros},
	}
	return connect.NewResponse(&billingv1.GetEarningsSummaryResponse{Summary: out}), nil
}

// GetPayoutMethod returns the user's payout election. When no row
// exists the store returns an UNSPECIFIED placeholder (NOT NotFound)
// so the web client can render the default "Hold $GRID" tile selected.
func (h *EarningsHandler) GetPayoutMethod(
	ctx context.Context,
	req *connect.Request[billingv1.GetPayoutMethodRequest],
) (*connect.Response[billingv1.GetPayoutMethodResponse], error) {
	uid, err := parseUUID(req.Msg.GetUserId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	m, err := h.Store.GetPayoutMethod(ctx, uid)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := payoutMethodToProto(m)
	return connect.NewResponse(&billingv1.GetPayoutMethodResponse{Method: out}), nil
}

// SetPayoutMethod persists the election. Validates that the kind is
// one of the four known enum values; UNSPECIFIED is allowed (the user
// can opt back to "hold $GRID" after choosing cash/charity).
func (h *EarningsHandler) SetPayoutMethod(
	ctx context.Context,
	req *connect.Request[billingv1.SetPayoutMethodRequest],
) (*connect.Response[billingv1.SetPayoutMethodResponse], error) {
	uid, err := parseUUID(req.Msg.GetUserId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	kindStr, ok := payoutMethodKindToString(req.Msg.GetKind())
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown PayoutMethodKind"))
	}
	dst := strings.TrimSpace(req.Msg.GetDestinationAddress())
	charity := strings.TrimSpace(req.Msg.GetCharityId())
	// Light shape checks — full validation (wallet-checksum / charity
	// catalog membership) is a Phase 1 concern.
	switch kindStr {
	case "CASH_USDC":
		if dst == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("destination_address required for CASH_USDC"))
		}
	case "CHARITY":
		if charity == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("charity_id required for CHARITY"))
		}
	}
	row := store.PayoutMethod{
		UserID:             uid,
		Kind:               kindStr,
		DestinationAddress: dst,
		CharityID:          charity,
	}
	if err := h.Store.UpsertPayoutMethod(ctx, row); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	saved, err := h.Store.GetPayoutMethod(ctx, uid)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&billingv1.SetPayoutMethodResponse{Method: payoutMethodToProto(saved)}), nil
}

// ── helpers ─────────────────────────────────────────────────────────

func payoutMethodToProto(m *store.PayoutMethod) *billingv1.PayoutMethod {
	out := &billingv1.PayoutMethod{
		UserId:             &commonv1.UUID{Value: m.UserID.String()},
		Kind:               payoutMethodKindFromString(m.Kind),
		DestinationAddress: m.DestinationAddress,
		CharityId:          m.CharityID,
	}
	if !m.UpdatedAt.IsZero() {
		out.UpdatedAt = timestamppb.New(m.UpdatedAt)
	}
	return out
}

func payoutMethodKindFromString(s string) billingv1.PayoutMethodKind {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "CASH_USDC":
		return billingv1.PayoutMethodKind_PAYOUT_METHOD_KIND_CASH_USDC
	case "FREE_VPN":
		return billingv1.PayoutMethodKind_PAYOUT_METHOD_KIND_FREE_VPN
	case "CHARITY":
		return billingv1.PayoutMethodKind_PAYOUT_METHOD_KIND_CHARITY
	default:
		return billingv1.PayoutMethodKind_PAYOUT_METHOD_KIND_UNSPECIFIED
	}
}

// payoutMethodKindToString maps the proto enum to the canonical text
// stored in payout_methods.kind. Returns (s, true) for every known
// value (including UNSPECIFIED — the user can revert to default).
func payoutMethodKindToString(k billingv1.PayoutMethodKind) (string, bool) {
	switch k {
	case billingv1.PayoutMethodKind_PAYOUT_METHOD_KIND_UNSPECIFIED:
		return "UNSPECIFIED", true
	case billingv1.PayoutMethodKind_PAYOUT_METHOD_KIND_CASH_USDC:
		return "CASH_USDC", true
	case billingv1.PayoutMethodKind_PAYOUT_METHOD_KIND_FREE_VPN:
		return "FREE_VPN", true
	case billingv1.PayoutMethodKind_PAYOUT_METHOD_KIND_CHARITY:
		return "CHARITY", true
	default:
		return "", false
	}
}

