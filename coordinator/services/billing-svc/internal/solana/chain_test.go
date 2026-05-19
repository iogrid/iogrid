package solana

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestIsEphemeralRPCErr covers the retry classification table. Adding a new
// matcher? Add a row here first.
func TestIsEphemeralRPCErr(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{errors.New("blockhash not found"), true},
		{errors.New("BLOCKHASH NOT FOUND"), true},
		{errors.New("dial tcp: i/o timeout"), true},
		{errors.New("context deadline exceeded"), true},
		{errors.New("connection reset by peer"), true},
		{errors.New("EOF"), true},
		{errors.New("HTTP 503: bad gateway"), true},
		{errors.New("HTTP 429: rate limit exceeded"), true},
		{errors.New("Node is behind by 200 slots"), true},
		{errors.New("invalid transaction signature"), false},
		{errors.New("insufficient funds for rent"), false},
		{nil, false},
	}
	for _, c := range cases {
		got := isEphemeralRPCErr(c.err)
		if got != c.want {
			t.Errorf("isEphemeralRPCErr(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

// TestBackoff_Bounded ensures the exponential backoff has a sane ceiling
// (so we don't sleep for hours after many retries).
func TestBackoff_Bounded(t *testing.T) {
	for i := 1; i <= 20; i++ {
		d := backoff(i)
		if d > 4*time.Second {
			t.Errorf("backoff(%d) = %v exceeds ceiling 4s", i, d)
		}
		if d < 250*time.Millisecond {
			t.Errorf("backoff(%d) = %v below floor 250ms", i, d)
		}
	}
}

// TestSleepCtx_CancelFires — sleepCtx returns false when ctx is cancelled.
func TestSleepCtx_CancelFires(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sleepCtx(ctx, 100*time.Millisecond) {
		t.Errorf("expected false on cancelled ctx")
	}
}
