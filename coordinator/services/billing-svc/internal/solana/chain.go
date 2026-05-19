// chain.go — thin wrapper around the blocto Solana RPC client.
//
// Centralises:
//   - blockhash + transaction submission
//   - submit-with-retry on ephemeral errors
//   - polling-based confirmation against `confirmed`/`finalized` commitment
//   - well-known program-id constants (Token-2022, SPL associated token)
//
// Higher-level callers (transfer.go, swap.go, burn.go) build instructions and
// hand them to `chainClient.SubmitAndConfirm`. The split keeps the Service
// type lean — it owns wallet + cfg + store, the chainClient owns the RPC.

package solana

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/blocto/solana-go-sdk/client"
	"github.com/blocto/solana-go-sdk/common"
	"github.com/blocto/solana-go-sdk/rpc"
	"github.com/blocto/solana-go-sdk/types"
)

// Token-2022 program id — the post-TGE $GRID mint will live under this
// program (transfer hooks / metadata extensions / etc per TOKENOMICS §"Token
// primitives"). When the env-var-supplied mint addr is owned by the legacy
// Token program the chainClient falls back automatically.
var (
	Token2022ProgramID = common.Token2022ProgramID
	LegacyTokenProgID  = common.TokenProgramID
)

// chainClient is a thin wrapper over the blocto SDK Client. Exposed for the
// rest of this package only — not for callers outside billing-svc.
type chainClient struct {
	rpc    *client.Client
	logger *slog.Logger

	// Tunables — exposed for tests.
	maxSubmitAttempts int
	confirmTimeout    time.Duration
	confirmPoll       time.Duration
	commitment        rpc.Commitment
}

// newChainClient constructs a chainClient pointed at `rpcURL`. A nil logger
// becomes slog.Default. Defaults: 3 submit attempts, 90s confirm timeout,
// 1s poll, `confirmed` commitment (matches Squads / Helius best-practice for
// off-chain bookkeeping).
func newChainClient(rpcURL string, logger *slog.Logger) *chainClient {
	if logger == nil {
		logger = slog.Default()
	}
	return &chainClient{
		rpc:               client.NewClient(rpcURL),
		logger:            logger,
		maxSubmitAttempts: 3,
		confirmTimeout:    90 * time.Second,
		confirmPoll:       1 * time.Second,
		commitment:        rpc.CommitmentConfirmed,
	}
}

// LatestBlockhash returns the most recent blockhash + last-valid block height
// at `confirmed` commitment.
func (c *chainClient) LatestBlockhash(ctx context.Context) (string, error) {
	res, err := c.rpc.GetLatestBlockhash(ctx)
	if err != nil {
		return "", fmt.Errorf("solana: getLatestBlockhash: %w", err)
	}
	return res.Blockhash, nil
}

// BuildAndSubmit builds a Transaction from the given instructions, signs it
// with `signers` (first signer is the fee payer), and submits via
// `sendTransaction` with retry on ephemeral RPC errors.
//
// Returns the base58 transaction signature on success. The caller is
// responsible for calling `ConfirmSignature` (or using `SubmitAndConfirm`)
// to wait for inclusion.
func (c *chainClient) BuildAndSubmit(
	ctx context.Context,
	instructions []types.Instruction,
	signers []types.Account,
	feePayer common.PublicKey,
) (string, error) {
	if len(signers) == 0 {
		return "", errors.New("solana: BuildAndSubmit: at least one signer required")
	}
	if len(instructions) == 0 {
		return "", errors.New("solana: BuildAndSubmit: no instructions")
	}

	var lastErr error
	for attempt := 1; attempt <= c.maxSubmitAttempts; attempt++ {
		bh, err := c.LatestBlockhash(ctx)
		if err != nil {
			lastErr = err
			if !isEphemeralRPCErr(err) {
				return "", err
			}
			c.logger.Warn("solana: blockhash fetch failed, retrying",
				slog.Int("attempt", attempt),
				slog.String("error", err.Error()))
			if !sleepCtx(ctx, backoff(attempt)) {
				return "", ctx.Err()
			}
			continue
		}

		tx, err := types.NewTransaction(types.NewTransactionParam{
			Message: types.NewMessage(types.NewMessageParam{
				FeePayer:        feePayer,
				RecentBlockhash: bh,
				Instructions:    instructions,
			}),
			Signers: signers,
		})
		if err != nil {
			// Build failures are programmer errors (e.g. signer not in
			// account list); no retry.
			return "", fmt.Errorf("solana: assemble tx: %w", err)
		}
		raw, err := tx.Serialize()
		if err != nil {
			return "", fmt.Errorf("solana: serialize tx: %w", err)
		}
		encoded := base64.StdEncoding.EncodeToString(raw)

		resp, err := c.rpc.RpcClient.SendTransactionWithConfig(ctx, encoded,
			rpc.SendTransactionConfig{
				Encoding:            rpc.SendTransactionConfigEncodingBase64,
				SkipPreflight:       false,
				PreflightCommitment: c.commitment,
			},
		)
		if err == nil && resp.Error == nil {
			c.logger.Info("solana: tx submitted",
				slog.String("signature", resp.Result),
				slog.Int("attempt", attempt))
			return resp.Result, nil
		}
		// `resp.Error` is an RPC-level error (e.g. "blockhash not found");
		// `err` is a transport-level error (e.g. dial timeout).
		var errMsg string
		if err != nil {
			errMsg = err.Error()
		} else {
			errMsg = fmt.Sprintf("rpc error: %v", resp.Error)
		}
		lastErr = fmt.Errorf("sendTransaction: %s", errMsg)

		if !isEphemeralRPCErr(lastErr) {
			return "", lastErr
		}
		c.logger.Warn("solana: tx submit failed, retrying",
			slog.Int("attempt", attempt),
			slog.String("error", errMsg))
		if !sleepCtx(ctx, backoff(attempt)) {
			return "", ctx.Err()
		}
	}
	return "", fmt.Errorf("solana: tx submit gave up after %d attempts: %w",
		c.maxSubmitAttempts, lastErr)
}

