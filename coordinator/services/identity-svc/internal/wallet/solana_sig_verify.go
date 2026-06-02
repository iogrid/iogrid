// Package wallet is the consumer-side (mobile VPN app) wallet binding
// primitive. It is intentionally a thin wrapper over internal/siws — the
// SIWS package already implements ed25519 verification, base58 decoding,
// and Solana address validation; this package layers the iogrid bind
// challenge format on top.
//
// Why a separate package: the SIWS flow under internal/siws is the
// PROVIDER side (Sign-In-With-Solana for $GRID payouts — provider must
// prove they control the destination address). The wallet bind here is
// the CONSUMER side — the mobile VPN user proves they control the
// wallet that holds the $GRID balance they'll be charged against. The
// challenge format + storage / replay defence are different (one-shot
// nonce embedded in the message, no Redis state). Sharing
// siws.VerifySignature underneath keeps the ed25519 path tested once.
package wallet

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/siws"
)

// Errors returned by this package. Mapped to HTTP statuses by the
// handler (4xx for everything user-controlled; the ed25519 path itself
// never raises a 5xx).
var (
	// ErrMalformedChallenge is returned when the signed-message string
	// does not match the iogrid bind format ("iogrid:bind:<nonce>:<ts>").
	ErrMalformedChallenge = errors.New("wallet: malformed bind challenge")
	// ErrExpiredChallenge is returned when the ts field of the challenge
	// is older than ChallengeMaxAge — defends against a stolen signature
	// being re-played weeks later.
	ErrExpiredChallenge = errors.New("wallet: bind challenge expired")
	// ErrInvalidSignature wraps siws.ErrInvalidSignature so callers can
	// switch on it without importing the siws package.
	ErrInvalidSignature = errors.New("wallet: invalid signature")
	// ErrInvalidAddress wraps siws.ErrInvalidAddress similarly.
	ErrInvalidAddress = errors.New("wallet: invalid wallet address")
)

// ChallengeMaxAge bounds how long a signed bind message stays
// acceptable. Five minutes matches the SIWS challenge TTL and is
// generous enough for a slow wallet round-trip on a flaky mobile link
// while keeping a stolen signature's replay window very small.
const ChallengeMaxAge = 5 * time.Minute

// BuildChallenge returns the exact bytes the wallet must sign for an
// iogrid bind. Format: "iogrid:bind:<nonce>:<unix_seconds>". The nonce
// is opaque to this package — callers should generate one with
// siws.NewNonce (or crypto/rand) and persist nothing (replay defence is
// the timestamp window plus the one-shot DB row).
func BuildChallenge(nonce string, ts time.Time) string {
	return fmt.Sprintf("iogrid:bind:%s:%d", nonce, ts.Unix())
}

// parseChallenge splits the wire form into (nonce, ts) and validates
// the prefix. Public so handlers can surface a precise error message
// for malformed payloads.
func parseChallenge(s string) (nonce string, ts time.Time, err error) {
	parts := strings.Split(s, ":")
	// Expect: ["iogrid", "bind", "<nonce>", "<ts>"]. The nonce may
	// contain hex characters but no colons.
	if len(parts) != 4 || parts[0] != "iogrid" || parts[1] != "bind" {
		return "", time.Time{}, ErrMalformedChallenge
	}
	nonce = parts[2]
	if nonce == "" {
		return "", time.Time{}, ErrMalformedChallenge
	}
	tsInt, parseErr := strconv.ParseInt(parts[3], 10, 64)
	if parseErr != nil {
		return "", time.Time{}, ErrMalformedChallenge
	}
	return nonce, time.Unix(tsInt, 0), nil
}

// VerifyBindSignature is the one-call entry point for the wallet bind
// HTTP handler. It:
//  1. Parses the challenge to confirm it's our shape (nonce + ts).
//  2. Checks the timestamp is within the ChallengeMaxAge window
//     (clock skew tolerance is one ChallengeMaxAge into the future too
//     so a wallet on a slightly-ahead device still passes).
//  3. Verifies the ed25519 signature against the claimed wallet
//     address via the shared siws.VerifySignature path.
//
// Returns nil on success; one of the package's sentinel errors otherwise.
func VerifyBindSignature(walletAddress, challenge, signatureB58 string, now time.Time) error {
	walletAddress = strings.TrimSpace(walletAddress)
	if walletAddress == "" {
		return ErrInvalidAddress
	}
	if _, err := siws.DecodeAddress(walletAddress); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidAddress, err)
	}
	_, ts, err := parseChallenge(challenge)
	if err != nil {
		return err
	}
	age := now.Sub(ts)
	if age > ChallengeMaxAge {
		return ErrExpiredChallenge
	}
	// Tolerate up to ChallengeMaxAge of future skew (device clock ahead).
	if age < -ChallengeMaxAge {
		return ErrExpiredChallenge
	}
	if err := siws.VerifySignature(walletAddress, challenge, signatureB58); err != nil {
		if errors.Is(err, siws.ErrInvalidAddress) {
			return fmt.Errorf("%w: %v", ErrInvalidAddress, err)
		}
		return fmt.Errorf("%w: %v", ErrInvalidSignature, err)
	}
	return nil
}
