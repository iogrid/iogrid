// Package store is the persistence layer for billing-svc. It speaks SQL
// to the service's private Postgres database.
//
// Domain rows are returned as plain Go structs so business logic doesn't
// import pgx types. Every write uses parameterised SQL; every read uses
// a context-scoped query.
package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a single-row lookup yields no rows.
var ErrNotFound = errors.New("store: not found")

// Migrations is embedded so the binary is self-contained — services are
// allowed to run goose-up at startup against their own database.
//
//go:embed migrations/*.sql
var Migrations embed.FS

// Store wraps a pgx pool. Construct via New.
type Store struct {
	pool *pgxpool.Pool
}

// New returns a Store backed by the given pgx pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Pool exposes the underlying pgx pool — used by ping probes and tests.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// ── Subscription rows ───────────────────────────────────────────────

// Subscription mirrors the proto Subscription message in row form.
type Subscription struct {
	ID                   uuid.UUID
	WorkspaceID          uuid.UUID
	Tier                 string
	Status               string
	StripeCustomerID     string
	StripeSubscriptionID string
	CurrentPeriodStart   *time.Time
	CurrentPeriodEnd     *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
	CanceledAt           *time.Time
}

// UpsertSubscription writes a Subscription row keyed on stripe_subscription_id.
// Called from Stripe webhooks AND from checkout-completion handlers.
func (s *Store) UpsertSubscription(ctx context.Context, sub Subscription) error {
	const q = `
INSERT INTO subscription (
    id, workspace_id, tier, status,
    stripe_customer_id, stripe_subscription_id,
    current_period_start, current_period_end,
    created_at, updated_at, canceled_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT (stripe_subscription_id) DO UPDATE SET
    tier                 = EXCLUDED.tier,
    status               = EXCLUDED.status,
    current_period_start = EXCLUDED.current_period_start,
    current_period_end   = EXCLUDED.current_period_end,
    updated_at           = now(),
    canceled_at          = EXCLUDED.canceled_at`
	if sub.ID == uuid.Nil {
		sub.ID = uuid.New()
	}
	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = time.Now().UTC()
	}
	sub.UpdatedAt = time.Now().UTC()
	_, err := s.pool.Exec(ctx, q,
		sub.ID, sub.WorkspaceID, sub.Tier, sub.Status,
		sub.StripeCustomerID, sub.StripeSubscriptionID,
		sub.CurrentPeriodStart, sub.CurrentPeriodEnd,
		sub.CreatedAt, sub.UpdatedAt, sub.CanceledAt,
	)
	return err
}

