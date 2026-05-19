// Package solana implements the $GRID payout + burn loops described in
// docs/TOKENOMICS.md.
//
// The package boots in one of two modes:
//
//   - Stub mode (GRID_TOKEN_MINT_ADDRESS or hot-wallet path unset): every
//     entry point logs "SKIP: token launch is post-Phase-1" and persists
//     placeholder rows so dashboards keep functioning. No RPC calls are
//     made.
//
//   - Live mode: the Service holds a Solana RPC client (chain.go), the
//     hot-wallet keypair, a Jupiter HTTP client, and the SPL Token program
//     id ($GRID is minted under Token-2022 by default). On every
//     RunDailySwapAndDistribute the loop:
//
//       1. Aggregates provider revenue from `usage_event` for the window.
//       2. Inserts SolanaPayout rows with status=PENDING.
//       3. Inserts a SolanaBurn row with status=PENDING.
//       4. For each PENDING row: swaps USDC → $GRID via Jupiter, then
//          TransferChecked to the provider's ATA OR BurnChecked from the
//          hot wallet's ATA. Each on-chain step transitions the row via
//          UpdateSolanaPayoutStatus / UpdateSolanaBurnStatus.
//
// Phase 0/1 uses a single hot-wallet keypair. Phase 2 wraps the signer in a
// Squads Protocol multisig — see multisig.go.

package solana

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/blocto/solana-go-sdk/common"
	"github.com/blocto/solana-go-sdk/types"
	"github.com/google/uuid"

	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/config"
	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/store"
)

// USDC mint on Solana mainnet — used as the source side of every Jupiter
// quote for the USD→$GRID swap. Public, well-known.
const USDCMint = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"

// Service is the entry point for both the daily payout/swap loop and
// the burn loop. Construct via New().
type Service struct {
	cfg            *config.Config
	store          *store.Store
	logger         *slog.Logger
	wallet         *types.Account // nil in stub mode
	jupiter        *JupiterClient
	chain          *chainClient   // nil in stub mode
	tokenProgramID common.PublicKey
	now            func() time.Time // injectable for tests
}

// New returns a configured Service. In stub mode wallet/chain are nil and
// jupiter is still wired (the HTTP client itself is cheap).
func New(cfg *config.Config, st *store.Store, logger *slog.Logger) (*Service, error) {
	if logger == nil {
		logger = slog.Default()
	}
	svc := &Service{
		cfg:     cfg,
		store:   st,
		logger:  logger,
		jupiter: NewJupiterClient(cfg.JupiterAPIURL, &http.Client{Timeout: 15 * time.Second}),
		now:     time.Now,
		// Default tokenProgramID even in stub mode so derivations don't
		// panic; the field is only consulted in live flows.
		tokenProgramID: Token2022ProgramID,
	}
	if cfg.SolanaEnabled() {
		w, err := loadKeypair(cfg.SolanaHotWalletPath)
		if err != nil {
			return nil, fmt.Errorf("load hot wallet: %w", err)
		}
		svc.wallet = w
		svc.chain = newChainClient(cfg.SolanaRPCURL, logger)
		svc.tokenProgramID = resolveTokenProgram(cfg.GRIDTokenProgram)
		logger.Info("solana: live mode",
			slog.String("wallet", svc.wallet.PublicKey.ToBase58()),
			slog.String("mint", cfg.GRIDTokenMint),
			slog.String("token_program", svc.tokenProgramID.ToBase58()),
			slog.String("multisig_mode", string(svc.MultisigMode())),
		)
	} else {
		logger.Warn("solana: stub mode (GRID_TOKEN_MINT_ADDRESS or hot-wallet path unset)")
	}
	return svc, nil
}

// Enabled reports whether the live wallet is loaded.
func (s *Service) Enabled() bool { return s.wallet != nil && s.cfg.SolanaEnabled() }

// WalletAddress returns the public key of the hot wallet, or empty in
// stub mode.
func (s *Service) WalletAddress() string {
	if !s.Enabled() {
		return ""
	}
	return s.wallet.PublicKey.ToBase58()
}

// PayoutDistribution carries the per-provider rewards for one day.
type PayoutDistribution struct {
	ProviderID    string  // UUID string
	WalletAddress string  // base58 Solana pubkey
	USDValueCents int64   // proportional share of daily provider revenue
	GRIDLamports  int64   // computed from Jupiter quote at swap time
	Share         float64 // 0..1, this provider's fraction of total
}

