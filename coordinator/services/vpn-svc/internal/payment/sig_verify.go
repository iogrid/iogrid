// Package payment is the $GRID payment-authorization layer for vpn-svc.
//
// Refs iogrid/iogrid#596 (Track 5 / EPIC #581).
//
// Two responsibilities live here:
//
//   - sig_verify.go     — ed25519 verify against a Solana base58 pubkey
//   - solana_balance.go — getTokenAccountBalance lookup via Solana RPC
//   - escrow.go         — domain types + arithmetic (atomic-unit math)
//
// The handlers package consumes these via the Service interface defined in
// escrow.go so unit tests can swap in a fake balance fetcher + clock.
package payment

import (
	"crypto/ed25519"
	"errors"
	"fmt"

	"github.com/mr-tron/base58"
)

// ErrSigInvalid is returned when an ed25519 signature does not match the
// supplied (wallet_address, message) pair. Callers should respond 401
// without leaking the precise sub-cause (signature vs. encoding).
var ErrSigInvalid = errors.New("payment: signature verification failed")

// SolanaPubkeyLen is the byte length of an ed25519 pubkey on Solana (32).
const SolanaPubkeyLen = 32

// SolanaSignatureLen is the byte length of an ed25519 signature (64).
const SolanaSignatureLen = 64

// VerifySolanaSignature returns nil iff `sig` is a valid ed25519 signature
// of `msg` under `wallet`. All inputs are base58 strings (Solana
// convention); base64/hex callers must convert at the boundary.
//
// Invariants enforced:
//   - wallet decodes to exactly 32 bytes (a valid ed25519 public key)
//   - signature decodes to exactly 64 bytes
//   - msg is treated as raw bytes — caller is responsible for any canonical
//     framing (we don't impose a JSON-canonicalisation step; the format is
//     a deterministic ':'-separated string per the #596 spec)
func VerifySolanaSignature(walletBase58, msg, signatureBase58 string) error {
	pubBytes, err := base58.Decode(walletBase58)
	if err != nil {
		return fmt.Errorf("%w: decode wallet: %v", ErrSigInvalid, err)
	}
	if len(pubBytes) != SolanaPubkeyLen {
		return fmt.Errorf("%w: wallet must be 32 bytes, got %d", ErrSigInvalid, len(pubBytes))
	}
	sigBytes, err := base58.Decode(signatureBase58)
	if err != nil {
		return fmt.Errorf("%w: decode signature: %v", ErrSigInvalid, err)
	}
	if len(sigBytes) != SolanaSignatureLen {
		return fmt.Errorf("%w: signature must be 64 bytes, got %d", ErrSigInvalid, len(sigBytes))
	}
	if !ed25519.Verify(ed25519.PublicKey(pubBytes), []byte(msg), sigBytes) {
		return ErrSigInvalid
	}
	return nil
}

// ValidateSolanaPubkey returns nil iff `s` decodes to a 32-byte ed25519
// pubkey. Surfaces "wallet_address required" / "wallet_address must be a
// 32-byte base58 string" as 400-able errors to the caller.
func ValidateSolanaPubkey(s string) error {
	if s == "" {
		return errors.New("wallet_address required")
	}
	b, err := base58.Decode(s)
	if err != nil {
		return fmt.Errorf("wallet_address invalid base58: %w", err)
	}
	if len(b) != SolanaPubkeyLen {
		return fmt.Errorf("wallet_address must decode to 32 bytes (got %d)", len(b))
	}
	return nil
}