// GetSubscriptionByWorkspace returns the most recent subscription for a
// workspace (a workspace can recycle subscriptions across plan changes).
func (s *Store) GetSubscriptionByWorkspace(ctx context.Context, workspaceID uuid.UUID) (*Subscription, error) {
	const q = `
SELECT id, workspace_id, tier, status,
       stripe_customer_id, stripe_subscription_id,
       current_period_start, current_period_end,
       created_at, updated_at, canceled_at
  FROM subscription
 WHERE workspace_id = $1
 ORDER BY created_at DESC
 LIMIT 1`
	var sub Subscription
	err := s.pool.QueryRow(ctx, q, workspaceID).Scan(
		&sub.ID, &sub.WorkspaceID, &sub.Tier, &sub.Status,
		&sub.StripeCustomerID, &sub.StripeSubscriptionID,
		&sub.CurrentPeriodStart, &sub.CurrentPeriodEnd,
		&sub.CreatedAt, &sub.UpdatedAt, &sub.CanceledAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

// ── Invoice rows ────────────────────────────────────────────────────

// Invoice mirrors the proto Invoice message.
type Invoice struct {
	ID               uuid.UUID
	WorkspaceID      uuid.UUID
	StripeInvoiceID  string
	PeriodStart      time.Time
	PeriodEnd        time.Time
	SubtotalCents    int64
	TaxCents         int64
	TotalCents       int64
	Currency         string
	Status           string
	HostedInvoiceURL *string
	IssuedAt         *time.Time
	PaidAt           *time.Time
}

// UpsertInvoice writes/refreshes an invoice row, keyed by Stripe invoice id.
func (s *Store) UpsertInvoice(ctx context.Context, inv Invoice) error {
	const q = `
INSERT INTO invoice (
    id, workspace_id, stripe_invoice_id,
    period_start, period_end,
    subtotal_cents, tax_cents, total_cents, currency,
    status, hosted_invoice_url, issued_at, paid_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
ON CONFLICT (stripe_invoice_id) DO UPDATE SET
    status              = EXCLUDED.status,
    hosted_invoice_url  = EXCLUDED.hosted_invoice_url,
    paid_at             = EXCLUDED.paid_at`
	if inv.ID == uuid.Nil {
		inv.ID = uuid.New()
	}
	_, err := s.pool.Exec(ctx, q,
		inv.ID, inv.WorkspaceID, inv.StripeInvoiceID,
		inv.PeriodStart, inv.PeriodEnd,
		inv.SubtotalCents, inv.TaxCents, inv.TotalCents, inv.Currency,
		inv.Status, inv.HostedInvoiceURL, inv.IssuedAt, inv.PaidAt,
	)
	return err
}

// ListInvoicesByWorkspace paginates by issued_at DESC.
func (s *Store) ListInvoicesByWorkspace(ctx context.Context, workspaceID uuid.UUID, limit, offset int) ([]Invoice, error) {
	const q = `
SELECT id, workspace_id, stripe_invoice_id,
       period_start, period_end,
       subtotal_cents, tax_cents, total_cents, currency,
       status, hosted_invoice_url, issued_at, paid_at
  FROM invoice
 WHERE workspace_id = $1
 ORDER BY COALESCE(issued_at, period_start) DESC
 LIMIT $2 OFFSET $3`
	rows, err := s.pool.Query(ctx, q, workspaceID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invoice
	for rows.Next() {
		var inv Invoice
		if err := rows.Scan(
			&inv.ID, &inv.WorkspaceID, &inv.StripeInvoiceID,
			&inv.PeriodStart, &inv.PeriodEnd,
			&inv.SubtotalCents, &inv.TaxCents, &inv.TotalCents, &inv.Currency,
			&inv.Status, &inv.HostedInvoiceURL, &inv.IssuedAt, &inv.PaidAt,
		); err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// ── PayoutAccount rows ──────────────────────────────────────────────

// PayoutAccount mirrors the proto PayoutAccount.
type PayoutAccount struct {
	ID                     uuid.UUID
	UserID                 uuid.UUID
	StripeConnectAccountID string
	Status                 string
	CountryCode            *string
	DefaultCurrency        *string
	OnboardedAt            *time.Time
	LastPayoutAt           *time.Time
	CreatedAt              time.Time
}

// UpsertPayoutAccount creates or refreshes by user_id.
func (s *Store) UpsertPayoutAccount(ctx context.Context, pa PayoutAccount) error {
	const q = `
INSERT INTO payout_account (
    id, user_id, stripe_connect_account_id, status,
    country_code, default_currency, onboarded_at, last_payout_at, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (user_id) DO UPDATE SET
    stripe_connect_account_id = EXCLUDED.stripe_connect_account_id,
    status                    = EXCLUDED.status,
    country_code              = COALESCE(EXCLUDED.country_code, payout_account.country_code),
    default_currency          = COALESCE(EXCLUDED.default_currency, payout_account.default_currency),
    onboarded_at              = COALESCE(EXCLUDED.onboarded_at, payout_account.onboarded_at),
    last_payout_at            = COALESCE(EXCLUDED.last_payout_at, payout_account.last_payout_at)`
	if pa.ID == uuid.Nil {
		pa.ID = uuid.New()
	}
	if pa.CreatedAt.IsZero() {
		pa.CreatedAt = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx, q,
		pa.ID, pa.UserID, pa.StripeConnectAccountID, pa.Status,
		pa.CountryCode, pa.DefaultCurrency, pa.OnboardedAt, pa.LastPayoutAt, pa.CreatedAt,
	)
	return err
}

// GetPayoutAccountByUser returns the payout account for a user (one-per-user).
func (s *Store) GetPayoutAccountByUser(ctx context.Context, userID uuid.UUID) (*PayoutAccount, error) {
	const q = `
SELECT id, user_id, stripe_connect_account_id, status,
       country_code, default_currency, onboarded_at, last_payout_at, created_at
  FROM payout_account
 WHERE user_id = $1`
	var pa PayoutAccount
	err := s.pool.QueryRow(ctx, q, userID).Scan(
		&pa.ID, &pa.UserID, &pa.StripeConnectAccountID, &pa.Status,
		&pa.CountryCode, &pa.DefaultCurrency, &pa.OnboardedAt, &pa.LastPayoutAt, &pa.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &pa, nil
}

// ── Payout rows ─────────────────────────────────────────────────────

// Payout mirrors the proto Payout.
type Payout struct {
	ID              uuid.UUID
	UserID          uuid.UUID
	PayoutAccountID uuid.UUID
	AmountCents     int64
	Currency        string
	Status          string
	StripePayoutID  *string
	PeriodStart     time.Time
	PeriodEnd       time.Time
	InitiatedAt     time.Time
	SettledAt       *time.Time
	FailureReason   *string
}

// InsertPayout writes a fresh payout row.
func (s *Store) InsertPayout(ctx context.Context, p Payout) error {
	const q = `
INSERT INTO payout (
    id, user_id, payout_account_id, amount_cents, currency, status,
    stripe_payout_id, period_start, period_end, initiated_at, settled_at, failure_reason
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	if p.InitiatedAt.IsZero() {
		p.InitiatedAt = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx, q,
		p.ID, p.UserID, p.PayoutAccountID, p.AmountCents, p.Currency, p.Status,
		p.StripePayoutID, p.PeriodStart, p.PeriodEnd, p.InitiatedAt, p.SettledAt, p.FailureReason,
	)
	return err
}

// ListPayoutsByUser returns payouts in the given window, newest first.
func (s *Store) ListPayoutsByUser(ctx context.Context, userID uuid.UUID, start, end time.Time, limit, offset int) ([]Payout, error) {
	const q = `
SELECT id, user_id, payout_account_id, amount_cents, currency, status,
       stripe_payout_id, period_start, period_end, initiated_at, settled_at, failure_reason
  FROM payout
 WHERE user_id = $1 AND period_end >= $2 AND period_start <= $3
 ORDER BY initiated_at DESC
 LIMIT $4 OFFSET $5`
	rows, err := s.pool.Query(ctx, q, userID, start, end, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Payout
	for rows.Next() {
		var p Payout
		if err := rows.Scan(
			&p.ID, &p.UserID, &p.PayoutAccountID, &p.AmountCents, &p.Currency, &p.Status,
			&p.StripePayoutID, &p.PeriodStart, &p.PeriodEnd, &p.InitiatedAt, &p.SettledAt, &p.FailureReason,
		); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ── Usage / Metering rows ───────────────────────────────────────────

// UsageEvent mirrors a single metered workload completion.
type UsageEvent struct {
	ID           uuid.UUID
	WorkspaceID  uuid.UUID
	ProviderID   *uuid.UUID
	WorkloadID   uuid.UUID
	WorkloadType string
	Quantity     int64
	CostCents    int64
	Currency     string
	RecordedAt   time.Time
}

// RecordUsageEvent inserts a UsageEvent row. The (workload_id) UNIQUE
// constraint dedupes at-least-once NATS delivery.
func (s *Store) RecordUsageEvent(ctx context.Context, e UsageEvent) error {
	const q = `
INSERT INTO usage_event (
    id, workspace_id, provider_id, workload_id, workload_type,
    quantity, cost_cents, currency, recorded_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (workload_id) DO NOTHING`
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.Currency == "" {
		e.Currency = "USD"
	}
	_, err := s.pool.Exec(ctx, q,
		e.ID, e.WorkspaceID, e.ProviderID, e.WorkloadID, e.WorkloadType,
		e.Quantity, e.CostCents, e.Currency, e.RecordedAt,
	)
	return err
}

// RollupDay computes and upserts daily aggregates for one calendar day
// (UTC). Idempotent — called by cron at 00:05 and also on-demand.
func (s *Store) RollupDay(ctx context.Context, day time.Time) (int64, error) {
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	dayEnd := dayStart.AddDate(0, 0, 1)

	const q = `
INSERT INTO usage_aggregate_daily (
    day, workspace_id, provider_id, workload_type,
    quantity, cost_cents, currency, rolled_up_at
)
SELECT
    $1::date AS day,
    workspace_id,
    provider_id,
    workload_type,
    SUM(quantity)::bigint   AS quantity,
    SUM(cost_cents)::bigint AS cost_cents,
    currency,
    now() AS rolled_up_at
  FROM usage_event
 WHERE recorded_at >= $2 AND recorded_at < $3
 GROUP BY workspace_id, provider_id, workload_type, currency
ON CONFLICT (day, workspace_id, provider_id, workload_type) DO UPDATE SET
    quantity     = EXCLUDED.quantity,
    cost_cents   = EXCLUDED.cost_cents,
    rolled_up_at = now()`
	tag, err := s.pool.Exec(ctx, q, dayStart, dayStart, dayEnd)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ProviderTotal returns total cost (cents) earned by a provider in window.
func (s *Store) ProviderTotal(ctx context.Context, providerID uuid.UUID, start, end time.Time) (int64, string, error) {
	const q = `
SELECT COALESCE(SUM(cost_cents), 0)::bigint AS total, COALESCE(MAX(currency), 'USD') AS currency
  FROM usage_event
 WHERE provider_id = $1 AND recorded_at >= $2 AND recorded_at < $3`
	var total int64
	var currency string
	err := s.pool.QueryRow(ctx, q, providerID, start, end).Scan(&total, &currency)
	if err != nil {
		return 0, "", err
	}
	return total, currency, nil
}

// AllProviderTotalsInWindow returns (provider_id, sum) pairs for all
// providers that earned anything in the window. Used by the daily
// payout/swap loop.
func (s *Store) AllProviderTotalsInWindow(ctx context.Context, start, end time.Time) (map[uuid.UUID]int64, int64, error) {
	const q = `
SELECT provider_id, SUM(cost_cents)::bigint
  FROM usage_event
 WHERE provider_id IS NOT NULL
   AND recorded_at >= $1 AND recorded_at < $2
 GROUP BY provider_id`
	rows, err := s.pool.Query(ctx, q, start, end)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := map[uuid.UUID]int64{}
	var grand int64
	for rows.Next() {
		var pid uuid.UUID
		var sum int64
		if err := rows.Scan(&pid, &sum); err != nil {
			return nil, 0, err
		}
		out[pid] = sum
		grand += sum
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, grand, nil
}

// ── Solana payout + burn rows ───────────────────────────────────────

// SolanaPayout records a $GRID distribution to a provider wallet.
type SolanaPayout struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	WalletAddress  string
	AmountLamports int64
	USDValueCents  int64
	TxSignature    *string
	Status         string
	PeriodStart    time.Time
	PeriodEnd      time.Time
	CreatedAt      time.Time
	SettledAt      *time.Time
}

// InsertSolanaPayout writes a $GRID payout row.
func (s *Store) InsertSolanaPayout(ctx context.Context, p SolanaPayout) error {
	const q = `
INSERT INTO solana_payout (
    id, user_id, wallet_address, amount_lamports, usd_value_cents,
    tx_signature, status, period_start, period_end, created_at, settled_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx, q,
		p.ID, p.UserID, p.WalletAddress, p.AmountLamports, p.USDValueCents,
		p.TxSignature, p.Status, p.PeriodStart, p.PeriodEnd, p.CreatedAt, p.SettledAt,
	)
	return err
}

// SolanaBurn records a daily 2% buyback-and-burn.
type SolanaBurn struct {
	ID             uuid.UUID
	PeriodStart    time.Time
	PeriodEnd      time.Time
	USDValueCents  int64
	AmountLamports int64
	TxSignature    *string
	Status         string
	CreatedAt      time.Time
}

// InsertSolanaBurn writes a burn record.
func (s *Store) InsertSolanaBurn(ctx context.Context, b SolanaBurn) error {
	const q = `
INSERT INTO solana_burn (
    id, period_start, period_end, usd_value_cents, amount_lamports,
    tx_signature, status, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	if b.CreatedAt.IsZero() {
		b.CreatedAt = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx, q,
		b.ID, b.PeriodStart, b.PeriodEnd, b.USDValueCents, b.AmountLamports,
		b.TxSignature, b.Status, b.CreatedAt,
	)
	return err
}

// ── Tax report rows ─────────────────────────────────────────────────

// TaxReport mirrors the tax_report row.
type TaxReport struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	TaxYear    int
	Quarter    int
	FormType   string
	CashCents  int64
	TokenCents int64
	PDFBytes   []byte
	IssuedAt   time.Time
}

// UpsertTaxReport stores a generated quarterly report PDF.
func (s *Store) UpsertTaxReport(ctx context.Context, r TaxReport) error {
	const q = `
INSERT INTO tax_report (
    id, user_id, tax_year, quarter, form_type,
    cash_cents, token_cents, pdf_bytes, issued_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (user_id, tax_year, quarter, form_type) DO UPDATE SET
    cash_cents  = EXCLUDED.cash_cents,
    token_cents = EXCLUDED.token_cents,
    pdf_bytes   = EXCLUDED.pdf_bytes,
    issued_at   = now()`
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	if r.IssuedAt.IsZero() {
		r.IssuedAt = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx, q,
		r.ID, r.UserID, r.TaxYear, r.Quarter, r.FormType,
		r.CashCents, r.TokenCents, r.PDFBytes, r.IssuedAt,
	)
	return err
}

// GuardClause prevents accidental use of an unconfigured store.
func GuardClause(s *Store) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("store not initialised")
	}
	return nil
}
