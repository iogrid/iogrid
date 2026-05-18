package stripeapi

import (
	"context"
	"testing"

	"github.com/stripe/stripe-go/v79"

	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/config"
)

// fakeBackend implements Backend without any HTTP traffic.
type fakeBackend struct {
	customers map[string]*stripe.Customer
	sessions  []*stripe.CheckoutSession
	portals   []*stripe.BillingPortalSession
	accounts  map[string]*stripe.Account
	links     []*stripe.AccountLink
	payouts   []*stripe.Payout

	checkoutErr error
	customerErr error
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{
		customers: map[string]*stripe.Customer{},
		accounts:  map[string]*stripe.Account{},
	}
}

func (f *fakeBackend) NewCheckoutSession(_ context.Context, p *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
	if f.checkoutErr != nil {
		return nil, f.checkoutErr
	}
	sess := &stripe.CheckoutSession{ID: "cs_test_123", URL: "https://stripe.test/checkout/cs_test_123"}
	if p.Metadata != nil {
		sess.Metadata = p.Metadata
	}
	f.sessions = append(f.sessions, sess)
	return sess, nil
}

func (f *fakeBackend) NewBillingPortalSession(_ context.Context, p *stripe.BillingPortalSessionParams) (*stripe.BillingPortalSession, error) {
	sess := &stripe.BillingPortalSession{ID: "bps_test", URL: "https://stripe.test/portal/bps_test", Customer: *p.Customer}
	f.portals = append(f.portals, sess)
	return sess, nil
}

func (f *fakeBackend) NewCustomer(_ context.Context, p *stripe.CustomerParams) (*stripe.Customer, error) {
	if f.customerErr != nil {
		return nil, f.customerErr
	}
	c := &stripe.Customer{ID: "cus_test_" + randID(), Metadata: p.Metadata}
	f.customers[c.ID] = c
	return c, nil
}

func (f *fakeBackend) NewConnectAccount(_ context.Context, p *stripe.AccountParams) (*stripe.Account, error) {
	a := &stripe.Account{ID: "acct_test_" + randID(), Metadata: p.Metadata}
	f.accounts[a.ID] = a
	return a, nil
}

func (f *fakeBackend) NewAccountLink(_ context.Context, p *stripe.AccountLinkParams) (*stripe.AccountLink, error) {
	l := &stripe.AccountLink{URL: "https://stripe.test/connect/" + *p.Account}
	f.links = append(f.links, l)
	return l, nil
}

func (f *fakeBackend) GetConnectAccount(_ context.Context, id string) (*stripe.Account, error) {
	if a, ok := f.accounts[id]; ok {
		return a, nil
	}
	return &stripe.Account{ID: id}, nil
}

func (f *fakeBackend) NewPayout(_ context.Context, p *stripe.PayoutParams) (*stripe.Payout, error) {
	po := &stripe.Payout{ID: "po_test_" + randID(), Status: stripe.PayoutStatusPending}
	if p.Amount != nil {
		po.Amount = *p.Amount
	}
	f.payouts = append(f.payouts, po)
	return po, nil
}

var counter int

func randID() string {
	counter++
	return string(rune('a' + counter%26))
}

// ── tests ──────────────────────────────────────────────────────────

func TestServiceCreateCheckoutSession_StripeDisabled(t *testing.T) {
	cfg := &config.Config{}
	svc := NewWithBackend(cfg, nil, newFakeBackend())
	_, err := svc.CreateCheckoutSession(context.Background(), uuidNew(t), "STARTER", "https://a", "https://b")
	if !IsStripeDisabled(err) {
		t.Fatalf("expected disabled err, got %v", err)
	}
}

func TestServiceCreateCheckoutSession_MissingPriceID(t *testing.T) {
	cfg := &config.Config{
		StripeSecretKey: "sk_test",
		StripePriceIDs:  map[string]string{},
	}
	be := newFakeBackend()
	svc := NewWithBackend(cfg, nil, be)
	_, err := svc.CreateCheckoutSession(context.Background(), uuidNew(t), "STARTER", "https://a", "https://b")
	if err == nil {
		t.Fatalf("expected error about missing price id")
	}
}

// TestMetadataUUID exercises the helper that validates webhook envelopes.
func TestMetadataUUID(t *testing.T) {
	_, err := metadataUUID(nil, "workspace_id")
	if err == nil {
		t.Errorf("expected error on nil metadata")
	}
	_, err = metadataUUID(map[string]string{"workspace_id": ""}, "workspace_id")
	if err == nil {
		t.Errorf("expected error on empty value")
	}
	id := uuidNew(t).String()
	v, err := metadataUUID(map[string]string{"workspace_id": id}, "workspace_id")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if v.String() != id {
		t.Errorf("uuid mismatch")
	}
}

func TestMapConnectStatus(t *testing.T) {
	cases := []struct {
		name string
		in   *stripe.Account
		want string
	}{
		{"nil", nil, "PENDING_INFO"},
		{"disabled", &stripe.Account{Requirements: &stripe.AccountRequirements{DisabledReason: "denied"}}, "RESTRICTED"},
		{"active", &stripe.Account{PayoutsEnabled: true}, "ACTIVE"},
		{"pending", &stripe.Account{PayoutsEnabled: false}, "PENDING_INFO"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mapConnectStatus(c.in); got != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		})
	}
}
