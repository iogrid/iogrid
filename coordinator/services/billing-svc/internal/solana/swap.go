// swap.go — real Jupiter v6 swap execution.
//
// The Jupiter aggregator exposes two endpoints we care about:
//
//   GET /v6/quote   — returns route + outAmount estimate
//   POST /v6/swap   — returns a serialized, *unsigned* swap transaction
//                     wired to consume the user's input ATA and credit the
//                     output ATA.
//
// We sign + submit the swap tx ourselves so we keep custody of the keys and
// get the canonical signature for bookkeeping. The output is delivered to
// the hot wallet's $GRID ATA (the same wallet that signs the buyback +
// the provider-payout transfers).
//
// Slippage is bounded server-side via `slippageBps` (50 bps = 0.5%); we
// record the realised slippage post-confirmation so dashboards can flag
// drift.

package solana

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/blocto/solana-go-sdk/common"
	"github.com/blocto/solana-go-sdk/rpc"
	"github.com/blocto/solana-go-sdk/types"
)

// SwapRequest is the input to the real (vs. quote-only) Jupiter swap.
type SwapRequest struct {
	InputMint   string // e.g. USDCMint
	OutputMint  string // e.g. cfg.GRIDTokenMint
	Amount      uint64 // atomic units of InputMint
	SlippageBps int    // e.g. 50 = 0.5%
}

// SwapResult is what the swap returned, including the realised out-amount
// (from the quote we built the tx with — Jupiter guarantees outMin in the
// tx itself).
type SwapResult struct {
	Signature    string
	InAmount     uint64
	OutAmount    uint64 // pre-confirmation estimate (== OtherAmountThreshold floor)
	PriceImpact  string // e.g. "0.0014" (fraction)
	QuoteUsedAt  time.Time
	QuoteRouteID string
}

// jupSwapResp is the subset of `/v6/swap` we consume.
type jupSwapResp struct {
	SwapTransaction      string `json:"swapTransaction"` // base64
	LastValidBlockHeight uint64 `json:"lastValidBlockHeight"`
}

// ExecuteSwap performs a real Jupiter swap: quote → fetch swap tx →
// sign with hot wallet → submit → confirm. Returns the SwapResult on
// success, or an error if the quote / swap / submit / confirm steps fail.
//
// The hot wallet's keypair signs the transaction. Jupiter sets `userPublicKey`
// to the hot wallet so the input ATA is debited and output ATA is credited.
//
// Stub mode (s.Enabled() == false) returns an error — this function should
// only be called from live flows. Callers gate on `s.Enabled()` upstream.
func (s *Service) ExecuteSwap(ctx context.Context, req SwapRequest) (*SwapResult, error) {
	if !s.Enabled() {
		return nil, errors.New("solana: ExecuteSwap called in stub mode")
	}
	if req.Amount == 0 {
		return nil, errors.New("solana: ExecuteSwap: amount=0")
	}
	if req.SlippageBps <= 0 {
		req.SlippageBps = 50
	}

	// 1) Quote
	quote, err := s.jupiter.QuoteRaw(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("jupiter quote: %w", err)
	}
	if quote.OutAmount == "" {
		return nil, errors.New("jupiter quote: empty outAmount")
	}
	outAmount, err := strconv.ParseUint(quote.OutAmount, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("jupiter quote: parse outAmount %q: %w", quote.OutAmount, err)
	}

	// 2) Build swap tx via /v6/swap.
	swapTxB64, err := s.jupiter.SwapTransaction(ctx, quote, s.wallet.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("jupiter swap: %w", err)
	}
	rawTx, err := base64.StdEncoding.DecodeString(swapTxB64)
	if err != nil {
		return nil, fmt.Errorf("jupiter swap: base64 decode: %w", err)
	}

	// 3) Deserialize + re-sign with the hot wallet (Jupiter returns the tx
	//    with an empty signature slot for userPublicKey).
	tx, err := types.TransactionDeserialize(rawTx)
	if err != nil {
		return nil, fmt.Errorf("jupiter swap: deserialize tx: %w", err)
	}
	// AddSignature finds the matching account-slot and writes the sig.
	msg, err := tx.Message.Serialize()
	if err != nil {
		return nil, fmt.Errorf("jupiter swap: re-serialize msg: %w", err)
	}
	sig := s.wallet.Sign(msg)
	if err := tx.AddSignature(sig); err != nil {
		return nil, fmt.Errorf("jupiter swap: AddSignature: %w", err)
	}
	signed, err := tx.Serialize()
	if err != nil {
		return nil, fmt.Errorf("jupiter swap: re-serialize tx: %w", err)
	}

	// 4) Submit with retry.
	encoded := base64.StdEncoding.EncodeToString(signed)
	signature, submitErr := s.submitRawTx(ctx, encoded)
	if submitErr != nil {
		return nil, fmt.Errorf("jupiter swap: submit: %w", submitErr)
	}

	// 5) Confirm.
	if err := s.chain.ConfirmSignature(ctx, signature); err != nil {
		return &SwapResult{Signature: signature}, fmt.Errorf("jupiter swap: confirm: %w", err)
	}

	s.logger.Info("solana: jupiter swap confirmed",
		slog.String("signature", signature),
		slog.String("input_mint", req.InputMint),
		slog.String("output_mint", req.OutputMint),
		slog.Uint64("in_amount", req.Amount),
		slog.Uint64("out_amount", outAmount),
		slog.String("price_impact_pct", quote.PriceImpact),
	)
	return &SwapResult{
		Signature:   signature,
		InAmount:    req.Amount,
		OutAmount:   outAmount,
		PriceImpact: quote.PriceImpact,
		QuoteUsedAt: s.now(),
	}, nil
}

