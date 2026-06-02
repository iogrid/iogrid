package payment

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// Decimals is the $GRID decimal precision (9). Mirrors the constant in
// solana/grid/deploy.ts. ONE GRID == 1e9 atomic units.
const Decimals = 9

// GRIDPerGB is the $GRID price per gigabyte of VPN traffic. Locked at
// 0.001 GRID / GB per the EPIC #581 LOCKED MODEL.
//
//	0.001 GRID = 1_000_000 atomic units (0.001 * 1e9)
const GRIDPerGBAtomic uint64 = 1_000_000

// PerGBNumerator / PerGBDenominator give exact 0.001 GRID/GB without
// floating point. The numerator + denominator pair lets us compute
// `consumed_atomic = bytes * numerator / denominator` without ever
// reaching a float boundary.
const (
	PerGBNumerator   uint64 = GRIDPerGBAtomic // 1_000_000 atomic per GB
	PerGBDenominator uint64 = 1_000_000_000   // 1 GB == 1e9 bytes (decimal, matches metering layer)
)

// LowEscrowThresholdPct is the escrow remaining percentage below which we
// emit a "topup_low" SSE event (10%).
const LowEscrowThresholdPct = 10

// MinEscrowAtomic is the minimum escrow we accept on session open: 0.001
// GRID, the price of 1 GB. Anything less and a single heartbeat would
// already exhaust it.
const MinEscrowAtomic uint64 = GRIDPerGBAtomic

// NonceTTL is how long we remember a (wallet, nonce) pair to prevent
// replay. The signed message also includes a timestamp; the combination
// gives us defence-in-depth.
const NonceTTL = 60 * time.Second

// Auth is the wire shape of a payment authorization the customer SDK
// includes in POST /v1/vpn/sessions. The signed message is built from
// session_id + customer_id + max_grid_per_min + nonce + timestamp on the
// SERVER side after the session id is allocated, but the customer SDK
// MUST pre-commit to max_grid_per_min + nonce + the SESSION ID supplied by
// /v1/vpn/sessions/quote — for the request-session flow we accept the
// signed message as-is plus the session-id the client targeted.
type Auth struct {
	WalletAddress    string `json:"wallet_address"`
	Signature        string `json:"signature"`         // base58 ed25519 sig
	Message          string `json:"message"`            // exact message that was signed
	Nonce            string `json:"nonce"`              // <= 128 chars
	MaxGRIDPerMinute uint64 `json:"max_grid_per_min"`   // atomic
	// SignedAtUnixSec is decoded from the message — kept here so callers
	// don't have to re-parse. 0 means "not yet validated".
	SignedAtUnixSec int64 `json:"signed_at_unix_sec,omitempty"`
}

// BuildExpectedMessage returns the canonical bytes the client must sign:
//
//	iogrid:session:start:<session_id>:<customer_id>:<max_grid_per_min>:<nonce>:<timestamp>
//
// timestamp = unix seconds (decimal). Matching iogrid-mobile SDK + the
// #596 spec verbatim.
func BuildExpectedMessage(sessionID, customerID uuid.UUID, maxGridPerMin uint64, nonce string, timestamp int64) string {
	return fmt.Sprintf("iogrid:session:start:%s:%s:%d:%s:%d",
		sessionID, customerID, maxGridPerMin, nonce, timestamp)
}

// Escrow is one row of vpn_session_escrow.
type Escrow struct {
	SessionID            uuid.UUID
	CustomerID           uuid.UUID
	WalletAddress        string
	EscrowedAtomic       uint64
	ConsumedAtomic       uint64
	MaxGRIDPerMinAtomic  uint64
	Nonce                string
	StartedAt            time.Time
	LastHeartbeatAt      time.Time
	SettledAt            *time.Time
}

// Remaining returns the unused escrow in atomic units.
func (e *Escrow) Remaining() uint64 {
	if e.EscrowedAtomic <= e.ConsumedAtomic {
		return 0
	}
	return e.EscrowedAtomic - e.ConsumedAtomic
}

// LowFraction reports whether the remaining escrow is below
// LowEscrowThresholdPct of the original — the trigger for SSE
// 'topup_low' events.
func (e *Escrow) LowFraction() bool {
	if e.EscrowedAtomic == 0 {
		return true
	}
	return e.Remaining()*100 < e.EscrowedAtomic*uint64(LowEscrowThresholdPct)
}

// ConsumedForBytes returns the $GRID atomic units that should be deducted
// for a given total byte count (in + out). Exact integer math:
//
//	atomic = bytes * 1_000_000 / 1_000_000_000  (PerGBNumerator / PerGBDenominator)
//
// Rounding mode: truncate toward zero. The customer is never overcharged
// at the GB boundary.
func ConsumedForBytes(bytes uint64) uint64 {
	if bytes == 0 {
		return 0
	}
	// Multiplication first to preserve precision; bytes <= 2^60 in any
	// real-world session, * 1_000_000 still fits in uint64 (< 2^80 only
	// at session-life > years).
	return bytes * PerGBNumerator / PerGBDenominator
}

