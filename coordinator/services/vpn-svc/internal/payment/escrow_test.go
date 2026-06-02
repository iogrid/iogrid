package payment

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mr-tron/base58"
)

// ── ConsumedForBytes ────────────────────────────────────────────────

func TestConsumedForBytes_KnownVectors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		bytes uint64
		want  uint64
	}{
		{"zero", 0, 0},
		{"one_byte", 1, 0}, // truncation toward zero
		{"one_gb_decimal", 1_000_000_000, GRIDPerGBAtomic},
		{"half_gb", 500_000_000, GRIDPerGBAtomic / 2},
		{"hundred_gb", 100 * 1_000_000_000, 100 * GRIDPerGBAtomic},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ConsumedForBytes(c.bytes); got != c.want {
				t.Fatalf("bytes=%d: got=%d want=%d", c.bytes, got, c.want)
			}
		})
	}
}

// ── Escrow arithmetic ───────────────────────────────────────────────

func TestEscrow_RemainingAndLowFraction(t *testing.T) {
	t.Parallel()
	e := &Escrow{EscrowedAtomic: 1_000_000_000, ConsumedAtomic: 0}
	if e.Remaining() != 1_000_000_000 {
		t.Fatalf("Remaining: got %d", e.Remaining())
	}
	if e.LowFraction() {
		t.Fatal("LowFraction should be false at 100%")
	}
	e.ConsumedAtomic = 800_000_000
	if e.LowFraction() {
		t.Fatal("LowFraction should be false at 20% remaining")
	}
	e.ConsumedAtomic = 950_000_000 // 5% remaining
	if !e.LowFraction() {
		t.Fatal("LowFraction should be true at 5% remaining")
	}
	e.ConsumedAtomic = 1_000_000_000 // 0% remaining
	if !e.LowFraction() {
		t.Fatal("LowFraction should be true at exhausted")
	}
}

// ── Signature verify ────────────────────────────────────────────────

func TestVerifySolanaSignature_RoundTrip(t *testing.T) {
	t.Parallel()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	msg := "iogrid:session:start:test"
	sig := ed25519.Sign(priv, []byte(msg))
	walletB58 := base58.Encode(pub)
	sigB58 := base58.Encode(sig)

	if err := VerifySolanaSignature(walletB58, msg, sigB58); err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	// Tampered message must fail.
	if err := VerifySolanaSignature(walletB58, msg+" tamper", sigB58); !errors.Is(err, ErrSigInvalid) {
		t.Fatalf("expected ErrSigInvalid for tampered msg, got %v", err)
	}
	// Wrong wallet must fail.
	pub2, _, _ := ed25519.GenerateKey(rand.Reader)
	if err := VerifySolanaSignature(base58.Encode(pub2), msg, sigB58); !errors.Is(err, ErrSigInvalid) {
		t.Fatalf("expected ErrSigInvalid for wrong wallet, got %v", err)
	}
	// Malformed signature length.
	if err := VerifySolanaSignature(walletB58, msg, base58.Encode([]byte{1, 2, 3})); !errors.Is(err, ErrSigInvalid) {
		t.Fatalf("expected ErrSigInvalid for short sig, got %v", err)
	}
}

// ── parseSignedMessage ──────────────────────────────────────────────

func TestParseSignedMessage(t *testing.T) {
	t.Parallel()
	sid := uuid.New()
	cid := uuid.New()
	msg := BuildExpectedMessage(sid, cid, 1234, "nonce-abc", 1700000000)
	ts, gotSid, gotCid, gotMax, gotNonce, ok := parseSignedMessage(msg)
	if !ok {
		t.Fatal("parseSignedMessage returned !ok")
	}
	if ts != 1700000000 {
		t.Fatalf("ts: got %d", ts)
	}
	if gotSid != sid {
		t.Fatalf("sid mismatch")
	}
	if gotCid != cid {
		t.Fatalf("cid mismatch")
	}
	if gotMax != 1234 {
		t.Fatalf("max mismatch: %d", gotMax)
	}
	if gotNonce != "nonce-abc" {
		t.Fatalf("nonce mismatch: %q", gotNonce)
	}
	// Bad prefix.
	if _, _, _, _, _, ok := parseSignedMessage("wrong:prefix:" + msg); ok {
		t.Fatal("expected !ok for wrong prefix")
	}
	// Truncated.
	short := strings.Join(strings.Split(msg, ":")[:5], ":")
	if _, _, _, _, _, ok := parseSignedMessage(short); ok {
		t.Fatal("expected !ok for truncated msg")
	}
}

