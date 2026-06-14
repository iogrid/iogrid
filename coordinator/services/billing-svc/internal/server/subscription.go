// subscription.go implements the read-side of the Connect-RPC
// SubscriptionService.
//
// GetSubscription (#802) + ListUsage (#675) are live.
//   - ListUsage backs the customer /usage surface (gateway-bff
//     GetCustomerUsage → web /customer/usage).
//   - GetSubscription backs /api/v1/vpn/account (gateway-bff
//     GetVPNAccount → web /customer/billing + /vpn). It returns the
//     workspace's subscription row, or an empty response (subscription
//     == nil) for the common no-subscription / free-tier case — the BFF
//     already maps that nil to a "FREE" tier view. It previously fell
//     through to the embedded Unimplemented stub, so gateway-bff
//     surfaced a 501 on every billing page view (#802; same class as
//     #686 CreateCheckoutSession / #675 ListUsage).
//
// CreateCheckoutSession / CreatePortalSession bind to stripeapi (#686).
// ListInvoices / CancelSubscription stay CodeUnimplemented via the
// embedded stub until the Stripe wiring ships.
package server

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	billingv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1/billingv1connect"
	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/store"
	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/stripeapi"
)

// SubscriptionHandler implements billingv1connect.SubscriptionServiceHandler.
type SubscriptionHandler struct {
	billingv1connect.UnimplementedSubscriptionServiceHandler
	Store *store.Store
	// Stripe backs CreateCheckoutSession (#686). Optional — when nil
	// the RPC returns CodeFailedPrecondition ("stripe disabled") so the
	// caller surfaces a real error instead of the Unimplemented stub
	// the web's ApiClient silently masks as an empty object.
	Stripe *stripeapi.Service
	// Now lets the test layer pin time; defaults to time.Now (UTC).
	Now func() time.Time
}

// NewSubscriptionHandler wires the dependencies.
func NewSubscriptionHandler(s *store.Store, stripe *stripeapi.Service) *SubscriptionHandler {
	return &SubscriptionHandler{
		Store:  s,
		Stripe: stripe,
		Now:    func() time.Time { return time.Now().UTC() },
	}
}

// tierWireName maps the proto enum to the short tier key stripeapi uses
// for its STRIPE_PRICE_<TIER> env lookup (same names the REST surface
// accepts in parseTier's canonical forms).
func tierWireName(t billingv1.SubscriptionTier) string {
	switch t {
	case billingv1.SubscriptionTier_SUBSCRIPTION_TIER_STARTER:
		return "starter"
	case billingv1.SubscriptionTier_SUBSCRIPTION_TIER_GROWTH:
		return "growth"
	case billingv1.SubscriptionTier_SUBSCRIPTION_TIER_ENTERPRISE:
		return "enterprise"
	case billingv1.SubscriptionTier_SUBSCRIPTION_TIER_PAYG:
		return "payg"
	default:
		return ""
	}
}

// statusFromString maps the textual lifecycle status stored on the
// subscription row (Stripe lifecycle strings — "active", "trialing",
// "past_due", "canceled", "incomplete", "unpaid", lower-case as the
// stripeapi webhook persists them) onto the proto enum. Unknown values
// fall back to UNSPECIFIED rather than guessing a state.
func statusFromString(s string) billingv1.SubscriptionStatus {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "active":
		return billingv1.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE
	case "trialing":
		return billingv1.SubscriptionStatus_SUBSCRIPTION_STATUS_TRIALING
	case "past_due":
		return billingv1.SubscriptionStatus_SUBSCRIPTION_STATUS_PAST_DUE
	case "canceled", "cancelled":
		return billingv1.SubscriptionStatus_SUBSCRIPTION_STATUS_CANCELED
	case "incomplete":
		return billingv1.SubscriptionStatus_SUBSCRIPTION_STATUS_INCOMPLETE
	case "unpaid":
		return billingv1.SubscriptionStatus_SUBSCRIPTION_STATUS_UNPAID
	default:
		return billingv1.SubscriptionStatus_SUBSCRIPTION_STATUS_UNSPECIFIED
	}
}

// subscriptionToProto maps a store.Subscription row onto the wire
// message. Nullable period bounds / canceled_at become absent proto
// fields (nil timestamp) rather than zero-time sentinels.
func subscriptionToProto(s *store.Subscription) *billingv1.Subscription {
	if s == nil {
		return nil
	}
	out := &billingv1.Subscription{
		Id:                   &commonv1.UUID{Value: s.ID.String()},
		WorkspaceId:          &commonv1.UUID{Value: s.WorkspaceID.String()},
		Tier:                 tierFromString(s.Tier),
		Status:               statusFromString(s.Status),
		StripeCustomerId:     s.StripeCustomerID,
		StripeSubscriptionId: s.StripeSubscriptionID,
		CreatedAt:            timestamppb.New(s.CreatedAt),
	}
	if s.CurrentPeriodStart != nil || s.CurrentPeriodEnd != nil {
		win := &commonv1.TimeWindow{}
		if s.CurrentPeriodStart != nil {
			win.Start = timestamppb.New(*s.CurrentPeriodStart)
		}
		if s.CurrentPeriodEnd != nil {
			win.End = timestamppb.New(*s.CurrentPeriodEnd)
		}
		out.CurrentPeriod = win
	}
	if s.CanceledAt != nil {
		out.CanceledAt = timestamppb.New(*s.CanceledAt)
	}
	return out
}