// BurnDecision is the outcome of evaluating the daily burn loop.
type BurnDecision struct {
	USDValueCents  int64
	GRIDLamports   int64
	Skipped        bool
	SkipReason     string
	IncineratorAcc string
}

// RunDailySwapAndDistribute is the high-level cron entry point.
//
// Steps:
//
//  1. Look up provider revenue totals for `day` from the store.
//  2. Split into (100 - cfg.BurnPercentage)% provider rewards + cfg.BurnPercentage% burn.
//  3. For each provider compute share, insert SolanaPayout row PENDING,
//     then (live mode) swap + transfer on-chain and update the row.
//  4. Insert a SolanaBurn row, then (live mode) swap + burn on-chain.
//
// In stub mode (Solana disabled) the records are still written with
// status="SKIPPED" so dashboards can show "would have paid out X" even
// before TGE.
func (s *Service) RunDailySwapAndDistribute(ctx context.Context, day time.Time) error {
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	dayEnd := dayStart.AddDate(0, 0, 1)

	totals, grand, err := s.store.AllProviderTotalsInWindow(ctx, dayStart, dayEnd)
	if err != nil {
		return fmt.Errorf("aggregate provider totals: %w", err)
	}
	if grand == 0 {
		s.logger.Info("solana daily loop: no revenue for window",
			slog.Time("start", dayStart))
		return nil
	}

	burnPct := s.cfg.BurnPercentage / 100.0
	burnCents := int64(float64(grand) * burnPct)
	if burnCents < 0 {
		burnCents = 0
	}
	providerPoolCents := grand - burnCents

	// Burn — record first, then execute.
	if err := s.runBurnForWindow(ctx, burnCents, dayStart, dayEnd); err != nil {
		s.logger.Error("solana daily burn failed",
			slog.String("error", err.Error()))
		// Continue — provider payouts are independent.
	}

	// Provider distributions.
	for providerID, providerCents := range totals {
		share := float64(providerCents) / float64(grand)
		providerUSDCents := int64(float64(providerPoolCents) * share)
		dist := PayoutDistribution{
			ProviderID:    providerID.String(),
			USDValueCents: providerUSDCents,
			Share:         share,
		}
		if err := s.distributeOne(ctx, dist, dayStart, dayEnd); err != nil {
			s.logger.Error("solana payout failed (continuing with other providers)",
				slog.String("provider_id", dist.ProviderID),
				slog.String("error", err.Error()))
			continue
		}
	}
	return nil
}

// runBurnForWindow records (or stubs) and, in live mode, executes the
// buyback-and-burn for one window. Returns nil even when the on-chain step
// fails (row is left in FAILED state for cron retry).
func (s *Service) runBurnForWindow(ctx context.Context, usdCents int64, periodStart, periodEnd time.Time) error {
	decision, err := s.evaluateBurn(ctx, usdCents, periodStart, periodEnd)
	if err != nil {
		return fmt.Errorf("evaluate burn: %w", err)
	}
	row := store.SolanaBurn{
		PeriodStart:    periodStart,
		PeriodEnd:      periodEnd,
		USDValueCents:  decision.USDValueCents,
		AmountLamports: decision.GRIDLamports,
		Status:         "PENDING",
	}
	if decision.Skipped {
		row.Status = "SKIPPED"
	}
	row.ID = uuid.New()
	if err := s.store.InsertSolanaBurn(ctx, row); err != nil {
		return fmt.Errorf("insert burn row: %w", err)
	}
	if decision.Skipped || !s.Enabled() {
		return nil
	}
	// Live mode: execute the buyback-and-burn.
	res, execErr := s.ExecuteBurn(ctx, decision.USDValueCents, s.cfg.BurnViaIncinerator)
	if execErr != nil {
		msg := execErr.Error()
		_ = s.store.UpdateSolanaBurnStatus(ctx, row.ID, "FAILED",
			ptrStr(""), ptrStr(""), &msg, nil)
		return execErr
	}
	burnedI64 := int64(res.GRIDBurned) // safe: lamports fit in int64 for any plausible burn
	_ = s.store.UpdateSolanaBurnStatus(ctx, row.ID, "CONFIRMED",
		ptrStr(res.BurnSignature), ptrStr(res.SwapSignature), nil, &burnedI64)
	return nil
}

