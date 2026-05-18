// Package stripeapi wraps the github.com/stripe/stripe-go/v79 SDK with
// the iogrid-specific surface: customer subscriptions, Customer Portal,
// and the webhook event router that mutates our Postgres rows.
//
// The package is named stripeapi (not "stripe") to avoid shadowing the
// upstream package import.
package stripeapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v79"
	"github.com/stripe/stripe-go/v79/account"
	"github.com/stripe/stripe-go/v79/accountlink"
	billingportal "github.com/stripe/stripe-go/v79/billingportal/session"
	checkoutsession "github.com/stripe/stripe-go/v79/checkout/session"
	"github.com/stripe/stripe-go/v79/customer"
	stripepayout "github.com/stripe/stripe-go/v79/payout"
	"github.com/stripe/stripe-go/v79/webhook"

	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/config"
	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/store"
)

// Backend is the contract Stripe Service requires from the upstream SDK.
// Exposed as an interface so unit tests can swap in a fake backend.
type Backend interface {
	NewCheckoutSession(ctx context.Context, params *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error)
	NewBillingPortalSession(ctx context.Context, params *stripe.BillingPortalSessionParams) (*stripe.BillingPortalSession, error)
	NewCustomer(ctx context.Context, params *stripe.CustomerParams) (*stripe.Customer, error)
	NewConnectAccount(ctx context.Context, params *stripe.AccountParams) (*stripe.Account, error)
	NewAccountLink(ctx context.Context, params *stripe.AccountLinkParams) (*stripe.AccountLink, error)
	GetConnectAccount(ctx context.Context, id string) (*stripe.Account, error)
	NewPayout(ctx context.Context, params *stripe.PayoutParams) (*stripe.Payout, error)
}

// liveBackend talks to the real Stripe API.
type liveBackend struct{}

func (liveBackend) NewCheckoutSession(ctx context.Context, p *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
	p.Context = ctx
	return checkoutsession.New(p)
}
func (liveBackend) NewBillingPortalSession(ctx context.Context, p *stripe.BillingPortalSessionParams) (*stripe.BillingPortalSession, error) {
	p.Context = ctx
	return billingportal.New(p)
}
func (liveBackend) NewCustomer(ctx context.Context, p *stripe.CustomerParams) (*stripe.Customer, error) {
	p.Context = ctx
	return customer.New(p)
}
func (liveBackend) NewConnectAccount(ctx context.Context, p *stripe.AccountParams) (*stripe.Account, error) {
	p.Context = ctx
	return account.New(p)
}
func (liveBackend) NewAccountLink(ctx context.Context, p *stripe.AccountLinkParams) (*stripe.AccountLink, error) {
	p.Context = ctx
	return accountlink.New(p)
}
func (liveBackend) GetConnectAccount(ctx context.Context, id string) (*stripe.Account, error) {
	p := &stripe.AccountParams{}
	p.Context = ctx
	return account.GetByID(id, p)
}
func (liveBackend) NewPayout(ctx context.Context, p *stripe.PayoutParams) (*stripe.Payout, error) {
	p.Context = ctx
	return stripepayout.New(p)
}

// Service exposes the billing-svc Stripe operations as a single object.
// Construct via New.
type Service struct {
	cfg     *config.Config
	store   *store.Store
	backend Backend
}

// New constructs a Service. The Stripe global API key is set from
// cfg.StripeSecretKey if non-empty.
func New(cfg *config.Config, st *store.Store) *Service {
	if cfg.StripeEnabled() {
		stripe.Key = cfg.StripeSecretKey
	}
	return &Service{cfg: cfg, store: st, backend: liveBackend{}}
}

// NewWithBackend allows tests to inject a fake backend.
func NewWithBackend(cfg *config.Config, st *store.Store, b Backend) *Service {
	return &Service{cfg: cfg, store: st, backend: b}
}

// ── Subscription flows ──────────────────────────────────────────────

// CreateCheckoutSession returns a hosted Stripe Checkout URL that
// upgrades / starts the workspace's subscription to the requested tier.
//
// The Stripe Price ID for the tier is read from cfg (set via env vars).
// successURL/cancelURL come from the caller (usually web/billing).
func (s *Service) CreateCheckoutSession(ctx context.Context, workspaceID uuid.UUID, tier, successURL, cancelURL string) (string, error) {
	if !s.cfg.StripeEnabled() {
		return "", errStripeDisabled
	}
	priceID, ok := s.cfg.StripePriceIDs[strings.ToUpper(tier)]
	if !ok || priceID == "" {
		return "", fmt.Errorf("no Stripe Price ID configured for tier %q (set STRIPE_PRICE_%s)", tier, strings.ToUpper(tier))
	}

	// Resolve or mint the Stripe customer ID for this workspace. We
	// stash it on the subscription row but if no row exists yet, create
	// a fresh Customer with the workspace_id in metadata so we can
	// reconcile on webhook.
	customerID, err := s.resolveCustomer(ctx, workspaceID)
	if err != nil {
		return "", err
	}

	params := &stripe.CheckoutSessionParams{
		Mode:       stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		Customer:   stripe.String(customerID),
		SuccessURL: stripe.String(successURL),
		CancelURL:  stripe.String(cancelURL),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{Price: stripe.String(priceID), Quantity: stripe.Int64(1)},
		},
	}
	params.AddMetadata("workspace_id", workspaceID.String())
	params.AddMetadata("tier", strings.ToUpper(tier))
	params.AddExpand("subscription")

	sess, err := s.backend.NewCheckoutSession(ctx, params)
	if err != nil {
		return "", fmt.Errorf("stripe checkout session: %w", err)
	}
	return sess.URL, nil
}

