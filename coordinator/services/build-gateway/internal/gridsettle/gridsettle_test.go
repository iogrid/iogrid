package gridsettle

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBillableToAtomic(t *testing.T) {
	cases := []struct {
		min  int64
		rate uint64
		want uint64
	}{
		{0, DefaultRatePerMinuteAtomic, 0},
		{-3, DefaultRatePerMinuteAtomic, 0},
		{1, DefaultRatePerMinuteAtomic, 500_000_000},    // 0.5 GRID
		{10, DefaultRatePerMinuteAtomic, 5_000_000_000}, // 5 GRID
		{4, GridDecimals, 4_000_000_000},                // 1 GRID/min × 4
		{2_000_000, 1, 1_000_000},                       // saturates at 1e6 min
	}
	for _, c := range cases {
		if got := BillableToAtomic(c.min, c.rate); got != c.want {
			t.Errorf("BillableToAtomic(%d,%d)=%d want %d", c.min, c.rate, got, c.want)
		}
	}
}

func TestHTTPSettler_PostsBuildEnd(t *testing.T) {
	var got BuildSettleInput
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/grid/build-end" {
			t.Errorf("path = %q, want /v1/grid/build-end", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := &HTTPSettler{BaseURL: srv.URL}
	in := BuildSettleInput{
		BuildID: "b1", AttemptID: "a1",
		CustomerWallet: "Cust111", ConsumedAtomic: 5_000_000_000,
	}
	if err := s.SettleBuild(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if got.BuildID != "b1" || got.CustomerWallet != "Cust111" || got.ConsumedAtomic != 5_000_000_000 {
		t.Fatalf("billing-svc received %+v", got)
	}
}

func TestHTTPSettler_NoWalletIsNoop(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := &HTTPSettler{BaseURL: srv.URL}
	// No customer wallet (the #718 binding isn't resolved) → must NOT call
	// billing-svc and must NOT error (don't break the status transition).
	if err := s.SettleBuild(context.Background(), BuildSettleInput{BuildID: "b1"}); err != nil {
		t.Fatalf("no-wallet settle should be a no-op, got %v", err)
	}
	if called {
		t.Fatal("billing-svc was called despite an empty wallet")
	}
}

func TestHTTPWalletResolver(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-Token") != "tok" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.URL.Path == "/internal/v1/users/u1/wallet" {
			_, _ = w.Write([]byte(`{"wallet_address":"Wallet111","wallet_provider":"phantom"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound) // unbound user
	}))
	defer srv.Close()
	r := &HTTPWalletResolver{IdentityURL: srv.URL, Token: "tok"}

	got, err := r.ResolveWallet(context.Background(), "u1")
	if err != nil || got != "Wallet111" {
		t.Fatalf("bound: got %q err %v, want Wallet111", got, err)
	}
	// unbound user → "" no error (settle no-ops)
	got, err = r.ResolveWallet(context.Background(), "u2")
	if err != nil || got != "" {
		t.Fatalf("unbound: got %q err %v, want empty", got, err)
	}
}