// GetSubscription returns the workspace's current subscription, backing
// gateway-bff's GetVPNAccount (/api/v1/vpn/account → web /customer/billing
// + /vpn). The common case is NO subscription (prepaid-$GRID / free
// tier): we return an empty response (subscription == nil), NOT an
// error — the BFF maps nil onto the public "FREE" tier view. Returning
// an error here (it previously fell through to the embedded
// Unimplemented stub → CodeUnimplemented → gateway-bff 501) ships a
// console error on every billing page view (#802; the #686/#675 class).
func (h *SubscriptionHandler) GetSubscription(
	ctx context.Context,
	req *connect.Request[billingv1.GetSubscriptionRequest],
) (*connect.Response[billingv1.GetSubscriptionResponse], error) {
	wsID, err := parseUUID(req.Msg.GetWorkspaceId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	sub, err := h.Store.GetSubscriptionByWorkspace(ctx, wsID)
	if errors.Is(err, store.ErrNotFound) {
		// No subscription on file → the free / prepaid-$GRID tier. Empty
		// (non-error) response so the caller renders the free-tier view
		// instead of a masked 501.
		return connect.NewResponse(&billingv1.GetSubscriptionResponse{}), nil
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&billingv1.GetSubscriptionResponse{
		Subscription: subscriptionToProto(sub),
	}), nil
}

// CreateCheckoutSession binds the Connect-RPC to the same stripeapi
// path billing-svc's REST surface uses (#686 — the RPC was the embedded
// Unimplemented stub, so gateway-bff's /api/v1/vpn/upgrade returned
// 501, the web ApiClient masked it as {}, and 'Choose Plus' navigated
// the browser to the literal URL "undefined").
func (h *SubscriptionHandler) CreateCheckoutSession(
	ctx context.Context,
	req *connect.Request[billingv1.CreateCheckoutSessionRequest],
) (*connect.Response[billingv1.CreateCheckoutSessionResponse], error) {
	wsRaw := req.Msg.GetWorkspaceId().GetValue()
	workspaceID, err := uuid.Parse(wsRaw)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid workspace_id %q", wsRaw))
	}
	tier := tierWireName(req.Msg.GetDesiredTier())
	if tier == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("desired_tier required"))
	}
	if req.Msg.GetSuccessUrl() == "" || req.Msg.GetCancelUrl() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("success_url and cancel_url required"))
	}
	if h.Stripe == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("stripe disabled: no payment backend configured"))
	}
	url, err := h.Stripe.CreateCheckoutSession(ctx, workspaceID, tier, req.Msg.GetSuccessUrl(), req.Msg.GetCancelUrl())
	if err != nil {
		if stripeapi.IsStripeDisabled(err) {
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		}
		return nil, connect.NewError(connect.CodeUnavailable, err)
	}
	return connect.NewResponse(&billingv1.CreateCheckoutSessionResponse{CheckoutUrl: url}), nil
}

// CreatePortalSession binds the Connect-RPC to stripeapi's Customer
// Portal path (pre-emptive #686-class fix: the embedded Unimplemented
// stub would surface as a masked 501→{} the day a web "Manage
// subscription" button ships — bind it with honest codes now, while
// the pattern is fresh).
func (h *SubscriptionHandler) CreatePortalSession(
	ctx context.Context,
	req *connect.Request[billingv1.CreatePortalSessionRequest],
) (*connect.Response[billingv1.CreatePortalSessionResponse], error) {
	wsRaw := req.Msg.GetWorkspaceId().GetValue()
	workspaceID, err := uuid.Parse(wsRaw)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid workspace_id %q", wsRaw))
	}
	if req.Msg.GetReturnUrl() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("return_url required"))
	}
	if h.Stripe == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("stripe disabled: no payment backend configured"))
	}
	url, err := h.Stripe.CreatePortalSession(ctx, workspaceID, req.Msg.GetReturnUrl())
	if err != nil {
		if stripeapi.IsStripeDisabled(err) {
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		}
		return nil, connect.NewError(connect.CodeUnavailable, err)
	}
	return connect.NewResponse(&billingv1.CreatePortalSessionResponse{PortalUrl: url}), nil
}

// listUsageDefaultPageSize / Max bound the page; PageRequest.page_size 0
// means "server default" per the proto contract.
const (
	listUsageDefaultPageSize = 100
	listUsageMaxPageSize     = 500
	// listUsageDefaultWindow is applied when the request carries no
	// TimeWindow: the trailing 30 days, matching the /customer/usage
	// page's headline cards.
	listUsageDefaultWindow = 30 * 24 * time.Hour
)