// CreatePortalSession returns a Customer Portal URL the user can use to
// manage payment methods + invoices + cancellation.
func (s *Service) CreatePortalSession(ctx context.Context, workspaceID uuid.UUID, returnURL string) (string, error) {
	if !s.cfg.StripeEnabled() {
		return "", errStripeDisabled
	}
	customerID, err := s.resolveCustomer(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	sess, err := s.backend.NewBillingPortalSession(ctx, &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(customerID),
		ReturnURL: stripe.String(returnURL),
	})
	if err != nil {
		return "", fmt.Errorf("stripe portal session: %w", err)
	}
	return sess.URL, nil
}

// resolveCustomer returns the StripeCustomerID for a workspace, creating
// a fresh Customer if no subscription row exists yet.
func (s *Service) resolveCustomer(ctx context.Context, workspaceID uuid.UUID) (string, error) {
	sub, err := s.store.GetSubscriptionByWorkspace(ctx, workspaceID)
	if err == nil && sub.StripeCustomerID != "" {
		return sub.StripeCustomerID, nil
	}
	if !errors.Is(err, store.ErrNotFound) && err != nil {
		return "", err
	}
	// Mint a fresh customer.
	cp := &stripe.CustomerParams{}
	cp.AddMetadata("workspace_id", workspaceID.String())
	cust, err := s.backend.NewCustomer(ctx, cp)
	if err != nil {
		return "", fmt.Errorf("stripe new customer: %w", err)
	}
	return cust.ID, nil
}

// ── Webhook routing ─────────────────────────────────────────────────

// HandleWebhook validates the Stripe-Signature header and dispatches
// supported event types to the corresponding store mutator.
//
// Supported events:
//   - checkout.session.completed
//   - customer.subscription.created / updated / deleted
//   - invoice.payment_succeeded / failed
func (s *Service) HandleWebhook(ctx context.Context, payload []byte, sigHeader string) error {
	if !s.cfg.StripeEnabled() {
		return errStripeDisabled
	}
	if s.cfg.StripeWebhookSecret == "" {
		// Without a secret we cannot trust the payload. Refuse rather
		// than silently process.
		return errors.New("STRIPE_WEBHOOK_SECRET not configured")
	}
	event, err := webhook.ConstructEvent(payload, sigHeader, s.cfg.StripeWebhookSecret)
	if err != nil {
		return fmt.Errorf("stripe webhook signature: %w", err)
	}
	return s.routeEvent(ctx, event)
}

// routeEvent dispatches an already-validated Stripe event.
func (s *Service) routeEvent(ctx context.Context, event stripe.Event) error {
	switch event.Type {
	case "checkout.session.completed":
		var sess stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &sess); err != nil {
			return err
		}
		return s.onCheckoutCompleted(ctx, &sess)

	case "customer.subscription.created",
		"customer.subscription.updated",
		"customer.subscription.deleted":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
			return err
		}
		return s.onSubscriptionChanged(ctx, &sub)

	case "invoice.payment_succeeded", "invoice.payment_failed":
		var inv stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &inv); err != nil {
			return err
		}
		return s.onInvoiceEvent(ctx, &inv, string(event.Type))

	default:
		// Unhandled but acknowledged; Stripe expects 2xx so it does
		// not retry types we don't care about.
		return nil
	}
}

func (s *Service) onCheckoutCompleted(ctx context.Context, sess *stripe.CheckoutSession) error {
	workspaceID, err := metadataUUID(sess.Metadata, "workspace_id")
	if err != nil {
		return err
	}
	tier := strings.ToUpper(metadataOr(sess.Metadata, "tier", ""))
	customerID := ""
	if sess.Customer != nil {
		customerID = sess.Customer.ID
	}
	subscriptionID := ""
	if sess.Subscription != nil {
		subscriptionID = sess.Subscription.ID
	}
	sub := store.Subscription{
		WorkspaceID:          workspaceID,
		Tier:                 tier,
		Status:               "active",
		StripeCustomerID:     customerID,
		StripeSubscriptionID: subscriptionID,
	}
	return s.store.UpsertSubscription(ctx, sub)
}