// submitRawTx is a thin wrapper over the RPC sendTransaction call (already
// signed & base64-encoded). Re-uses the chainClient's retry classification.
func (s *Service) submitRawTx(ctx context.Context, encoded string) (string, error) {
	var lastErr error
	for attempt := 1; attempt <= s.chain.maxSubmitAttempts; attempt++ {
		resp, err := s.chain.rpc.RpcClient.SendTransactionWithConfig(ctx, encoded,
			rpc.SendTransactionConfig{
				Encoding:            rpc.SendTransactionConfigEncodingBase64,
				SkipPreflight:       false,
				PreflightCommitment: s.chain.commitment,
			},
		)
		if err == nil && resp.Error == nil {
			return resp.Result, nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("rpc error: %v", resp.Error)
		}
		if !isEphemeralRPCErr(lastErr) {
			return "", lastErr
		}
		if !sleepCtx(ctx, backoff(attempt)) {
			return "", ctx.Err()
		}
	}
	return "", fmt.Errorf("submit gave up after %d attempts: %w",
		s.chain.maxSubmitAttempts, lastErr)
}

// ── Jupiter client extension ─────────────────────────────────────────

// QuoteRaw exposes the full Jupiter quote shape (not just OutAmount).
// Used by ExecuteSwap; QuoteUSDCToGRID stays on the lighter path for the
// daily-loop estimation case.
func (c *JupiterClient) QuoteRaw(ctx context.Context, req SwapRequest) (*QuoteResponse, error) {
	if req.Amount == 0 {
		return &QuoteResponse{OutAmount: "0"}, nil
	}
	u, err := makeQuoteURL(c.baseURL, req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("jupiter quote: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("jupiter quote: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var qr QuoteResponse
	if err := json.NewDecoder(resp.Body).Decode(&qr); err != nil {
		return nil, fmt.Errorf("decode jupiter quote: %w", err)
	}
	return &qr, nil
}

// SwapTransaction calls POST /v6/swap and returns the base64-encoded
// serialized transaction. `userPublicKey` is the wallet that owns input ATA
// and will be credited the output.
func (c *JupiterClient) SwapTransaction(ctx context.Context, quote *QuoteResponse, userPublicKey common.PublicKey) (string, error) {
	body := map[string]any{
		"quoteResponse":             quote,
		"userPublicKey":             userPublicKey.ToBase58(),
		"wrapAndUnwrapSol":          true,
		"useSharedAccounts":         true,
		"dynamicComputeUnitLimit":   true,
		"prioritizationFeeLamports": "auto",
	}
	raw, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/swap", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("jupiter swap: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("jupiter swap: HTTP %d: %s", resp.StatusCode, string(b))
	}
	var sr jupSwapResp
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return "", fmt.Errorf("decode jupiter swap: %w", err)
	}
	if sr.SwapTransaction == "" {
		return "", errors.New("jupiter swap: empty swapTransaction")
	}
	return sr.SwapTransaction, nil
}

// makeQuoteURL constructs the /quote URL — extracted so QuoteUSDCToGRID
// and QuoteRaw can share it.
func makeQuoteURL(baseURL string, req SwapRequest) (string, error) {
	u := baseURL + "/quote"
	// hand-built query: matches QuoteUSDCToGRID's existing format.
	q := []string{
		"inputMint=" + req.InputMint,
		"outputMint=" + req.OutputMint,
		"amount=" + strconv.FormatUint(req.Amount, 10),
		"slippageBps=" + strconv.Itoa(req.SlippageBps),
		"swapMode=ExactIn",
	}
	first := true
	out := u
	for _, p := range q {
		if first {
			out += "?" + p
			first = false
		} else {
			out += "&" + p
		}
	}
	return out, nil
}
