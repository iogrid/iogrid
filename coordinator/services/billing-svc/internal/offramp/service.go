package offramp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/store"
)

// Service binds the offramp.Provider catalogue to the persistence layer.
// Routes call StartOffRamp / HandleWebhook / GetStatus; the service
// hides the registry + store coupling from the HTTP layer.
type Service struct {
	registry *Registry
	store    *store.Store
	logger   *slog.Logger
}

// NewService constructs a Service. registry MUST be non-nil; routes that
// call into a Service with no registered providers receive a typed
// "no providers configured" error.
func NewService(registry *Registry, st *store.Store, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{registry: registry, store: st, logger: logger}
}

// Registry exposes the underlying registry — callers that just need to
// list providers (e.g. the gRPC ListOffRampProviders RPC) read this.
func (s *Service) Registry() *Registry { return s.registry }

// StartOffRampInput is the canonical input to Service.StartOffRamp. The
// gRPC and JSON edge layers translate their wire shapes into this.
type StartOffRampInput struct {
	UserID        uuid.UUID
	ProviderName  string
	WalletAddress string
	GridAmount    uint64
	FiatCurrency  string
	ReturnURL     string
}

// StartOffRampOutput is what the routes layer returns to the browser.
type StartOffRampOutput struct {
	RequestID   uuid.UUID
	RedirectURL string
}

// StartOffRamp resolves the provider, persists a pending row, and
// returns the partner redirect URL.
func (s *Service) StartOffRamp(ctx context.Context, in StartOffRampInput) (*StartOffRampOutput, error) {
	if in.UserID == uuid.Nil {
		return nil, errors.New("offramp: UserID required")
	}
	if in.WalletAddress == "" {
		return nil, errors.New("offramp: WalletAddress required")
	}
	if in.GridAmount == 0 {
		return nil, errors.New("offramp: GridAmount must be > 0")
	}
	provider, err := s.registry.GetProvider(in.ProviderName)
	if err != nil {
		return nil, err
	}

	requestID := uuid.New()
	req := OffRampRequest{
		RequestID:     requestID.String(),
		ProviderID:    in.UserID.String(),
		WalletAddress: in.WalletAddress,
		GridAmount:    in.GridAmount,
		ReturnURL:     in.ReturnURL,
		FiatCurrency:  in.FiatCurrency,
	}
	redirectURL, err := provider.BuildRedirectURL(req)
	if err != nil {
		return nil, fmt.Errorf("offramp: build redirect: %w", err)
	}

	var returnURLPtr *string
	if in.ReturnURL != "" {
		v := in.ReturnURL
		returnURLPtr = &v
	}
	row := store.OffRampRequest{
		ID:            requestID,
		UserID:        in.UserID,
		ProviderName:  provider.Name(),
		WalletAddress: in.WalletAddress,
		// NOTE: store.OffRampRequest.GridAmount is int64 in the schema —
		// uint64 lamports can technically overflow but $GRID supply (1B
		// tokens × 10^9 = 10^18) fits exactly within int64 max so this
		// is safe for the lifetime of the token.
		GridAmount:   int64(in.GridAmount),
		FiatCurrency: nonEmptyOr(in.FiatCurrency, "USD"),
		Status:       StatusPending,
		RedirectURL:  redirectURL,
		ReturnURL:    returnURLPtr,
	}
	if err := s.store.InsertOffRampRequest(ctx, row); err != nil {
		return nil, fmt.Errorf("offramp: persist request: %w", err)
	}
	s.logger.Info("offramp: started",
		slog.String("provider", provider.Name()),
		slog.String("request_id", requestID.String()),
		slog.String("user_id", in.UserID.String()),
		slog.Uint64("grid_lamports", in.GridAmount),
	)
	return &StartOffRampOutput{RequestID: requestID, RedirectURL: redirectURL}, nil
}

// HandleWebhook verifies the partner signature, parses the payload, and
// updates the persisted row. Returns the canonical OffRampStatus so the
// caller can emit a downstream telemetry event.
//
// Errors:
//   - ErrUnknownProvider — provider_name not registered
//   - ErrInvalidSignature — signature failed verification (caller should respond 401)
//   - other — wraps store / decode errors
func (s *Service) HandleWebhook(ctx context.Context, providerName string, payload []byte, signature string) (*OffRampStatus, error) {
	provider, err := s.registry.GetProvider(providerName)
	if err != nil {
		return nil, err
	}
	if !provider.VerifyWebhookSignature(payload, signature) {
		return nil, ErrInvalidSignature
	}
	status, err := provider.ParseWebhook(payload)
	if err != nil {
		return nil, fmt.Errorf("offramp: parse webhook: %w", err)
	}
	id, err := uuid.Parse(status.RequestID)
	if err != nil {
		return nil, fmt.Errorf("offramp: webhook ref %q not a UUID: %w", status.RequestID, err)
	}
	up := store.OffRampStatusUpdate{
		Status: status.Status,
	}
	if status.ProviderRefID != "" {
		v := status.ProviderRefID
		up.ProviderRefID = &v
	}
	if status.FiatAmount != "" {
		v := status.FiatAmount
		up.FiatAmount = &v
	}
	if status.FiatCurrency != "" {
		v := status.FiatCurrency
		up.FiatCurrency = &v
	}
	if status.TxnSignature != "" {
		v := status.TxnSignature
		up.TxnSignature = &v
	}
	if status.CompletedAt != nil {
		up.CompletedAt = status.CompletedAt
	}
	if err := s.store.UpdateOffRampRequestStatus(ctx, id, up); err != nil {
		return nil, fmt.Errorf("offramp: update row: %w", err)
	}
	s.logger.Info("offramp: webhook applied",
		slog.String("provider", providerName),
		slog.String("request_id", status.RequestID),
		slog.String("status", status.Status),
	)
	return status, nil
}

// GetStatus returns the persisted row for a request.
func (s *Service) GetStatus(ctx context.Context, requestID uuid.UUID) (*store.OffRampRequest, error) {
	return s.store.GetOffRampRequest(ctx, requestID)
}

// ListUserRequests returns the user's recent off-ramp attempts, newest
// first. Bounded by limit (default 50).
func (s *Service) ListUserRequests(ctx context.Context, userID uuid.UUID, limit, offset int) ([]store.OffRampRequest, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.store.ListOffRampRequestsByUser(ctx, userID, limit, offset)
}

func nonEmptyOr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