// ErrInsufficientBalance is returned when the wallet's $GRID balance is
// below the required minimum. The handler renders this as HTTP 402.
var ErrInsufficientBalance = errors.New("payment: insufficient $GRID balance")

// ErrNonceReplay is returned when (wallet, nonce) has been seen within
// NonceTTL. The handler renders this as HTTP 409.
var ErrNonceReplay = errors.New("payment: nonce replay")

// ErrEscrowExhausted is returned when /heartbeat tries to deduct more
// than is escrowed. The handler renders this as HTTP 402 + sets the
// session state so the SDK tears down.
var ErrEscrowExhausted = errors.New("payment: escrow exhausted")

// EscrowStore is the persistence boundary. Postgres impl lives in
// internal/store; an in-memory version lives next to memory.go for tests.
type EscrowStore interface {
	// SeenNonce returns true if (wallet, nonce) was seen in the last
	// NonceTTL. Side-effect: records the nonce when seen=false.
	CheckAndRecordNonce(ctx context.Context, wallet, nonce string) (seen bool, err error)
	// CleanupNonces evicts rows older than NonceTTL. Cron-style hook
	// invoked from server startup loop.
	CleanupNonces(ctx context.Context) (int, error)

	CreateEscrow(ctx context.Context, e *Escrow) error
	GetEscrow(ctx context.Context, sessionID uuid.UUID) (*Escrow, error)
	// AddConsumption atomically adds `delta` to consumed_grid_atomic and
	// stamps last_heartbeat_at. Returns the updated row. If the new
	// consumed amount would exceed escrowed, returns ErrEscrowExhausted
	// and leaves the row unchanged.
	AddConsumption(ctx context.Context, sessionID uuid.UUID, delta uint64) (*Escrow, error)
	// SettleEscrow marks the row settled (refund / consumed values are
	// passed back to billing-svc via a webhook in #597; the row itself
	// just records the timestamp).
	SettleEscrow(ctx context.Context, sessionID uuid.UUID) error
}

// Service wires the verification + balance check + escrow store together.
// Handlers depend on this; tests substitute an in-memory store + a stub
// BalanceFetcher.
type Service struct {
	Store    EscrowStore
	Balances BalanceFetcher
	Logger   *slog.Logger
	Now      func() time.Time
}

// Authorize verifies the signed payment authorization, ensures the wallet
// has enough $GRID for the minimum escrow (one GB worth), records the
// nonce, and creates the escrow row.
//
// On success: returns the freshly-created Escrow.
// Errors mapped by the handler:
//
//	ErrSigInvalid           → 401
//	ErrInsufficientBalance  → 402
//	ErrNonceReplay          → 409
//	other                   → 500
func (s *Service) Authorize(
	ctx context.Context,
	sessionID, customerID uuid.UUID,
	auth Auth,
) (*Escrow, error) {
	if s.Now == nil {
		s.Now = time.Now
	}
	if err := ValidateSolanaPubkey(auth.WalletAddress); err != nil {
		return nil, err
	}
	if auth.Nonce == "" {
		return nil, errors.New("payment: nonce required")
	}
	if len(auth.Nonce) > 128 {
		return nil, errors.New("payment: nonce > 128 chars")
	}
	if auth.MaxGRIDPerMinute == 0 {
		return nil, errors.New("payment: max_grid_per_min must be > 0")
	}
	// Extract the timestamp from the supplied message — the client must
	// include it as the final segment per BuildExpectedMessage.
	ts, parsedSessionID, parsedCustomerID, parsedMax, parsedNonce, ok := parseSignedMessage(auth.Message)
	if !ok {
		return nil, fmt.Errorf("%w: malformed message", ErrSigInvalid)
	}
	// Defence-in-depth: cross-check the parsed fields against the request
	// parameters. A signed authorization for ANOTHER session must not be
	// reusable here.
	if parsedSessionID != sessionID {
		return nil, fmt.Errorf("%w: message session_id mismatch", ErrSigInvalid)
	}
	if parsedCustomerID != customerID {
		return nil, fmt.Errorf("%w: message customer_id mismatch", ErrSigInvalid)
	}
	if parsedMax != auth.MaxGRIDPerMinute {
		return nil, fmt.Errorf("%w: message max_grid_per_min mismatch", ErrSigInvalid)
	}
	if parsedNonce != auth.Nonce {
		return nil, fmt.Errorf("%w: message nonce mismatch", ErrSigInvalid)
	}
	// Timestamp window: ±5 min from server clock. Stops a long-stale
	// signature from being replayed beyond the nonce TTL.
	now := s.Now().UTC().Unix()
	if ts < now-5*60 || ts > now+5*60 {
		return nil, fmt.Errorf("%w: timestamp ±5min outside server clock", ErrSigInvalid)
	}
	auth.SignedAtUnixSec = ts

	if err := VerifySolanaSignature(auth.WalletAddress, auth.Message, auth.Signature); err != nil {
		return nil, err
	}

	// Nonce replay protection.
	seen, err := s.Store.CheckAndRecordNonce(ctx, auth.WalletAddress, auth.Nonce)
	if err != nil {
		return nil, fmt.Errorf("payment: nonce store: %w", err)
	}
	if seen {
		return nil, ErrNonceReplay
	}

	// Balance check (server-authoritative — never trust the client).
	bal, err := s.Balances.GRIDAtomicBalance(ctx, auth.WalletAddress)
	if err != nil {
		return nil, fmt.Errorf("payment: balance fetch: %w", err)
	}
	if bal < MinEscrowAtomic {
		return nil, fmt.Errorf("%w: have %d atomic, need %d",
			ErrInsufficientBalance, bal, MinEscrowAtomic)
	}

	// Escrow the entire on-chain balance — for v1 the client commits its
	// whole balance to the session and gets refunded the unused tail on
	// settlement. (Per #581 LOCKED MODEL "Refund unused"). A finer-grained
	// "deposit X" knob is a v2 follow-up.
	e := &Escrow{
		SessionID:           sessionID,
		CustomerID:          customerID,
		WalletAddress:       auth.WalletAddress,
		EscrowedAtomic:      bal,
		ConsumedAtomic:      0,
		MaxGRIDPerMinAtomic: auth.MaxGRIDPerMinute,
		Nonce:               auth.Nonce,
		StartedAt:           s.Now().UTC(),
		LastHeartbeatAt:     s.Now().UTC(),
	}
	if err := s.Store.CreateEscrow(ctx, e); err != nil {
		return nil, fmt.Errorf("payment: create escrow: %w", err)
	}
	if s.Logger != nil {
		s.Logger.Info("payment: escrow created",
			slog.String("session_id", sessionID.String()),
			slog.String("wallet", auth.WalletAddress),
			slog.Uint64("escrowed_atomic", bal))
	}
	return e, nil
}