// ── Service.Authorize end-to-end (fakes) ────────────────────────────

type fakeBalances struct {
	mu      sync.Mutex
	balance uint64
	err     error
	calls   int
}

func (f *fakeBalances) GRIDAtomicBalance(ctx context.Context, wallet string) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.balance, f.err
}

type fakeStore struct {
	mu       sync.Mutex
	nonces   map[string]bool
	escrows  map[uuid.UUID]*Escrow
	failCheck bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		nonces:  make(map[string]bool),
		escrows: make(map[uuid.UUID]*Escrow),
	}
}
func (f *fakeStore) CheckAndRecordNonce(ctx context.Context, w, n string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := w + "|" + n
	if f.nonces[k] {
		return true, nil
	}
	f.nonces[k] = true
	return false, nil
}
func (f *fakeStore) CleanupNonces(ctx context.Context) (int, error) { return 0, nil }
func (f *fakeStore) CreateEscrow(ctx context.Context, e *Escrow) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	clone := *e
	f.escrows[e.SessionID] = &clone
	return nil
}
func (f *fakeStore) GetEscrow(ctx context.Context, sid uuid.UUID) (*Escrow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e, ok := f.escrows[sid]; ok {
		clone := *e
		return &clone, nil
	}
	return nil, errors.New("not found")
}
func (f *fakeStore) AddConsumption(ctx context.Context, sid uuid.UUID, delta uint64) (*Escrow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.escrows[sid]
	if !ok {
		return nil, errors.New("not found")
	}
	if e.ConsumedAtomic+delta > e.EscrowedAtomic {
		return nil, ErrEscrowExhausted
	}
	e.ConsumedAtomic += delta
	e.LastHeartbeatAt = time.Now()
	clone := *e
	return &clone, nil
}
func (f *fakeStore) SettleEscrow(ctx context.Context, sid uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e, ok := f.escrows[sid]; ok {
		t := time.Now()
		e.SettledAt = &t
	}
	return nil
}

func mintAuth(t *testing.T, sid, cid uuid.UUID, nonce string, ts int64) (Auth, ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	msg := BuildExpectedMessage(sid, cid, 100_000, nonce, ts)
	sig := ed25519.Sign(priv, []byte(msg))
	return Auth{
		WalletAddress:    base58.Encode(pub),
		Signature:        base58.Encode(sig),
		Message:          msg,
		Nonce:            nonce,
		MaxGRIDPerMinute: 100_000,
	}, pub
}

func TestService_Authorize_Happy(t *testing.T) {
	t.Parallel()
	sid := uuid.New()
	cid := uuid.New()
	auth, _ := mintAuth(t, sid, cid, "nonce1", time.Now().Unix())
	svc := &Service{
		Store:    newFakeStore(),
		Balances: &fakeBalances{balance: 50_000_000}, // 0.05 GRID
	}
	e, err := svc.Authorize(context.Background(), sid, cid, auth)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if e.EscrowedAtomic != 50_000_000 {
		t.Fatalf("escrowed: %d", e.EscrowedAtomic)
	}
	if e.WalletAddress != auth.WalletAddress {
		t.Fatalf("wallet mismatch")
	}
}

func TestService_Authorize_InsufficientBalance(t *testing.T) {
	t.Parallel()
	sid, cid := uuid.New(), uuid.New()
	auth, _ := mintAuth(t, sid, cid, "n", time.Now().Unix())
	svc := &Service{Store: newFakeStore(), Balances: &fakeBalances{balance: 100}} // < MinEscrow
	_, err := svc.Authorize(context.Background(), sid, cid, auth)
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("expected ErrInsufficientBalance, got %v", err)
	}
}

