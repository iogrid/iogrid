package server

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	billingv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1"
	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
)

// CreateCheckoutSession contract tests (#686): the RPC must return REAL
// error codes — the Unimplemented stub was silently masked by the web's
// ApiClient and navigated the browser to "undefined".

func validCheckoutReq() *billingv1.CreateCheckoutSessionRequest {
	return &billingv1.CreateCheckoutSessionRequest{
		WorkspaceId: &commonv1.UUID{Value: uuid.NewString()},
		DesiredTier: billingv1.SubscriptionTier_SUBSCRIPTION_TIER_STARTER,
		SuccessUrl:  "https://iogrid.org/customer/billing?status=success",
		CancelUrl:   "https://iogrid.org/vpn/upgrade",
	}
}

func TestCreateCheckoutSession_NilStripeIsFailedPrecondition(t *testing.T) {
	h := NewSubscriptionHandler(nil, nil)
	_, err := h.CreateCheckoutSession(context.Background(), connect.NewRequest(validCheckoutReq()))
	if got := connect.CodeOf(err); got != connect.CodeFailedPrecondition {
		t.Fatalf("code = %v, want FailedPrecondition (err=%v)", got, err)
	}
}

func TestCreateCheckoutSession_BadWorkspaceID(t *testing.T) {
	h := NewSubscriptionHandler(nil, nil)
	req := validCheckoutReq()
	req.WorkspaceId = &commonv1.UUID{Value: "not-a-uuid"}
	_, err := h.CreateCheckoutSession(context.Background(), connect.NewRequest(req))
	if got := connect.CodeOf(err); got != connect.CodeInvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument (err=%v)", got, err)
	}
}

func TestCreateCheckoutSession_UnspecifiedTier(t *testing.T) {
	h := NewSubscriptionHandler(nil, nil)
	req := validCheckoutReq()
	req.DesiredTier = billingv1.SubscriptionTier_SUBSCRIPTION_TIER_UNSPECIFIED
	_, err := h.CreateCheckoutSession(context.Background(), connect.NewRequest(req))
	if got := connect.CodeOf(err); got != connect.CodeInvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument (err=%v)", got, err)
	}
}

func TestCreateCheckoutSession_MissingURLs(t *testing.T) {
	h := NewSubscriptionHandler(nil, nil)
	req := validCheckoutReq()
	req.SuccessUrl = ""
	_, err := h.CreateCheckoutSession(context.Background(), connect.NewRequest(req))
	if got := connect.CodeOf(err); got != connect.CodeInvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument (err=%v)", got, err)
	}
}
