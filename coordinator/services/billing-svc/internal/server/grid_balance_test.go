package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestGridFromAtomic verifies the atomic→decimal $GRID rendering used by
// /v1/grid/balance (#632). $GRID has 9 decimals; the UI string keeps 4 dp.
func TestGridFromAtomic(t *testing.T) {
	cases := []struct {
		atomic uint64
		want   string
	}{
		{0, "0.0000"},
		{1_000_000_000, "1.0000"},     // exactly 1 GRID
		{1_500_000_000, "1.5000"},     // 1.5 GRID
		{100_000_000_000, "100.0000"}, // faucet drop
		{123_456_700_000, "123.4567"}, // 4-dp truncation
		{5_000_000, "0.0050"},         // sub-GRID dust (0.005 GRID)
		{500_000, "0.0005"},           // smaller dust
		{1, "0.0000"},                 // below 4dp precision → rounds to 0
	}
	for _, c := range cases {
		if got := gridFromAtomic(c.atomic); got != c.want {
			t.Errorf("gridFromAtomic(%d) = %q, want %q", c.atomic, got, c.want)
		}
	}
}

// TestHandleBalanceValidation covers the input-validation + stub-mode
// guards on /v1/grid/balance without a live Solana RPC. Anti-fake-state
// (#417): stub mode returns 503, never a fake zero balance.
func TestHandleBalanceValidation(t *testing.T) {
	r := chi.NewRouter()
	// Solana nil → stub mode; Store nil. mountGrid wires the route.
	mountGrid(r, &GridDeps{})
	srv := httptest.NewServer(r)
	defer srv.Close()

	t.Run("missing wallet → 400", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/v1/grid/balance")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("want 400, got %d", resp.StatusCode)
		}
	})

	t.Run("malformed wallet → 400", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/v1/grid/balance?wallet=not-a-pubkey")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("want 400, got %d", resp.StatusCode)
		}
	})

	t.Run("stub mode (no $GRID mint) → 503 not fake-zero", func(t *testing.T) {
		// A well-formed base58 pubkey (the system program id) so we get
		// past address validation and hit the stub-mode guard.
		resp, err := http.Get(srv.URL + "/v1/grid/balance?wallet=11111111111111111111111111111111")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("want 503 in stub mode, got %d", resp.StatusCode)
		}
	})
}