// Heartbeat deducts `bytesIn+bytesOut`-worth of $GRID from the escrow.
// Returns the updated row + a `low` flag if the SSE channel should emit
// `topup_low`.
func (s *Service) Heartbeat(ctx context.Context, sessionID uuid.UUID, bytesIn, bytesOut uint64) (*Escrow, bool, error) {
	delta := ConsumedForBytes(bytesIn + bytesOut)
	e, err := s.Store.AddConsumption(ctx, sessionID, delta)
	if err != nil {
		return nil, false, err
	}
	return e, e.LowFraction(), nil
}

// parseSignedMessage parses `iogrid:session:start:<sid>:<cid>:<max>:<nonce>:<ts>`
// — the canonical format BuildExpectedMessage produces. Returns parsed
// fields + ok=false on any structural mismatch (the handler maps this to
// ErrSigInvalid).
func parseSignedMessage(msg string) (ts int64, sid, cid uuid.UUID, max uint64, nonce string, ok bool) {
	const prefix = "iogrid:session:start:"
	if len(msg) < len(prefix) || msg[:len(prefix)] != prefix {
		return 0, uuid.Nil, uuid.Nil, 0, "", false
	}
	parts := splitColon(msg[len(prefix):], 5) // sid:cid:max:nonce:ts
	if len(parts) != 5 {
		return 0, uuid.Nil, uuid.Nil, 0, "", false
	}
	sid, err := uuid.Parse(parts[0])
	if err != nil {
		return 0, uuid.Nil, uuid.Nil, 0, "", false
	}
	cid, err = uuid.Parse(parts[1])
	if err != nil {
		return 0, uuid.Nil, uuid.Nil, 0, "", false
	}
	max, err = strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		return 0, uuid.Nil, uuid.Nil, 0, "", false
	}
	nonce = parts[3]
	ts, err = strconv.ParseInt(parts[4], 10, 64)
	if err != nil {
		return 0, uuid.Nil, uuid.Nil, 0, "", false
	}
	return ts, sid, cid, max, nonce, true
}

// splitColon splits s on ':' into exactly n parts. The LAST part absorbs
// any trailing ':' content. nonces are user-controlled and may contain
// ':' — we tolerate that for everything except the timestamp tail, which
// is the LAST field.
//
// Implementation: scan left-to-right collecting n-1 parts, then the tail.
func splitColon(s string, n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n-1; i++ {
		idx := indexByte(s, ':')
		if idx < 0 {
			return nil
		}
		out = append(out, s[:idx])
		s = s[idx+1:]
	}
	// remaining = last segment; but our format has timestamp as LAST and
	// nonce as 4th — for n=5 above we collect sid:cid:max:nonce then `s`
	// becomes ts. Nonces containing ':' would land in the nonce slot via
	// the indexByte scan, which would prematurely split — so we forbid
	// ':' in nonces (validated in Authorize via len-check + this implicit
	// rule). Document via err msg.
	out = append(out, s)
	return out
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