// evaluateBurn quotes Jupiter for USDC→$GRID at the burn USD amount.
// In stub mode it returns Skipped=true.
func (s *Service) evaluateBurn(ctx context.Context, usdCents int64, _, _ time.Time) (*BurnDecision, error) {
	dec := &BurnDecision{
		USDValueCents:  usdCents,
		IncineratorAcc: s.cfg.IncineratorAddress,
	}
	if !s.Enabled() {
		dec.Skipped = true
		dec.SkipReason = "SKIP: token launch is post-Phase-1"
		s.logger.Info("burn skipped: token mint not configured",
			slog.Int64("usd_cents", usdCents))
		return dec, nil
	}
	if usdCents == 0 {
		return dec, nil
	}
	lamports, err := s.jupiter.QuoteUSDCToGRID(ctx, usdCents, s.cfg.GRIDTokenMint)
	if err != nil {
		return nil, err
	}
	dec.GRIDLamports = lamports
	return dec, nil
}

// distributeOne records (or stubs) a single $GRID payout to a provider.
//
// The provider's Solana wallet binding lives in identity-svc — we don't
// import that service here; instead we read the wallet binding through
// a NATS event in production. For Phase 0/1 the wallet is passed in via
// the metering event itself; if absent we mark status=MISSING_WALLET and
// skip the swap (the cents are still recorded so the report is accurate).
//
// In live mode the on-chain TransferChecked is submitted + confirmed, then
// the row transitions PENDING → SUBMITTED → CONFIRMED. On failure the row
// moves to FAILED with ErrorMessage set.
func (s *Service) distributeOne(ctx context.Context, dist PayoutDistribution, periodStart, periodEnd time.Time) error {
	row := store.SolanaPayout{
		ID:            uuid.New(),
		WalletAddress: dist.WalletAddress,
		USDValueCents: dist.USDValueCents,
		PeriodStart:   periodStart,
		PeriodEnd:     periodEnd,
		Status:        "PENDING",
	}
	if dist.WalletAddress == "" {
		row.Status = "MISSING_WALLET"
	}
	// providerID parses as UUID — caller passed it stringified.
	if id, err := uuid.Parse(dist.ProviderID); err == nil {
		row.UserID = id
	}

	if !s.Enabled() {
		row.Status = "SKIPPED"
		s.logger.Info("solana payout skipped: token mint not configured",
			slog.String("provider_id", dist.ProviderID),
			slog.Int64("usd_cents", dist.USDValueCents))
		return s.store.InsertSolanaPayout(ctx, row)
	}
	if row.Status == "MISSING_WALLET" {
		return s.store.InsertSolanaPayout(ctx, row)
	}

	// Quote — keeps the row consistent even when on-chain submit fails.
	lamports, err := s.jupiter.QuoteUSDCToGRID(ctx, dist.USDValueCents, s.cfg.GRIDTokenMint)
	if err != nil {
		row.Status = "FAILED"
		em := err.Error()
		row.ErrorMessage = &em
		_ = s.store.InsertSolanaPayout(ctx, row)
		return err
	}
	row.AmountLamports = lamports
	if err := s.store.InsertSolanaPayout(ctx, row); err != nil {
		return err
	}

	// Execute: swap USDC→$GRID into the hot wallet's ATA, then transfer
	// to the provider's ATA. Two on-chain steps so we capture each
	// signature separately for audit.
	swap, err := s.ExecuteSwap(ctx, SwapRequest{
		InputMint:   USDCMint,
		OutputMint:  s.cfg.GRIDTokenMint,
		Amount:      uint64(dist.USDValueCents) * 10_000,
		SlippageBps: 50,
	})
	if err != nil {
		msg := err.Error()
		_ = s.store.UpdateSolanaPayoutStatus(ctx, row.ID, "FAILED", nil, nil, &msg, nil)
		return err
	}
	dest := common.PublicKeyFromString(dist.WalletAddress)
	sig, err := s.TransferGRID(ctx, dest, swap.OutAmount)
	if err != nil {
		msg := err.Error()
		_ = s.store.UpdateSolanaPayoutStatus(ctx, row.ID, "FAILED",
			ptrStr(sig), ptrStr(swap.Signature), &msg, nil)
		return err
	}
	out := int64(swap.OutAmount)
	_ = s.store.UpdateSolanaPayoutStatus(ctx, row.ID, "CONFIRMED",
		ptrStr(sig), ptrStr(swap.Signature), nil, &out)
	return nil
}

// RunBurnLoop is the public hook for the burn-only cron (legacy / debug).
// Reuses runBurnForWindow so the on-chain path is identical.
func (s *Service) RunBurnLoop(ctx context.Context, day time.Time) error {
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	dayEnd := dayStart.AddDate(0, 0, 1)

	_, grand, err := s.store.AllProviderTotalsInWindow(ctx, dayStart, dayEnd)
	if err != nil {
		return err
	}
	burnCents := int64(float64(grand) * s.cfg.BurnPercentage / 100.0)
	return s.runBurnForWindow(ctx, burnCents, dayStart, dayEnd)
}

