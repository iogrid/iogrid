// Package siws implements the Sign-In-With-Solana flow used by
// identity-svc to bind a provider's Solana wallet to their User record.
//
// Why this exists
// ---------------
// Provider payouts shift from Stripe Connect to native $GRID per
// docs/TOKENOMICS.md. Every payout target is a Solana wallet, so before
// the daemon can ship work the operator must prove they control the
// destination address. SIWS is the standard pattern: server issues a
// random nonce embedded in a canonical message, the wallet ed25519-signs
// the message bytes, and the server verifies the signature against the
// claimed public key.
//
// Message format
// --------------
// The exact bytes the wallet must sign:
//
//   iogrid.org wants you to sign in with your Solana account: <addr>
//
//   Nonce: <64-hex-chars>
//
// Both Phantom and Solflare render the message verbatim in their signature
// prompt, so the user sees the iogrid identity in cleartext before they
// approve. The 32-byte nonce gives 256-bit collision resistance; replay
// is further prevented by deleting the challenge after a single
// verification attempt (see ChallengeStore.Consume).
//
// Verification
// ------------
// VerifySignature decodes the base58 address into a 32-byte ed25519
// public key, the base58 signature into a 64-byte ed25519 signature, and
// runs the stdlib ed25519.Verify. Wallets sign the raw UTF-8 bytes — we
// do NOT prepend Solana's "off-chain message envelope" because Phantom /
// Solflare's signMessage RPC operates on the raw payload (the envelope
// is a node-level transaction concept).
package siws

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/mr-tron/base58"
	"github.com/redis/go-redis/v9"
)

// Errors returned by this package. Callers should test with errors.Is so
// HTTP handlers can map to 4xx vs 5xx without string comparison.
var (
	// ErrInvalidAddress is returned when the wallet_address argument is
	// not a valid base58-encoded 32-byte ed25519 public key.
	ErrInvalidAddress = errors.New("siws: invalid wallet address")
	// ErrInvalidSignature is returned when the signature failed
	// verification, was not base58, or was not 64 bytes.
	ErrInvalidSignature = errors.New("siws: invalid signature")
	// ErrChallengeNotFound is returned when the challenge has expired or
	// has already been consumed (replay defence).
	ErrChallengeNotFound = errors.New("siws: challenge not found or expired")
)

// DefaultChallengeTTL is the lifetime of a SIWS challenge. 5 minutes is
// long enough for a slow user to click through Phantom's confirmation
// modal and short enough that a stolen challenge has very little reuse
// window.
const DefaultChallengeTTL = 5 * time.Minute

// BuildMessage returns the exact byte string the wallet must sign. The
// shape is the SIWS canonical form: a clear-text scope line ("<domain>
// wants you to sign in with your Solana account: <addr>") followed by a
// blank line and a Nonce: <hex> line.
func BuildMessage(domain, walletAddress, nonceHex string) string {
	return fmt.Sprintf(
		"%s wants you to sign in with your Solana account: %s\n\nNonce: %s",
		domain, walletAddress, nonceHex,
	)
}

// NewNonce returns 32 cryptographically random bytes hex-encoded. 64 hex
// chars × 4 bits = 256 bits of entropy.
func NewNonce() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("siws: rand: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// DecodeAddress returns the raw 32-byte ed25519 public key for a
// base58-encoded Solana address. Returns ErrInvalidAddress on any
// decode / length error.
func DecodeAddress(addr string) (ed25519.PublicKey, error) {
	if addr == "" {
		return nil, ErrInvalidAddress
	}
	raw, err := base58.Decode(addr)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidAddress, err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: expected %d bytes, got %d",
			ErrInvalidAddress, ed25519.PublicKeySize, len(raw))
	}
	return ed25519.PublicKey(raw), nil
}

// VerifySignature checks that signatureB58 is a valid ed25519 signature
// of messageBytes by the keypair whose public component is encoded as
// walletAddress (base58). Wraps the stdlib ed25519.Verify so callers
// can't accidentally bypass the constant-time path.
func VerifySignature(walletAddress, message, signatureB58 string) error {
	pub, err := DecodeAddress(walletAddress)
	if err != nil {
		return err
	}
	sig, err := base58.Decode(signatureB58)
	if err != nil {
		return fmt.Errorf("%w: not base58", ErrInvalidSignature)
	}
	if len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("%w: expected %d bytes, got %d",
			ErrInvalidSignature, ed25519.SignatureSize, len(sig))
	}
	if !ed25519.Verify(pub, []byte(message), sig) {
		return ErrInvalidSignature
	}
	return nil
}

// EncodeSignature is the inverse of base58-decode used in
// VerifySignature. Exposed so tests can produce signatures wallets would
// otherwise create.
func EncodeSignature(sig []byte) string {
	return base58.Encode(sig)
}

