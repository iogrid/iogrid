package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/blocto/solana-go-sdk/common"
	"github.com/go-chi/chi/v5"

	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/grid"
	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/solana"
)

// GridDeps bundles the collaborators used by both /v1/grid/session-end
// (#597) and /v1/devnet/faucet (#595). Wired up in main.go alongside the
// existing server.Deps.
type GridDeps struct {
	Meter *grid.SessionMeter
	// BuildMeter settles iOS-build provider earnings (#700/#707). nil
	// disables /v1/grid/build-end (returns 503).
	BuildMeter *grid.BuildMeter
	Store      *grid.PostgresStore
	Solana     *solana.Service // for the faucet (mint authority)
	Logger     *slog.Logger
	// DevnetMode is true when we're authorised to mint test $GRID via the
	// faucet. Wired from IOGRID_CLUSTER env (= "devnet").
	DevnetMode bool
	// FaucetAmount in atomic units. Defaults to 100 GRID (= 100 * 1e9 atomic).
	FaucetAmount uint64
	// FaucetCooldown is the rate-limit window per wallet (default 1h).
	FaucetCooldown time.Duration
}

const (
	defaultFaucetAmount   uint64        = 100_000_000_000 // 100 GRID
	defaultFaucetCooldown time.Duration = 1 * time.Hour
)

// mountGrid attaches /v1/grid/session-end + /v1/devnet/faucet to the
// router. Called from server.Mount.
func mountGrid(r chi.Router, deps *GridDeps) {
	if deps == nil {
		return
	}
	r.Post("/v1/grid/session-end", deps.handleSessionEnd)
	r.Post("/v1/grid/build-end", deps.handleBuildEnd)
	r.Post("/v1/devnet/faucet", deps.handleFaucet)
	r.Get("/v1/grid/balance", deps.handleBalance)
}

// ── /v1/grid/build-end ──────────────────────────────────────────────
// Provider-earnings settlement for a completed iOS build. build-gateway
// POSTs the (build_id, attempt_id, customer_wallet, provider_*, escrowed,
// consumed) tuple on terminal build status; the BuildMeter writes the
// 85/15 split row that the settlement-worker drains. Idempotent on
// (build_id, attempt_id) so a build-gateway retry can't double-pay.
func (g *GridDeps) handleBuildEnd(w http.ResponseWriter, r *http.Request) {
	if g.BuildMeter == nil {
		writeErr(w, http.StatusServiceUnavailable, "grid build meter disabled")
		return
	}
	var in grid.BuildInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	row, err := g.BuildMeter.Settle(r.Context(), in)
	if err != nil {
		// Zero consumption is a benign no-settlement, not a 500.
		if errors.Is(err, grid.ErrNoBuildConsumption) {
			writeJSON(w, http.StatusOK, map[string]any{"settled": false, "reason": "no consumption"})
			return
		}
		if g.Logger != nil {
			g.Logger.Warn("grid: build settle failed",
				slog.String("build_id", in.BuildID.String()),
				slog.String("attempt_id", in.AttemptID.String()),
				slog.String("error", err.Error()))
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":              row.ID,
		"build_id":        row.BuildID,
		"attempt_id":      row.AttemptID,
		"consumed_atomic": row.ConsumedAtomic,
		"refund_atomic":   row.RefundAtomic,
		"provider_share":  row.ProviderShare,
		"iogrid_share":    row.IogridShare,
	})
}

// ── /v1/grid/balance ─────────────────────────────────────────────────
//
//	GET /v1/grid/balance?wallet=<base58>
//	-> 200 {
//	     wallet,
//	     balance_atomic, balance_grid,           // on-chain $GRID held
//	     grace_overage_owed_atomic,              // arrears to clear next top-up
//	     grace_overage_cap_atomic,               // founder-ruled ceiling
//	     available_atomic                        // balance - owed (may be < 0)
//	   }
//
// Backs the customer prepaid-balance surface (#632). gateway-bff resolves
// the caller's bound wallet and forwards it here; this service owns the
// Solana RPC + the grace-overage arrears query.
//
// Anti-fake-state (#417): when the Solana subsystem is in stub mode (no
// $GRID mint configured yet) we return 503 rather than a misleading zero
// balance — the web surface renders an explicit "balance unavailable"
// banner instead of a fake $0.00 that masks the outage.
func (g *GridDeps) handleBalance(w http.ResponseWriter, r *http.Request) {
	wallet := strings.TrimSpace(r.URL.Query().Get("wallet"))
	if wallet == "" {
		writeErr(w, http.StatusBadRequest, "wallet query param required")
		return
	}
	recipient := common.PublicKeyFromString(wallet)
	if recipient.ToBase58() != wallet {
		writeErr(w, http.StatusBadRequest, "wallet malformed")
		return
	}
	if g.Solana == nil || !g.Solana.Enabled() {
		writeErr(w, http.StatusServiceUnavailable, "balance unavailable: $GRID mint not configured (pre-TGE)")
		return
	}
	if g.Store == nil {
		writeErr(w, http.StatusServiceUnavailable, "grid store unconfigured")
		return
	}
	balAtomic, err := g.Solana.GRIDAtomicWalletBalance(r.Context(), wallet)
	if err != nil {
		if g.Logger != nil {
			g.Logger.Warn("grid: balance read failed",
				slog.String("wallet", wallet), slog.String("error", err.Error()))
		}
		writeErr(w, http.StatusBadGateway, "balance read failed: "+err.Error())
		return
	}
	owed, err := g.Store.SumGraceOverageOwedByCustomer(r.Context(), wallet)
	if err != nil {
		if g.Logger != nil {
			g.Logger.Warn("grid: overage query failed",
				slog.String("wallet", wallet), slog.String("error", err.Error()))
		}
		writeErr(w, http.StatusInternalServerError, "overage lookup failed")
		return
	}
	// available = on-chain balance minus arrears owed. Signed so the web
	// can render a slightly-negative prepaid balance under the grace cap.
	available := int64(balAtomic) - int64(owed)
	writeJSON(w, http.StatusOK, map[string]any{
		"wallet":                    wallet,
		"balance_atomic":            balAtomic,
		"balance_grid":              gridFromAtomic(balAtomic),
		"grace_overage_owed_atomic": owed,
		"grace_overage_cap_atomic":  grid.GraceOverageCapAtomic,
		"available_atomic":          available,
	})
}

