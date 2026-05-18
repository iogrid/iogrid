package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/auth"
)

func TestLimiter_AllowAndExhaust(t *testing.T) {
	l := New(10, 3, time.Minute)
	now := time.Unix(0, 0)
	l.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		ok, _ := l.Allow("k1")
		if !ok {
			t.Fatalf("allow %d: want true", i)
		}
	}
	ok, retry := l.Allow("k1")
	if ok {
		t.Fatal("4th request must be rate-limited")
	}
	if retry <= 0 {
		t.Fatal("retry-after should be positive")
	}
}

func TestLimiter_RefillsOverTime(t *testing.T) {
	l := New(10, 1, time.Minute)
	now := time.Unix(0, 0)
	l.now = func() time.Time { return now }

	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("first should pass")
	}
	if ok, _ := l.Allow("k"); ok {
		t.Fatal("second should fail (burst=1, no refill yet)")
	}
	// 200ms passes — at 10/s that's 2 tokens credited but capped to burst=1.
	now = now.Add(200 * time.Millisecond)
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("after refill should succeed")
	}
}

func TestLimiter_PerKeyIsolation(t *testing.T) {
	l := New(10, 1, time.Minute)
	now := time.Unix(0, 0)
	l.now = func() time.Time { return now }

	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("a should pass once")
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("b is independent and should pass")
	}
}

func TestLimiter_Reap(t *testing.T) {
	l := New(10, 1, 10*time.Millisecond)
	now := time.Unix(0, 0)
	l.now = func() time.Time { return now }

	_, _ = l.Allow("temp")
	if l.Size() != 1 {
		t.Fatalf("size after allow = %d", l.Size())
	}
	now = now.Add(time.Second)
	l.Reap()
	if l.Size() != 0 {
		t.Fatalf("reap left buckets: %d", l.Size())
	}
}

func TestMiddleware_Authed429(t *testing.T) {
	authed := New(100, 1, time.Minute)
	anon := New(100, 100, time.Minute)
	now := time.Unix(0, 0)
	authed.now = func() time.Time { return now }
	anon.now = func() time.Time { return now }

	mw := Middleware(authed, anon)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))

	c := &auth.Claims{}
	c.Subject = uuid.NewString()

	r1 := httptest.NewRequest("GET", "/", nil).WithContext(auth.NewContextForTesting(httptest.NewRequest("GET", "/", nil).Context(), c))
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, r1)
	if w1.Code != 200 {
		t.Fatalf("first should pass, got %d", w1.Code)
	}
	r2 := httptest.NewRequest("GET", "/", nil).WithContext(auth.NewContextForTesting(httptest.NewRequest("GET", "/", nil).Context(), c))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, r2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("second should be 429, got %d", w2.Code)
	}
	if w2.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After header")
	}
}

func TestMiddleware_AnonByIP(t *testing.T) {
	authed := New(100, 100, time.Minute)
	anon := New(100, 1, time.Minute)
	now := time.Unix(0, 0)
	authed.now = func() time.Time { return now }
	anon.now = func() time.Time { return now }

	mw := Middleware(authed, anon)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))

	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.1:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("first anon should pass, got %d", w.Code)
	}
	r2 := httptest.NewRequest("GET", "/", nil)
	r2.RemoteAddr = "203.0.113.1:1234"
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, r2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("second anon should be 429, got %d", w2.Code)
	}
}
