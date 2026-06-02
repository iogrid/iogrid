package payment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/mr-tron/base58"
)

// BalanceFetcher abstracts the Solana RPC call for tests. The real
// implementation is SolanaRPCBalance; tests pass a stub.
type BalanceFetcher interface {
	GRIDAtomicBalance(ctx context.Context, wallet string) (uint64, error)
}

// SolanaRPCBalance is the production implementation. It computes the
// recipient's associated-token-account (ATA) for the $GRID mint and calls
// `getTokenAccountBalance` against the configured RPC.
type SolanaRPCBalance struct {
	RPCURL    string        // e.g. https://api.devnet.solana.com OR Helius free
	MintB58   string        // $GRID mint address (base58)
	HTTP      *http.Client  // injectable for tests; nil → 10s timeout default
	TokenProg string        // base58 token program id (legacy SPL by default)
}

// NewSolanaRPCBalance builds a fetcher with a 10s HTTP timeout and the
// legacy SPL Token program (`TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA`).
// Pass a non-empty `tokenProg` to override for Token-2022 mints.
func NewSolanaRPCBalance(rpcURL, mintB58, tokenProg string) *SolanaRPCBalance {
	if tokenProg == "" {
		tokenProg = LegacyTokenProgramID
	}
	return &SolanaRPCBalance{
		RPCURL:    rpcURL,
		MintB58:   mintB58,
		HTTP:      &http.Client{Timeout: 10 * time.Second},
		TokenProg: tokenProg,
	}
}

// LegacyTokenProgramID is the well-known legacy SPL Token program (matches
// the constant in billing-svc/internal/solana/chain.go).
const LegacyTokenProgramID = "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"

// AssociatedTokenProgramID is the Associated Token Account program id.
const AssociatedTokenProgramID = "ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL"

// GRIDAtomicBalance returns the recipient's $GRID balance in atomic units
// (9-decimal lamport-style). Returns 0 on "account does not exist" — that's
// the legitimate state when a wallet has never received any $GRID. Any
// other transport error bubbles up.
func (s *SolanaRPCBalance) GRIDAtomicBalance(ctx context.Context, wallet string) (uint64, error) {
	if s.RPCURL == "" {
		return 0, fmt.Errorf("payment: SolanaRPCBalance.RPCURL empty")
	}
	if s.MintB58 == "" {
		return 0, fmt.Errorf("payment: SolanaRPCBalance.MintB58 empty (set GRID_TOKEN_MINT_ADDRESS)")
	}
	ata, err := deriveATA(wallet, s.MintB58, s.TokenProg)
	if err != nil {
		return 0, fmt.Errorf("payment: derive ATA: %w", err)
	}
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "getTokenAccountBalance",
		"params":  []any{ata},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.RPCURL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return 0, fmt.Errorf("payment: solana rpc: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("payment: solana rpc HTTP %d: %s", resp.StatusCode, string(raw))
	}
	var rpcResp struct {
		Result struct {
			Value struct {
				Amount   string `json:"amount"`
				Decimals int    `json:"decimals"`
			} `json:"value"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &rpcResp); err != nil {
		return 0, fmt.Errorf("payment: decode rpc resp: %w (raw=%s)", err, truncate(string(raw), 200))
	}
	if rpcResp.Error != nil {
		// "could not find account" / "Invalid param: not a Token account" both
		// mean "no $GRID yet" — treat as zero, not as an error.
		switch rpcResp.Error.Code {
		case -32602, -32601:
			return 0, nil
		}
		if isAccountNotFoundMsg(rpcResp.Error.Message) {
			return 0, nil
		}
		return 0, fmt.Errorf("payment: solana rpc error %d: %s",
			rpcResp.Error.Code, rpcResp.Error.Message)
	}
	if rpcResp.Result.Value.Amount == "" {
		return 0, nil
	}
	n, err := strconv.ParseUint(rpcResp.Result.Value.Amount, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("payment: parse balance amount %q: %w",
			rpcResp.Result.Value.Amount, err)
	}
	return n, nil
}

func isAccountNotFoundMsg(s string) bool {
	switch {
	case contains(s, "could not find account"),
		contains(s, "Invalid param: not a Token account"),
		contains(s, "Invalid param: not a v4 account"),
		contains(s, "AccountNotFound"):
		return true
	}
	return false
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && bytes.Contains([]byte(haystack), []byte(needle))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// deriveATA derives the Associated Token Account address for a (wallet,
// mint, tokenProgram) triple. Implements the standard Solana ATA seed
// scheme:
//
//	seeds = [wallet_bytes, token_program_bytes, mint_bytes]
//	program = ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL
//
// We re-implement findProgramAddress in-package so vpn-svc doesn't have to
// pull in the entire blocto SDK just for one address derivation.
func deriveATA(walletB58, mintB58, tokenProgB58 string) (string, error) {
	wallet, err := base58.Decode(walletB58)
	if err != nil || len(wallet) != 32 {
		return "", fmt.Errorf("invalid wallet base58")
	}
	mint, err := base58.Decode(mintB58)
	if err != nil || len(mint) != 32 {
		return "", fmt.Errorf("invalid mint base58")
	}
	tokenProg, err := base58.Decode(tokenProgB58)
	if err != nil || len(tokenProg) != 32 {
		return "", fmt.Errorf("invalid token program base58")
	}
	assoc, err := base58.Decode(AssociatedTokenProgramID)
	if err != nil {
		return "", fmt.Errorf("invalid assoc program base58: %w", err)
	}
	seeds := [][]byte{wallet, tokenProg, mint}
	pda, _, err := findProgramAddress(seeds, assoc)
	if err != nil {
		return "", err
	}
	return base58.Encode(pda), nil
}