// gridFromAtomic renders an atomic (9-decimal) $GRID amount as a decimal
// string with up to 4 fractional digits — enough precision for the UI
// without exposing lamport-level noise.
func gridFromAtomic(atomic uint64) string {
	const decimals = 1_000_000_000 // 1e9, $GRID has 9 decimals
	whole := atomic / decimals
	frac := (atomic % decimals) / 100_000 // keep 4 dp
	return strconv.FormatUint(whole, 10) + "." +
		leftPad(strconv.FormatUint(frac, 10), 4)
}

func leftPad(s string, n int) string {
	for len(s) < n {
		s = "0" + s
	}
	return s
}

// ── /v1/grid/session-end ────────────────────────────────────────────

func (g *GridDeps) handleSessionEnd(w http.ResponseWriter, r *http.Request) {
	if g.Meter == nil {
		writeErr(w, http.StatusServiceUnavailable, "grid meter disabled")
		return
	}
	var in grid.Input
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	row, err := g.Meter.Settle(r.Context(), in)
	if err != nil {
		if g.Logger != nil {
			g.Logger.Warn("grid: settle failed",
				slog.String("session_id", in.SessionID.String()),
				slog.String("error", err.Error()))
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":              row.ID,
		"session_id":      row.SessionID,
		"consumed_atomic": row.ConsumedAtomic,
		"refund_atomic":   row.RefundAtomic,
		"provider_share":  row.ProviderShare,
		"iogrid_share":    row.IogridShare,
	})
}

// ── /v1/devnet/faucet ────────────────────────────────────────────────

type faucetReq struct {
	WalletAddress string `json:"wallet_address"`
}

func (g *GridDeps) handleFaucet(w http.ResponseWriter, r *http.Request) {
	if !g.DevnetMode {
		writeErr(w, http.StatusForbidden, "devnet faucet disabled — set IOGRID_CLUSTER=devnet")
		return
	}
	if g.Solana == nil || !g.Solana.Enabled() {
		writeErr(w, http.StatusServiceUnavailable, "solana service disabled (need GRID_TOKEN_MINT_ADDRESS + treasury keypair)")
		return
	}
	if g.Store == nil {
		writeErr(w, http.StatusServiceUnavailable, "grid store unconfigured")
		return
	}
	var req faucetReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.WalletAddress == "" {
		writeErr(w, http.StatusBadRequest, "wallet_address required")
		return
	}
	// Sanity-check the recipient is a valid base58 pubkey before we even
	// touch the rate-limit table.
	recipient := common.PublicKeyFromString(req.WalletAddress)
	if recipient.ToBase58() != req.WalletAddress {
		writeErr(w, http.StatusBadRequest, "wallet_address malformed")
		return
	}
	cooldown := g.FaucetCooldown
	if cooldown == 0 {
		cooldown = defaultFaucetCooldown
	}
	amount := g.FaucetAmount
	if amount == 0 {
		amount = defaultFaucetAmount
	}
	last, err := g.Store.LastFaucetClaim(r.Context(), req.WalletAddress)
	if err != nil && !errors.Is(err, grid.ErrNotFound) {
		writeErr(w, http.StatusInternalServerError, "rate-limit lookup failed")
		return
	}
	if last != nil && time.Since(last.ClaimedAt) < cooldown {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":           "rate_limited",
			"retry_after_sec": int(cooldown.Seconds() - time.Since(last.ClaimedAt).Seconds()),
		})
		return
	}
	// Submit the transfer from the treasury ATA. The Solana.Service
	// already exposes TransferGRID — we reuse it directly. (The faucet
	// transfers from the existing treasury supply rather than minting
	// fresh; on devnet this is functionally identical and keeps the
	// transaction simpler.)
	sig, err := g.Solana.TransferGRID(r.Context(), recipient, amount)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "transfer failed: "+err.Error())
		return
	}
	if err := g.Store.InsertFaucetClaim(r.Context(), &grid.FaucetClaim{
		WalletAddress: req.WalletAddress,
		MintedAtomic:  amount,
		TxSignature:   sig,
	}); err != nil {
		// Log only — the on-chain transfer already happened.
		if g.Logger != nil {
			g.Logger.Warn("grid: faucet claim record failed",
				slog.String("wallet", req.WalletAddress),
				slog.String("error", err.Error()))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"wallet_address": req.WalletAddress,
		"amount_atomic":  amount,
		"amount_grid":    "100",
		"signature":      sig,
	})
}

// FaucetClusterFromEnv returns true if the current process is allowed to
// run the devnet faucet (i.e. IOGRID_CLUSTER == "devnet"). Anywhere else
// (staging, production) the faucet is hard-disabled.
func FaucetClusterFromEnv() bool {
	return strings.EqualFold(os.Getenv("IOGRID_CLUSTER"), "devnet")
}