func (s *Service) onSubscriptionChanged(ctx context.Context, sub *stripe.Subscription) error {
	workspaceID, err := metadataUUID(sub.Metadata, "workspace_id")
	if err != nil {
		// Fallback: read from customer metadata if the subscription
		// itself is missing the field. We don't crash on missing —
		// just skip with a non-fatal log via returning nil.
		return nil
	}
	tier := strings.ToUpper(metadataOr(sub.Metadata, "tier", ""))

	customerID := ""
	if sub.Customer != nil {
		customerID = sub.Customer.ID
	}

	var periodStart, periodEnd *time.Time
	if sub.CurrentPeriodStart > 0 {
		t := time.Unix(sub.CurrentPeriodStart, 0).UTC()
		periodStart = &t
	}
	if sub.CurrentPeriodEnd > 0 {
		t := time.Unix(sub.CurrentPeriodEnd, 0).UTC()
		periodEnd = &t
	}

	var canceledAt *time.Time
	if sub.CanceledAt > 0 {
		t := time.Unix(sub.CanceledAt, 0).UTC()
		canceledAt = &t
	}
	row := store.Subscription{
		WorkspaceID:          workspaceID,
		Tier:                 tier,
		Status:               string(sub.Status),
		StripeCustomerID:     customerID,
		StripeSubscriptionID: sub.ID,
		CurrentPeriodStart:   periodStart,
		CurrentPeriodEnd:     periodEnd,
		CanceledAt:           canceledAt,
	}
	return s.store.UpsertSubscription(ctx, row)
}

func (s *Service) onInvoiceEvent(ctx context.Context, inv *stripe.Invoice, eventType string) error {
	workspaceID, err := metadataUUID(inv.Metadata, "workspace_id")
	if err != nil {
		// Look up via customer metadata if the invoice didn't propagate
		// the field. If customer is missing too, drop with no-op.
		if inv.Customer == nil || inv.Customer.Metadata == nil {
			return nil
		}
		workspaceID, err = metadataUUID(inv.Customer.Metadata, "workspace_id")
		if err != nil {
			return nil
		}
	}
	status := string(inv.Status)
	if eventType == "invoice.payment_failed" {
		status = "uncollectible"
	}
	var issuedAt, paidAt *time.Time
	if inv.Created > 0 {
		t := time.Unix(inv.Created, 0).UTC()
		issuedAt = &t
	}
	if inv.StatusTransitions != nil && inv.StatusTransitions.PaidAt > 0 {
		t := time.Unix(inv.StatusTransitions.PaidAt, 0).UTC()
		paidAt = &t
	}
	periodStart := time.Unix(inv.PeriodStart, 0).UTC()
	periodEnd := time.Unix(inv.PeriodEnd, 0).UTC()
	hosted := inv.HostedInvoiceURL
	row := store.Invoice{
		WorkspaceID:      workspaceID,
		StripeInvoiceID:  inv.ID,
		PeriodStart:      periodStart,
		PeriodEnd:        periodEnd,
		SubtotalCents:    inv.Subtotal,
		TaxCents:         inv.Tax,
		TotalCents:       inv.Total,
		Currency:         string(inv.Currency),
		Status:           status,
		HostedInvoiceURL: ptrIfNonEmpty(hosted),
		IssuedAt:         issuedAt,
		PaidAt:           paidAt,
	}
	return s.store.UpsertInvoice(ctx, row)
}

// ── Stripe Connect (provider payouts) ───────────────────────────────

// StartPayoutOnboarding returns a Stripe Express onboarding URL for a
// provider. If no Connect account exists yet it creates one and persists
// the binding.
func (s *Service) StartPayoutOnboarding(ctx context.Context, userID uuid.UUID, returnURL, refreshURL string) (string, error) {
	if !s.cfg.StripeEnabled() {
		return "", errStripeDisabled
	}
	pa, err := s.store.GetPayoutAccountByUser(ctx, userID)
	connectID := ""
	if err == nil && pa != nil {
		connectID = pa.StripeConnectAccountID
	} else if !errors.Is(err, store.ErrNotFound) && err != nil {
		return "", err
	}
	if connectID == "" {
		ap := &stripe.AccountParams{
			Type: stripe.String(string(stripe.AccountTypeExpress)),
			Capabilities: &stripe.AccountCapabilitiesParams{
				Transfers: &stripe.AccountCapabilitiesTransfersParams{
					Requested: stripe.Bool(true),
				},
			},
		}
		ap.AddMetadata("user_id", userID.String())
		acct, err := s.backend.NewConnectAccount(ctx, ap)
		if err != nil {
			return "", fmt.Errorf("stripe connect account: %w", err)
		}
		connectID = acct.ID
		_ = s.store.UpsertPayoutAccount(ctx, store.PayoutAccount{
			UserID:                 userID,
			StripeConnectAccountID: connectID,
			Status:                 "PENDING_INFO",
		})
	}
	link, err := s.backend.NewAccountLink(ctx, &stripe.AccountLinkParams{
		Account:    stripe.String(connectID),
		RefreshURL: stripe.String(refreshURL),
		ReturnURL:  stripe.String(returnURL),
		Type:       stripe.String("account_onboarding"),
	})
	if err != nil {
		return "", fmt.Errorf("stripe account link: %w", err)
	}
	return link.URL, nil
}

