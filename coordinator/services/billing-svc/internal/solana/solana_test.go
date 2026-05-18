package solana

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/config"
)

// TestJupiterQuoteUSDCToGRID exercises the swap-decision branches that
// don't need a real Solana RPC connection. Uses httptest as the swap
// quote source.
func TestJupiterQuoteUSDCToGRID_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/quote" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		q := r.URL.Query()
		if got := q.Get("inputMint"); got != USDCMint {
			t.Errorf("inputMint = %q", got)
		}
		if got := q.Get("outputMint"); got != "GRIDmint" {
			t.Errorf("outputMint = %q", got)
		}
		amt, _ := strconv.ParseInt(q.Get("amount"), 10, 64)
		// 100 cents = $1.00 = 1_000_000 USDC atomic
		if amt != 1_000_000 {
			t.Errorf("amount = %d, want 1_000_000", amt)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(QuoteResponse{
			InputMint:   USDCMint,
			OutputMint:  "GRIDmint",
			InAmount:    "1000000",
			OutAmount:   "42000000000", // 42 $GRID (9 decimals)
			SlippageBps: 50,
		})
	}))
	defer srv.Close()

	jc := NewJupiterClient(srv.URL, srv.Client())
	out, err := jc.QuoteUSDCToGRID(context.Background(), 100, "GRIDmint")
	if err != nil {
		t.Fatalf("QuoteUSDCToGRID: %v", err)
	}
	if out != 42_000_000_000 {
		t.Errorf("out = %d, want 42_000_000_000", out)
	}
}

func TestJupiterQuoteUSDCToGRID_ZeroIsNoop(t *testing.T) {
	jc := NewJupiterClient("http://does-not-matter", http.DefaultClient)
	out, err := jc.QuoteUSDCToGRID(context.Background(), 0, "GRIDmint")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out != 0 {
		t.Errorf("out = %d, want 0", out)
	}
}

func TestJupiterQuoteUSDCToGRID_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"slippage too tight"}`))
	}))
	defer srv.Close()
	jc := NewJupiterClient(srv.URL, srv.Client())
	_, err := jc.QuoteUSDCToGRID(context.Background(), 1000, "GRIDmint")
	if err == nil {
		t.Fatalf("expected error on HTTP 400")
	}
}

// TestServiceEnabled covers both stub and live evaluation. We don't
// load a real keypair here because that requires file IO; the live
// path is exercised in the integration test.
func TestServiceEnabled_StubMode(t *testing.T) {
	cfg := &config.Config{
		GRIDTokenMint:       "", // empty → stub
		SolanaHotWalletPath: "",
		BurnPercentage:      2,
		IncineratorAddress:  "1nc1nerator11111111111111111111111111111111",
		JupiterAPIURL:       "https://does-not-matter",
	}
	svc, err := New(cfg, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if svc.Enabled() {
		t.Errorf("expected stub mode")
	}
	if svc.WalletAddress() != "" {
		t.Errorf("WalletAddress should be empty in stub mode")
	}
}

// TestBurnDecision_SkippedWhenDisabled verifies the stub-mode branch
// of evaluateBurn returns Skipped without making an HTTP call.
func TestBurnDecision_SkippedWhenDisabled(t *testing.T) {
	cfg := &config.Config{
		JupiterAPIURL:      "http://will.not.be.dialed",
		IncineratorAddress: "1nc1nerator11111111111111111111111111111111",
	}
	svc, err := New(cfg, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	dec, err := svc.evaluateBurn(context.Background(), 12345, time.Now(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("evaluateBurn: %v", err)
	}
	if !dec.Skipped {
		t.Errorf("expected Skipped=true in stub mode")
	}
	if dec.SkipReason == "" {
		t.Errorf("expected non-empty SkipReason")
	}
	if dec.GRIDLamports != 0 {
		t.Errorf("expected 0 lamports, got %d", dec.GRIDLamports)
	}
}