// workloadTypeFromText maps usage_event.workload_type — the short TEXT
// names the metering consumer persists (DOCKER | GPU | IOS_BUILD |
// BANDWIDTH, plus variants like BANDWIDTH_VPN) — onto the common
// WorkloadType enum the wire contract uses.
func workloadTypeFromText(t string) commonv1.WorkloadType {
	switch {
	case strings.HasPrefix(t, "BANDWIDTH"):
		return commonv1.WorkloadType_WORKLOAD_TYPE_BANDWIDTH
	case strings.HasPrefix(t, "DOCKER"):
		return commonv1.WorkloadType_WORKLOAD_TYPE_DOCKER
	case strings.HasPrefix(t, "GPU"):
		return commonv1.WorkloadType_WORKLOAD_TYPE_GPU
	case strings.HasPrefix(t, "IOS_BUILD"):
		return commonv1.WorkloadType_WORKLOAD_TYPE_IOS_BUILD
	default:
		return commonv1.WorkloadType_WORKLOAD_TYPE_UNSPECIFIED
	}
}

// typePrefixFromEnum is the inverse mapping for the request's
// type_filter → the workload_type TEXT prefix used in the store query.
// UNSPECIFIED means "no filter".
func typePrefixFromEnum(t commonv1.WorkloadType) string {
	switch t {
	case commonv1.WorkloadType_WORKLOAD_TYPE_BANDWIDTH:
		return "BANDWIDTH"
	case commonv1.WorkloadType_WORKLOAD_TYPE_DOCKER:
		return "DOCKER"
	case commonv1.WorkloadType_WORKLOAD_TYPE_GPU:
		return "GPU"
	case commonv1.WorkloadType_WORKLOAD_TYPE_IOS_BUILD:
		return "IOS_BUILD"
	default:
		return ""
	}
}

// ListUsage returns a workspace's metered usage_event rows for a time
// window, newest first, with a page cost subtotal. Pagination is an
// opaque offset token (decimal string) — fine at current volumes; swap
// to a keyset token if usage_event grows past offset-scan comfort.
func (h *SubscriptionHandler) ListUsage(
	ctx context.Context,
	req *connect.Request[billingv1.ListUsageRequest],
) (*connect.Response[billingv1.ListUsageResponse], error) {
	wsID, err := parseUUID(req.Msg.GetWorkspaceId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	// Window: default to the trailing 30 days; honour either bound when
	// supplied (TimeWindow is half-open [start, end)).
	now := h.Now()
	start := now.Add(-listUsageDefaultWindow)
	end := now
	if w := req.Msg.GetWindow(); w != nil {
		if ts := w.GetStart(); ts != nil {
			start = ts.AsTime()
		}
		if ts := w.GetEnd(); ts != nil {
			end = ts.AsTime()
		}
	}

	// Page: 0 → default; cap the max so a single call can't dump the
	// whole table. The token is an opaque decimal offset.
	size := int(req.Msg.GetPage().GetPageSize())
	if size <= 0 {
		size = listUsageDefaultPageSize
	}
	if size > listUsageMaxPageSize {
		size = listUsageMaxPageSize
	}
	offset := 0
	if tok := req.Msg.GetPage().GetPageToken(); tok != "" {
		o, err := strconv.Atoi(tok)
		if err != nil || o < 0 {
			return nil, connect.NewError(
				connect.CodeInvalidArgument,
				errors.New("invalid page_token"),
			)
		}
		offset = o
	}

	// Fetch size+1 to learn whether another page exists without a
	// second COUNT query; the sentinel row is trimmed before encoding.
	rows, subtotalCents, err := h.Store.ListUsageEvents(
		ctx, wsID, start, end,
		typePrefixFromEnum(req.Msg.GetTypeFilter()),
		size+1, offset,
	)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	nextToken := ""
	if len(rows) > size {
		// Sentinel hit: more rows exist. Its cost must not leak into
		// this page's subtotal.
		subtotalCents -= rows[size].CostCents
		rows = rows[:size]
		nextToken = strconv.Itoa(offset + size)
	}

	usage := make([]*billingv1.UsageRecord, 0, len(rows))
	currency := "USD"
	for _, e := range rows {
		if e.Currency != "" {
			currency = e.Currency
		}
		rec := &billingv1.UsageRecord{
			Id:          &commonv1.UUID{Value: e.ID.String()},
			WorkspaceId: &commonv1.UUID{Value: e.WorkspaceID.String()},
			WorkloadId:  &commonv1.UUID{Value: e.WorkloadID.String()},
			Type:        workloadTypeFromText(e.WorkloadType),
			Quantity:    uint64(e.Quantity),
			Cost: &commonv1.Money{
				Currency: e.Currency,
				// cost_cents → Money.micros (millionths of the major
				// unit): 1 cent = 10_000 micros.
				Micros: e.CostCents * 10_000,
			},
			RecordedAt: timestamppb.New(e.RecordedAt),
		}
		usage = append(usage, rec)
	}

	return connect.NewResponse(&billingv1.ListUsageResponse{
		Usage: usage,
		Page:  &commonv1.PageResponse{NextPageToken: nextToken},
		PageSubtotal: &commonv1.Money{
			Currency: currency,
			Micros:   subtotalCents * 10_000,
		},
	}), nil
}