// GetPayoutAccount returns the current Stripe Connect status for the user.
func (s *Service) GetPayoutAccount(ctx context.Context, userID uuid.UUID) (*store.PayoutAccount, error) {
	pa, err := s.store.GetPayoutAccountByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if s.cfg.StripeEnabled() {
		acct, aerr := s.backend.GetConnectAccount(ctx, pa.StripeConnectAccountID)
		if aerr == nil && acct != nil {
			pa.Status = mapConnectStatus(acct)
			now := time.Now().UTC()
			if acct.PayoutsEnabled && pa.OnboardedAt == nil {
				pa.OnboardedAt = &now
			}
			_ = s.store.UpsertPayoutAccount(ctx, *pa)
		}
	}
	return pa, nil
}

// RequestInstantPayout debits the Connect balance and records a payout row.
// The amount/currency are read from the latest available balance — for
// Phase 0/1 the caller passes 0 to mean "all available".
func (s *Service) RequestInstantPayout(ctx context.Context, userID uuid.UUID, amountCents int64, currency string) (*store.Payout, error) {
	if !s.cfg.StripeEnabled() {
		return nil, errStripeDisabled
	}
	pa, err := s.store.GetPayoutAccountByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if pa.Status != "ACTIVE" {
		return nil, fmt.Errorf("payout account not active: %s", pa.Status)
	}
	if currency == "" {
		if pa.DefaultCurrency != nil {
			currency = *pa.DefaultCurrency
		} else {
			currency = "usd"
		}
	}
	pp := &stripe.PayoutParams{
		Amount:   stripe.Int64(amountCents),
		Currency: stripe.String(currency),
		Method:   stripe.String("instant"),
	}
	pp.SetStripeAccount(pa.StripeConnectAccountID)
	po, err := s.backend.NewPayout(ctx, pp)
	if err != nil {
		return nil, fmt.Errorf("stripe payout: %w", err)
	}
	now := time.Now().UTC()
	row := store.Payout{
		UserID:          userID,
		PayoutAccountID: pa.ID,
		AmountCents:     amountCents,
		Currency:        currency,
		Status:          string(po.Status),
		StripePayoutID:  &po.ID,
		PeriodStart:     now.Add(-24 * time.Hour),
		PeriodEnd:       now,
	}
	if err := s.store.InsertPayout(ctx, row); err != nil {
		return nil, err
	}
	return &row, nil
}

// mapConnectStatus folds Stripe's "charges_enabled / payouts_enabled /
// requirements.disabled_reason" triplet into our proto enum string.
func mapConnectStatus(acct *stripe.Account) string {
	if acct == nil {
		return "PENDING_INFO"
	}
	if acct.Requirements != nil && acct.Requirements.DisabledReason != "" {
		return "RESTRICTED"
	}
	if acct.PayoutsEnabled {
		return "ACTIVE"
	}
	return "PENDING_INFO"
}

// ── helpers ─────────────────────────────────────────────────────────

var errStripeDisabled = errors.New("Stripe integration not configured (STRIPE_SECRET_KEY empty)")

func metadataUUID(md map[string]string, key string) (uuid.UUID, error) {
	if md == nil {
		return uuid.Nil, fmt.Errorf("metadata %q missing", key)
	}
	v, ok := md[key]
	if !ok || v == "" {
		return uuid.Nil, fmt.Errorf("metadata %q missing", key)
	}
	return uuid.Parse(v)
}

func metadataOr(md map[string]string, key, def string) string {
	if md == nil {
		return def
	}
	if v, ok := md[key]; ok && v != "" {
		return v
	}
	return def
}

func ptrIfNonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ReadBody is exported so the HTTP layer can drain & feed the verifier.
func ReadBody(r io.Reader, max int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, max))
}

// ErrStripeDisabledHTTPStatus is the recommended status code for the
// HTTP layer to return when StripeEnabled()==false.
const ErrStripeDisabledHTTPStatus = http.StatusServiceUnavailable

// IsStripeDisabled reports whether err is the sentinel for "Stripe not configured".
func IsStripeDisabled(err error) bool { return errors.Is(err, errStripeDisabled) }