func TestService_Authorize_NonceReplay(t *testing.T) {
	t.Parallel()
	sid, cid := uuid.New(), uuid.New()
	auth, _ := mintAuth(t, sid, cid, "samenonce", time.Now().Unix())
	st := newFakeStore()
	svc := &Service{Store: st, Balances: &fakeBalances{balance: MinEscrowAtomic}}
	if _, err := svc.Authorize(context.Background(), sid, cid, auth); err != nil {
		t.Fatalf("1st authorize failed: %v", err)
	}
	// Replay with same nonce + a NEW session id (the signature now mis-matches
	// the parsed sid in the message — so first we'd hit ErrSigInvalid). To
	// actually exercise the nonce-replay path, re-use the exact same auth
	// against the SAME session id (e.g. retry after a 5xx).
	_, err := svc.Authorize(context.Background(), sid, cid, auth)
	if !errors.Is(err, ErrNonceReplay) {
		t.Fatalf("expected ErrNonceReplay, got %v", err)
	}
}

func TestService_Authorize_TimestampOutOfWindow(t *testing.T) {
	t.Parallel()
	sid, cid := uuid.New(), uuid.New()
	auth, _ := mintAuth(t, sid, cid, "ts", time.Now().Add(-1*time.Hour).Unix())
	svc := &Service{Store: newFakeStore(), Balances: &fakeBalances{balance: MinEscrowAtomic}}
	_, err := svc.Authorize(context.Background(), sid, cid, auth)
	if !errors.Is(err, ErrSigInvalid) {
		t.Fatalf("expected ErrSigInvalid (timestamp window), got %v", err)
	}
}

func TestService_Authorize_BadSig(t *testing.T) {
	t.Parallel()
	sid, cid := uuid.New(), uuid.New()
	auth, _ := mintAuth(t, sid, cid, "ts", time.Now().Unix())
	// Tamper with the message AFTER signing — server rebuilds expected
	// shape but the supplied signature won't verify.
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	auth.WalletAddress = base58.Encode(pub)
	svc := &Service{Store: newFakeStore(), Balances: &fakeBalances{balance: MinEscrowAtomic}}
	_, err := svc.Authorize(context.Background(), sid, cid, auth)
	if !errors.Is(err, ErrSigInvalid) {
		t.Fatalf("expected ErrSigInvalid (wrong wallet), got %v", err)
	}
}

func TestService_Heartbeat_DeductsAndExhausts(t *testing.T) {
	t.Parallel()
	sid, cid := uuid.New(), uuid.New()
	auth, _ := mintAuth(t, sid, cid, "hb", time.Now().Unix())
	svc := &Service{
		Store:    newFakeStore(),
		Balances: &fakeBalances{balance: 2_000_000}, // 0.002 GRID = 2 GB worth
	}
	if _, err := svc.Authorize(context.Background(), sid, cid, auth); err != nil {
		t.Fatalf("authorize: %v", err)
	}
	// Consume 1 GB worth — should still be ok, but at 50% (not low).
	e, low, err := svc.Heartbeat(context.Background(), sid, 500_000_000, 500_000_000)
	if err != nil {
		t.Fatalf("hb1: %v", err)
	}
	if e.ConsumedAtomic != GRIDPerGBAtomic {
		t.Fatalf("consumed: %d, want %d", e.ConsumedAtomic, GRIDPerGBAtomic)
	}
	if low {
		t.Fatal("hb1: low=true at 50%")
	}
	// Consume another 0.95 GB — should now be at ~5% remaining → low=true.
	_, low, err = svc.Heartbeat(context.Background(), sid, 500_000_000, 450_000_000)
	if err != nil {
		t.Fatalf("hb2: %v", err)
	}
	if !low {
		t.Fatal("hb2: expected low=true at ~5% remaining")
	}
	// Final heartbeat that exhausts: 1 GB more (cumulative 2.95 GB > escrowed
	// 2 GB worth). Expect ErrEscrowExhausted.
	_, _, err = svc.Heartbeat(context.Background(), sid, 1_000_000_000, 0)
	if !errors.Is(err, ErrEscrowExhausted) {
		t.Fatalf("expected ErrEscrowExhausted, got %v", err)
	}
}
