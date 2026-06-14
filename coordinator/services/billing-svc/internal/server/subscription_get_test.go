package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"

	billingv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1"
	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/store"
)

// GetSubscription backs gateway-bff's /api/v1/vpn/account (web
// /customer/billing + /vpn). It was an Unimplemented stub → 501 on every
// billing page view (#802). These tests pin: the status enum mapping,
// that the row→proto mapper drops no field (the #709/#725 in-memory-
// green/Postgres-broken class), and the input-validation guard.

func TestStatusFromString(t *testing.T) {
	cases := []struct {
		text string
		want billingv1.SubscriptionStatus
	}{
		// Stripe lifecycle strings as the webhook persists them (lower-case).
		{"active", billingv1.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE},
		{"trialing", billingv1.SubscriptionStatus_SUBSCRIPTION_STATUS_TRIALING},
		{"past_due", billingv1.SubscriptionStatus_SUBSCRIPTION_STATUS_PAST_DUE},
		{"canceled", billingv1.SubscriptionStatus_SUBSCRIPTION_STATUS_CANCELED},
		// British spelling tolerated.
		{"cancelled", billingv1.SubscriptionStatus_SUBSCRIPTION_STATUS_CANCELED},
		{"incomplete", billingv1.SubscriptionStatus_SUBSCRIPTION_STATUS_INCOMPLETE},
		{"unpaid", billingv1.SubscriptionStatus_SUBSCRIPTION_STATUS_UNPAID},
		// Case / whitespace tolerance.
		{"  ACTIVE  ", billingv1.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE},
		// Unknown / empty → UNSPECIFIED, never a guessed state.
		{"", billingv1.SubscriptionStatus_SUBSCRIPTION_STATUS_UNSPECIFIED},
		{"weird_new_status", billingv1.SubscriptionStatus_SUBSCRIPTION_STATUS_UNSPECIFIED},
	}
	for _, c := range cases {
		if got := statusFromString(c.text); got != c.want {
			t.Errorf("statusFromString(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestSubscriptionToProto_Nil(t *testing.T) {
	if got := subscriptionToProto(nil); got != nil {
		t.Fatalf("subscriptionToProto(nil) = %v, want nil", got)
	}
}

// subscriptionToProto must carry every populated field onto the wire
// message. A future column added to store.Subscription but not mapped
// here regresses the customer billing surface silently — assert the full
// set survives.
func TestSubscriptionToProto_FieldComplete(t *testing.T) {
	id := uuid.New()
	wsID := uuid.New()
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	created := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	canceled := time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC)

	row := &store.Subscription{
		ID:                   id,
		WorkspaceID:          wsID,
		Tier:                 "GROWTH",
		Status:               "active",
		StripeCustomerID:     "cus_ABC123",
		StripeSubscriptionID: "sub_XYZ789",
		CurrentPeriodStart:   &start,
		CurrentPeriodEnd:     &end,
		CreatedAt:            created,
		CanceledAt:           &canceled,
	}

	got := subscriptionToProto(row)
	if got == nil {
		t.Fatal("subscriptionToProto returned nil for a populated row")
	}
	if got.GetId().GetValue() != id.String() {
		t.Errorf("id = %q, want %q", got.GetId().GetValue(), id.String())
	}
	if got.GetWorkspaceId().GetValue() != wsID.String() {
		t.Errorf("workspace_id = %q, want %q", got.GetWorkspaceId().GetValue(), wsID.String())
	}
	if got.GetTier() != billingv1.SubscriptionTier_SUBSCRIPTION_TIER_GROWTH {
		t.Errorf("tier = %v, want GROWTH", got.GetTier())
	}
	if got.GetStatus() != billingv1.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE {
		t.Errorf("status = %v, want ACTIVE", got.GetStatus())
	}
	if got.GetStripeCustomerId() != "cus_ABC123" {
		t.Errorf("stripe_customer_id = %q, want cus_ABC123", got.GetStripeCustomerId())
	}
	if got.GetStripeSubscriptionId() != "sub_XYZ789" {
		t.Errorf("stripe_subscription_id = %q, want sub_XYZ789", got.GetStripeSubscriptionId())
	}
	if !got.GetCurrentPeriod().GetStart().AsTime().Equal(start) {
		t.Errorf("current_period.start = %v, want %v", got.GetCurrentPeriod().GetStart().AsTime(), start)
	}
	if !got.GetCurrentPeriod().GetEnd().AsTime().Equal(end) {
		t.Errorf("current_period.end = %v, want %v", got.GetCurrentPeriod().GetEnd().AsTime(), end)
	}
	if !got.GetCreatedAt().AsTime().Equal(created) {
		t.Errorf("created_at = %v, want %v", got.GetCreatedAt().AsTime(), created)
	}
	if !got.GetCanceledAt().AsTime().Equal(canceled) {
		t.Errorf("canceled_at = %v, want %v", got.GetCanceledAt().AsTime(), canceled)
	}
}

// A row with no period bounds / no canceled_at must leave those proto
// fields absent (nil), not stamp a zero-time sentinel the web would
// misrender as 1970.
func TestSubscriptionToProto_NullablesAbsent(t *testing.T) {
	row := &store.Subscription{
		ID:          uuid.New(),
		WorkspaceID: uuid.New(),
		Tier:        "PAYG",
		Status:      "active",
		CreatedAt:   time.Now().UTC(),
	}
	got := subscriptionToProto(row)
	if got.GetCurrentPeriod() != nil {
		t.Errorf("current_period = %v, want nil (no bounds set)", got.GetCurrentPeriod())
	}
	if got.GetCanceledAt() != nil {
		t.Errorf("canceled_at = %v, want nil (not canceled)", got.GetCanceledAt())
	}
}

// The input guard runs before the store is touched, so a nil store is
// fine here: a malformed workspace_id is InvalidArgument, never the old
// Unimplemented.
func TestGetSubscription_BadWorkspaceID(t *testing.T) {
	h := NewSubscriptionHandler(nil, nil)
	_, err := h.GetSubscription(context.Background(), connect.NewRequest(&billingv1.GetSubscriptionRequest{
		WorkspaceId: &commonv1.UUID{Value: "not-a-uuid"},
	}))
	if got := connect.CodeOf(err); got != connect.CodeInvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument (err=%v)", got, err)
	}
}

func TestGetSubscription_MissingWorkspaceID(t *testing.T) {
	h := NewSubscriptionHandler(nil, nil)
	_, err := h.GetSubscription(context.Background(), connect.NewRequest(&billingv1.GetSubscriptionRequest{}))
	if got := connect.CodeOf(err); got != connect.CodeInvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument (err=%v)", got, err)
	}
}

// Guard the wire contract gateway-bff's GetVPNAccount depends on: the
// populated proto serialises to proto3-JSON camelCase keys (the BFF
// reads sub.Subscription.Tier/.Status; the web's VPNAccount shape is
// derived BFF-side, but any future direct protojson stream — like the
// EarningsService e2e — must carry camelCase, the #630/#801 class).
func TestSubscriptionProtoJSON_CamelCase(t *testing.T) {
	row := &store.Subscription{
		ID:                   uuid.New(),
		WorkspaceID:          uuid.New(),
		Tier:                 "STARTER",
		Status:               "active",
		StripeCustomerID:     "cus_1",
		StripeSubscriptionID: "sub_1",
		CreatedAt:            time.Now().UTC(),
	}
	resp := &billingv1.GetSubscriptionResponse{Subscription: subscriptionToProto(row)}
	raw, err := protojson.Marshal(resp)
	if err != nil {
		t.Fatalf("protojson.Marshal: %v", err)
	}
	s := string(raw)
	for _, key := range []string{"stripeCustomerId", "stripeSubscriptionId"} {
		if !strings.Contains(s, key) {
			t.Errorf("protojson output missing camelCase key %q: %s", key, s)
		}
	}
}