// ConfirmSignature polls `getSignatureStatuses` until the transaction is at
// the configured commitment (or finalized) — whichever comes first — or the
// timeout expires.
//
// Returns nil on success, ctx.Err() on cancel, and a wrapped error on
// timeout / on-chain failure. The on-chain "Err" field, if non-nil, makes
// this method return a non-retryable error: the tx made it on-chain but
// reverted.
func (c *chainClient) ConfirmSignature(ctx context.Context, signature string) error {
	deadline := time.Now().Add(c.confirmTimeout)
	for {
		st, err := c.rpc.GetSignatureStatusWithConfig(ctx, signature, client.GetSignatureStatusesConfig{
			SearchTransactionHistory: true,
		})
		if err != nil && !isEphemeralRPCErr(err) {
			return fmt.Errorf("solana: getSignatureStatus: %w", err)
		}
		if st != nil {
			if st.Err != nil {
				return fmt.Errorf("solana: tx %s reverted on-chain: %v", signature, st.Err)
			}
			if st.ConfirmationStatus != nil {
				switch *st.ConfirmationStatus {
				case rpc.CommitmentConfirmed, rpc.CommitmentFinalized:
					return nil
				}
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("solana: confirm %s timed out after %s",
				signature, c.confirmTimeout)
		}
		if !sleepCtx(ctx, c.confirmPoll) {
			return ctx.Err()
		}
	}
}

// SubmitAndConfirm = BuildAndSubmit + ConfirmSignature.
func (c *chainClient) SubmitAndConfirm(
	ctx context.Context,
	instructions []types.Instruction,
	signers []types.Account,
	feePayer common.PublicKey,
) (string, error) {
	sig, err := c.BuildAndSubmit(ctx, instructions, signers, feePayer)
	if err != nil {
		return "", err
	}
	if err := c.ConfirmSignature(ctx, sig); err != nil {
		return sig, err
	}
	return sig, nil
}

// isEphemeralRPCErr returns true for RPC errors that are worth retrying.
// We're conservative — only obvious-transient classes match.
func isEphemeralRPCErr(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "blockhash not found"):
		return true
	case strings.Contains(s, "timeout"), strings.Contains(s, "deadline exceeded"):
		return true
	case strings.Contains(s, "connection reset"), strings.Contains(s, "eof"):
		return true
	case strings.Contains(s, "503"), strings.Contains(s, "502"), strings.Contains(s, "504"):
		return true
	case strings.Contains(s, "429"), strings.Contains(s, "rate limit"):
		return true
	case strings.Contains(s, "node is behind"):
		return true
	}
	return false
}

// backoff returns exponential backoff with a small floor (250ms).
func backoff(attempt int) time.Duration {
	base := 250 * time.Millisecond
	for i := 1; i < attempt; i++ {
		base *= 2
	}
	if base > 4*time.Second {
		base = 4 * time.Second
	}
	return base
}

// sleepCtx sleeps for d unless ctx fires first. Returns false on cancel.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