// EncodeAddress base58-encodes an ed25519 public key the way Solana
// wallets present it to users.
func EncodeAddress(pub ed25519.PublicKey) string {
	return base58.Encode(pub)
}

// --- Challenge storage ----------------------------------------------------

// ChallengeRecord is the server-side state for an outstanding SIWS
// challenge. We store the nonce + the full message so completion does
// not need to re-derive (and so the message can never silently drift
// between Start and Complete).
type ChallengeRecord struct {
	WalletAddress string
	Nonce         string
	Message       string
	UserID        string // empty when create-if-missing flow
	ExpiresAt     time.Time
}

// ChallengeStore persists outstanding challenges with TTL. The Redis
// implementation is canonical; tests substitute MemoryChallengeStore.
type ChallengeStore interface {
	Put(ctx context.Context, rec ChallengeRecord, ttl time.Duration) error
	Consume(ctx context.Context, walletAddress string) (ChallengeRecord, error)
}

// challengeKey is the Redis key namespace for outstanding challenges.
// Keyed by (wallet_address) so a fresh Start invalidates a prior pending
// challenge — important when the user retries because Phantom timed out.
func challengeKey(walletAddress string) string {
	return "iogrid:identity:siws:" + walletAddress
}

// RedisChallengeStore is the production challenge store. SET NX is not
// used — a fresh Start always overwrites; we rely on the consume-and-
// delete contract to prevent replay.
type RedisChallengeStore struct {
	Client *redis.Client
}

// Put writes the challenge with the supplied TTL.
func (s *RedisChallengeStore) Put(ctx context.Context, rec ChallengeRecord, ttl time.Duration) error {
	if s == nil || s.Client == nil {
		return errors.New("siws: redis client not configured")
	}
	// Pack as nonce|user_id|message so we can recover all three fields in
	// Consume. Walletaddress is the key so we don't need to store it.
	value := rec.Nonce + "\x00" + rec.UserID + "\x00" + rec.Message
	return s.Client.Set(ctx, challengeKey(rec.WalletAddress), value, ttl).Err()
}

// Consume atomically reads + deletes the challenge for the given
// address. Returns ErrChallengeNotFound when the key is missing
// (expired, never created, or already consumed).
func (s *RedisChallengeStore) Consume(ctx context.Context, walletAddress string) (ChallengeRecord, error) {
	if s == nil || s.Client == nil {
		return ChallengeRecord{}, errors.New("siws: redis client not configured")
	}
	key := challengeKey(walletAddress)
	// GETDEL is available on Redis 6.2+. Atomic on the server.
	val, err := s.Client.GetDel(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return ChallengeRecord{}, ErrChallengeNotFound
	}
	if err != nil {
		return ChallengeRecord{}, fmt.Errorf("siws: redis getdel: %w", err)
	}
	return parseChallengeValue(walletAddress, val)
}

// parseChallengeValue is the inverse of the pack in Put.
func parseChallengeValue(walletAddress, val string) (ChallengeRecord, error) {
	// Three NUL-separated fields: nonce, user_id, message.
	a := indexByte(val, 0)
	if a < 0 {
		return ChallengeRecord{}, errors.New("siws: malformed challenge record")
	}
	b := indexByte(val[a+1:], 0)
	if b < 0 {
		return ChallengeRecord{}, errors.New("siws: malformed challenge record (missing message)")
	}
	return ChallengeRecord{
		WalletAddress: walletAddress,
		Nonce:         val[:a],
		UserID:        val[a+1 : a+1+b],
		Message:       val[a+1+b+1:],
	}, nil
}

// indexByte avoids importing strings just for IndexByte.
func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// --- in-memory store (tests / dev fallback) -------------------------------

// MemoryChallengeStore is a process-local fallback used when Redis is
// unavailable (dev mode) and by unit tests. NOT safe across pods.
type MemoryChallengeStore struct {
	records map[string]memRecord
}

type memRecord struct {
	rec     ChallengeRecord
	expires time.Time
}

// NewMemoryChallengeStore returns an empty in-memory store.
func NewMemoryChallengeStore() *MemoryChallengeStore {
	return &MemoryChallengeStore{records: map[string]memRecord{}}
}

// Put implements ChallengeStore.
func (m *MemoryChallengeStore) Put(_ context.Context, rec ChallengeRecord, ttl time.Duration) error {
	m.records[rec.WalletAddress] = memRecord{rec: rec, expires: time.Now().Add(ttl)}
	return nil
}

// Consume implements ChallengeStore.
func (m *MemoryChallengeStore) Consume(_ context.Context, walletAddress string) (ChallengeRecord, error) {
	r, ok := m.records[walletAddress]
	if !ok {
		return ChallengeRecord{}, ErrChallengeNotFound
	}
	delete(m.records, walletAddress)
	if time.Now().After(r.expires) {
		return ChallengeRecord{}, ErrChallengeNotFound
	}
	return r.rec, nil
}
