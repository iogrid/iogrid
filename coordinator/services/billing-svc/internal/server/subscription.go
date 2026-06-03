// subscription.go implements the read-side of the Connect-RPC
// SubscriptionService.
//
// Only ListUsage is live (#675) — it backs the customer /usage surface
// (gateway-bff GetCustomerUsage → web /customer/usage), which returned
// 501 Not Implemented until this landed (the deep authenticated UAT
// found the page silently masking the 501 as "$0.00 / No usage").
//
// CreateCheckoutSession / CreatePortalSession / ListInvoices /
// CancelSubscription stay CodeUnimplemented via the embedded stub until
// the Stripe wiring ships.
package server

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	billingv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1/billingv1connect"
	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/store"
)

// SubscriptionHandler implements billingv1connect.SubscriptionServiceHandler.
type SubscriptionHandler struct {
	billingv1connect.UnimplementedSubscriptionServiceHandler
	Store *store.Store
	// Now lets the test layer pin time; defaults to time.Now (UTC).
	Now func() time.Time
}

// NewSubscriptionHandler wires the dependency.
func NewSubscriptionHandler(s *store.Store) *SubscriptionHandler {
	return &SubscriptionHandler{Store: s, Now: func() time.Time { return time.Now().UTC() }}
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
