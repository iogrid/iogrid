package solana

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/blocto/solana-go-sdk/common"
	"github.com/blocto/solana-go-sdk/types"

	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/config"
)

// TestQuoteRaw_HappyPath asserts that the raw quote path returns the parsed
// QuoteResponse intact (priceImpactPct preserved for slippage logging).
func TestQuoteRaw_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(QuoteResponse{
			InputMint:   USDCMint,
			OutputMint:  "GRIDmint",
			InAmount:    "1000000",
			OutAmount:   "42000000000",
			PriceImpact: "0.001",
			SlippageBps: 50,
		})
	}))
	defer srv.Close()
	jc := NewJupiterClient(srv.URL, srv.Client())
	q, err := jc.QuoteRaw(context.Background(), SwapRequest{
		InputMint: USDCMint, OutputMint: "GRIDmint", Amount: 1_000_000, SlippageBps: 50,
	})
	if err != nil {
		t.Fatalf("QuoteRaw: %v", err)
	}
	if q.OutAmount != "42000000000" {
		t.Errorf("OutAmount = %q", q.OutAmount)
	}
	if q.PriceImpact != "0.001" {
		t.Errorf("PriceImpact = %q", q.PriceImpact)
	}
}

// TestSwapTransaction_RoundtripBase64 asserts the JupiterClient correctly
// surfaces the base64-encoded tx body returned by /v6/swap. We can't fully
// roundtrip a real Solana tx without a working blockhash; we just verify
// the wire shape.
func TestSwapTransaction_RoundtripBase64(t *testing.T) {
	want := base64.StdEncoding.EncodeToString([]byte("hello-swap-tx-bytes"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/swap" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(jupSwapResp{SwapTransaction: want, LastValidBlockHeight: 100})
	}))
	defer srv.Close()
	jc := NewJupiterClient(srv.URL, srv.Client())
	user := common.PublicKeyFromString("11111111111111111111111111111112")
	got, err := jc.SwapTransaction(context.Background(), &QuoteResponse{OutAmount: "1"}, user)
	if err != nil {
		t.Fatalf("SwapTransaction: %v", err)
	}
	if got != want {
		t.Errorf("SwapTransaction = %q, want %q", got, want)
	}
}

// TestSwapTransaction_EmptyResponseRejected — defensive: Jupiter sometimes
// returns 200 with `swapTransaction:""` when liquidity dries up.
func TestSwapTransaction_EmptyResponseRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jupSwapResp{})
	}))
	defer srv.Close()
	jc := NewJupiterClient(srv.URL, srv.Client())
	_, err := jc.SwapTransaction(context.Background(), &QuoteResponse{OutAmount: "1"},
		common.PublicKeyFromString("11111111111111111111111111111112"))
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected empty-response error, got %v", err)
	}
}

// TestMakeQuoteURL — small but useful: the URL builder ordering matters
// because Jupiter's CDN keys on the raw query string.
func TestMakeQuoteURL(t *testing.T) {
	got, err := makeQuoteURL("https://x.test/v6", SwapRequest{
		InputMint:   "A",
		OutputMint:  "B",
		Amount:      1234,
		SlippageBps: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "https://x.test/v6/quote?inputMint=A&outputMint=B&amount=1234&slippageBps=25&swapMode=ExactIn"
	if got != want {
		t.Errorf("URL = %q\nwant %q", got, want)
	}
}

// TestSwapTransaction_Deserialize confirms a Jupiter-shaped tx (built
// locally) round-trips through TransactionDeserialize. Ensures our
// deserialise + AddSignature path doesn't bit-rot when the SDK upgrades.
func TestSwapTransaction_DeserializeRoundtrip(t *testing.T) {
	acct := types.NewAccount()
	msg := types.NewMessage(types.NewMessageParam{
		FeePayer:        acct.PublicKey,
		RecentBlockhash: "11111111111111111111111111111111",
		Instructions: []types.Instruction{
			{ProgramID: common.PublicKeyFromString("11111111111111111111111111111112"),
				Accounts: []types.AccountMeta{{PubKey: acct.PublicKey, IsSigner: true, IsWritable: true}},
				Data:     []byte{0}},
		},
	})
	tx, err := types.NewTransaction(types.NewTransactionParam{
		Message: msg,
		Signers: []types.Account{acct},
	})
	if err != nil {
		t.Fatalf("NewTransaction: %v", err)
	}
	raw, err := tx.Serialize()
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	round, err := types.TransactionDeserialize(raw)
	if err != nil {
		t.Fatalf("Deserialize: %v", err)
	}
	if round.Message.Header.NumRequireSignatures != 1 {
		t.Errorf("NumRequireSignatures = %d", round.Message.Header.NumRequireSignatures)
	}
}

// ── helpers ───────────────────────────────────────────────────────

func testConfig(mint string) *config.Config {
	return &config.Config{
		GRIDTokenMint:       mint,
		GRIDTokenProgram:    "token-2022",
		SolanaHotWalletPath: "", // stub
		JupiterAPIURL:       "http://does-not-matter",
		BurnPercentage:      2.0,
		IncineratorAddress:  "1nc1nerator11111111111111111111111111111111",
	}
}
