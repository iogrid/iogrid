package ratelimit

import (
	"context"
	"testing"
	"time"
)

func TestMemoryLimiter_AllowsUpToMax(t *testing.T) {
	l := NewMemory()
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		ok, _, err := l.Allow(ctx, "k", 3, time.Minute)
		if err != nil || !ok {
			t.Fatalf("call %d: ok=%v err=%v", i, ok, err)
		}
	}
	ok, retry, err := l.Allow(ctx, "k", 3, time.Minute)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ok {
		t.Fatalf("4th call should have been blocked")
	}
	if retry <= 0 {
		t.Fatalf("retry-after should be > 0, got %v", retry)
	}
}

func TestMemoryLimiter_DifferentKeysIndependent(t *testing.T) {
	l := NewMemory()
	ctx := context.Background()
	ok1, _, _ := l.Allow(ctx, "alpha", 1, time.Minute)
	ok2, _, _ := l.Allow(ctx, "bravo", 1, time.Minute)
	if !ok1 || !ok2 {
		t.Fatalf("different keys should be independent")
	}
	ok3, _, _ := l.Allow(ctx, "alpha", 1, time.Minute)
	if ok3 {
		t.Fatalf("second alpha call should have been blocked")
	}
}

func TestMemoryLimiter_WindowExpires(t *testing.T) {
	l := NewMemory()
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		l.Allow(ctx, "k", 2, 10*time.Millisecond)
	}
	ok, _, _ := l.Allow(ctx, "k", 2, 10*time.Millisecond)
	if ok {
		t.Fatalf("should have been rate-limited before window expiry")
	}
	time.Sleep(20 * time.Millisecond)
	ok, _, _ = l.Allow(ctx, "k", 2, 10*time.Millisecond)
	if !ok {
		t.Fatalf("after window expiry should allow again")
	}
}

func TestNilLimiter_FailsOpen(t *testing.T) {
	var l *RedisLimiter
	ok, _, err := l.Allow(context.Background(), "k", 1, time.Minute)
	if err != nil {
		t.Fatalf("nil limiter error: %v", err)
	}
	if !ok {
		t.Fatalf("nil limiter should fail open")
	}
}