// StartDailyCron starts an in-process daily cron (used when
// DAILY_PAYOUT_ENABLED=true and no external k8s CronJob is wired). It runs
// the previous-day window at startup and then once every 24 hours.
// Cancel via ctx.
func (s *Service) StartDailyCron(ctx context.Context) {
	if s == nil {
		return
	}
	if !s.cfg.DailyPayoutEnabled {
		s.logger.Info("solana: daily cron disabled (DAILY_PAYOUT_ENABLED=false)")
		return
	}
	go func() {
		s.logger.Info("solana: daily cron starting",
			slog.String("schedule", s.cfg.DailyPayoutCron),
			slog.Bool("live", s.Enabled()))
		// Run-on-boot for yesterday's window so a crash-loop doesn't drop
		// payouts. Subsequent runs are exactly 24h apart.
		runForYesterday := func() {
			if err := s.RunDailySwapAndDistribute(ctx, s.now().AddDate(0, 0, -1)); err != nil {
				s.logger.Error("solana daily cron tick failed",
					slog.String("error", err.Error()))
			}
		}
		runForYesterday()
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				s.logger.Info("solana: daily cron stopping")
				return
			case <-ticker.C:
				runForYesterday()
			}
		}
	}()
}

// loadKeypair reads a Solana CLI keypair JSON file (array of 64 ints).
func loadKeypair(path string) (*types.Account, error) {
	if path == "" {
		return nil, errors.New("hot-wallet keypair path empty")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var arr []byte
	if err := json.Unmarshal(b, &arr); err != nil {
		return nil, fmt.Errorf("parse keypair (expected JSON int array): %w", err)
	}
	if len(arr) != 64 {
		return nil, fmt.Errorf("keypair must be 64 bytes, got %d", len(arr))
	}
	acct, err := types.AccountFromBytes(arr)
	if err != nil {
		return nil, err
	}
	return &acct, nil
}

// ptrStr returns &s, or nil if s is empty — saves an inline three-line
// helper at every call site.
func ptrStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ── Jupiter swap client ─────────────────────────────────────────────

// JupiterClient is a thin wrapper over the Jupiter /quote endpoint.
type JupiterClient struct {
	baseURL string
	http    *http.Client
}

// NewJupiterClient constructs a client. baseURL is e.g.
// "https://quote-api.jup.ag/v6".
func NewJupiterClient(baseURL string, hc *http.Client) *JupiterClient {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &JupiterClient{baseURL: baseURL, http: hc}
}

// QuoteResponse is the subset of fields we consume from Jupiter /v6/quote.
type QuoteResponse struct {
	InputMint   string `json:"inputMint"`
	OutputMint  string `json:"outputMint"`
	InAmount    string `json:"inAmount"`
	OutAmount   string `json:"outAmount"`
	OtherAmount string `json:"otherAmountThreshold"`
	PriceImpact string `json:"priceImpactPct"`
	SlippageBps int    `json:"slippageBps"`
}

// QuoteUSDCToGRID returns the $GRID lamport amount Jupiter expects to
// output for an inAmount of USDC equivalent to `usdCents` cents.
//
// USDC has 6 decimals: 1 USDC == 1_000_000 atomic.
// 1 cent == 10_000 atomic.
func (c *JupiterClient) QuoteUSDCToGRID(ctx context.Context, usdCents int64, gridMint string) (int64, error) {
	if usdCents <= 0 {
		return 0, nil
	}
	atomic := usdCents * 10_000
	u, err := url.Parse(c.baseURL + "/quote")
	if err != nil {
		return 0, err
	}
	q := u.Query()
	q.Set("inputMint", USDCMint)
	q.Set("outputMint", gridMint)
	q.Set("amount", strconv.FormatInt(atomic, 10))
	q.Set("slippageBps", "50") // 0.5%
	q.Set("swapMode", "ExactIn")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("jupiter quote: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return 0, fmt.Errorf("jupiter quote: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var qr QuoteResponse
	if err := json.NewDecoder(resp.Body).Decode(&qr); err != nil {
		return 0, fmt.Errorf("decode jupiter quote: %w", err)
	}
	out, err := strconv.ParseInt(qr.OutAmount, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse outAmount %q: %w", qr.OutAmount, err)
	}
	return out, nil
}
