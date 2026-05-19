package moonpay_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/offramp"
	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/offramp/moonpay"
)

func newAdapter(t *testing.T) *moonpay.Adapter {
	t.Helper()
	a, err := moonpay.New(moonpay.Config{
		APIKey:        "pk_test_abc",
		WebhookSecret: "shhh",
		BaseURL:       "https://sell.moonpay.com",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

func TestNew_RequiresAPIKey(t *testing.T) {
	if _, err := moonpay.New(moonpay.Config{WebhookSecret: "x"}); err == nil {
		t.Fatalf("expected error when APIKey empty")
	}
}

func TestNew_RequiresWebhookSecret(t *testing.T) {
	if _, err := moonpay.New(moonpay.Config{APIKey: "x"}); err == nil {
		t.Fatalf("expected error when WebhookSecret empty")
	}
}

func TestAdapter_Name(t *testing.T) {
	if newAdapter(t).Name() != "moonpay" {
		t.Fatalf("Name mismatch")
	}
}

func TestBuildRedirectURL_ContainsRequiredParams(t *testing.T) {
	a := newAdapter(t)
	out, err := a.BuildRedirectURL(offramp.OffRampRequest{
		RequestID:     "req-abc",
		ProviderID:    "user-123",
		WalletAddress: "Sol111aaa",
		GridAmount:    1_500_000_000, // 1.5 GRID
		ReturnURL:     "https://app.iogrid.org/provide/earnings",
		FiatCurrency:  "USD",
	})
	if err != nil {
		t.Fatalf("BuildRedirectURL: %v", err)
	}
	u, err := url.Parse(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	q := u.Query()
	if got := q.Get("apiKey"); got != "pk_test_abc" {
		t.Errorf("apiKey=%q", got)
	}
	if got := q.Get("baseCurrencyAmount"); got != "1.5" {
		t.Errorf("baseCurrencyAmount=%q want '1.5'", got)
	}
	if got := q.Get("quoteCurrencyCode"); got != "usd" {
		t.Errorf("quoteCurrencyCode=%q want 'usd'", got)
	}
	if got := q.Get("refundWalletAddress"); got != "Sol111aaa" {
		t.Errorf("refundWalletAddress=%q", got)
	}
	if got := q.Get("externalCustomerId"); got != "user-123" {
		t.Errorf("externalCustomerId=%q", got)
	}
	if got := q.Get("externalTransactionId"); got != "req-abc" {
		t.Errorf("externalTransactionId=%q", got)
	}
	if got := q.Get("redirectURL"); got != "https://app.iogrid.org/provide/earnings" {
		t.Errorf("redirectURL=%q", got)
	}
	if q.Get("signature") == "" {
		t.Errorf("signature missing")
	}
}

func TestBuildRedirectURL_SignatureRoundTrips(t *testing.T) {
	a := newAdapter(t)
	out, err := a.BuildRedirectURL(offramp.OffRampRequest{
		RequestID:     "req-abc",
		ProviderID:    "user-123",
		WalletAddress: "Sol111aaa",
		GridAmount:    1_000_000_000,
		ReturnURL:     "https://app.iogrid.org/provide/earnings",
		FiatCurrency:  "USD",
	})
	if err != nil {
		t.Fatalf("BuildRedirectURL: %v", err)
	}
	idx := strings.Index(out, "?")
	if idx < 0 {
		t.Fatalf("missing ?: %s", out)
	}
	qs := out[idx+1:]
	sigIdx := strings.Index(qs, "&signature=")
	if sigIdx < 0 {
		t.Fatalf("no signature param: %s", qs)
	}
	signed := qs[:sigIdx]
	sigEsc := strings.TrimPrefix(qs[sigIdx:], "&signature=")
	sig, err := url.QueryUnescape(sigEsc)
	if err != nil {
		t.Fatalf("unescape sig: %v", err)
	}
	if !a.VerifyRedirectSignature(signed, sig) {
		t.Fatalf("signature failed to verify")
	}
}

func TestBuildRedirectURL_RejectsEmptyInputs(t *testing.T) {
	a := newAdapter(t)
	cases := []offramp.OffRampRequest{
		{RequestID: ""},
		{RequestID: "x", WalletAddress: ""},
		{RequestID: "x", WalletAddress: "w", GridAmount: 0},
	}
	for i, c := range cases {
		if _, err := a.BuildRedirectURL(c); err == nil {
			t.Errorf("case %d: expected error, got nil", i)
		}
	}
}

func TestVerifyWebhookSignature_BareHex(t *testing.T) {
	a := newAdapter(t)
	body := []byte(`{"type":"transaction_updated"}`)
	mac := hmac.New(sha256.New, []byte("shhh"))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	if !a.VerifyWebhookSignature(body, sig) {
		t.Fatalf("expected bare-hex sig to verify")
	}
	if a.VerifyWebhookSignature(body, "00") {
		t.Fatalf("expected bad sig to fail")
	}
	if a.VerifyWebhookSignature(nil, sig) {
		t.Fatalf("expected empty body to fail")
	}
}

func TestVerifyWebhookSignature_TimestampedHeader(t *testing.T) {
	a := newAdapter(t)
	body := []byte(`{"type":"transaction_updated"}`)
	ts := "1716000000"
	signed := []byte(ts + "." + string(body))
	mac := hmac.New(sha256.New, []byte("shhh"))
	mac.Write(signed)
	sig := hex.EncodeToString(mac.Sum(nil))
	header := "t=" + ts + ",s=" + sig

	if !a.VerifyWebhookSignature(body, header) {
		t.Fatalf("expected timestamped header to verify")
	}
}

func TestParseWebhook_CompletedTransaction(t *testing.T) {
	a := newAdapter(t)
	body := []byte(`{
		"type":"transaction_updated",
		"data":{
			"id":"mp-tx-789",
			"externalTransactionId":"req-abc",
			"externalCustomerId":"user-123",
			"status":"completed",
			"baseCurrencyAmount":1.5,
			"quoteCurrencyAmount":150.00,
			"quoteCurrencyCode":"usd",
			"cryptoTransactionId":"sig-deadbeef",
			"updatedAt":"2026-05-19T12:00:00Z"
		}
	}`)
	got, err := a.ParseWebhook(body)
	if err != nil {
		t.Fatalf("ParseWebhook: %v", err)
	}
	if got.RequestID != "req-abc" {
		t.Errorf("RequestID=%q", got.RequestID)
	}
	if got.Status != offramp.StatusCompleted {
		t.Errorf("Status=%q want completed", got.Status)
	}
	if got.GridAmount != 1_500_000_000 {
		t.Errorf("GridAmount=%d want 1500000000", got.GridAmount)
	}
	if got.FiatAmount != "150.00" {
		t.Errorf("FiatAmount=%q want '150.00'", got.FiatAmount)
	}
	if got.FiatCurrency != "USD" {
		t.Errorf("FiatCurrency=%q want USD", got.FiatCurrency)
	}
	if got.TxnSignature != "sig-deadbeef" {
		t.Errorf("TxnSignature=%q", got.TxnSignature)
	}
	if got.ProviderRefID != "mp-tx-789" {
		t.Errorf("ProviderRefID=%q", got.ProviderRefID)
	}
	if got.CompletedAt == nil || got.CompletedAt.Year() != 2026 {
		t.Errorf("CompletedAt=%v", got.CompletedAt)
	}
	_ = time.Now
}

func TestParseWebhook_StatusMapping(t *testing.T) {
	a := newAdapter(t)
	cases := map[string]string{
		"completed":            offramp.StatusCompleted,
		"failed":               offramp.StatusFailed,
		"waitingForSwap":       offramp.StatusSwapping,
		"waitingForPayout":     offramp.StatusOffRamping,
		"pending":              offramp.StatusOffRamping,
		"waitingAuthorization": offramp.StatusSwapping,
		"unknown":              offramp.StatusPending,
	}
	for in, want := range cases {
		body := []byte(`{"data":{"externalTransactionId":"r","status":"` + in + `"}}`)
		got, err := a.ParseWebhook(body)
		if err != nil {
			t.Fatalf("ParseWebhook(%s): %v", in, err)
		}
		if got.Status != want {
			t.Errorf("status %q → %q, want %q", in, got.Status, want)
		}
	}
}

func TestParseWebhook_MissingRefRejected(t *testing.T) {
	a := newAdapter(t)
	if _, err := a.ParseWebhook([]byte(`{"data":{"status":"pending"}}`)); err == nil {
		t.Fatalf("expected missing externalTransactionId to error")
	}
}

func TestLamportRendering_TrimsTrailingZeros(t *testing.T) {
	a := newAdapter(t)
	out, err := a.BuildRedirectURL(offramp.OffRampRequest{
		RequestID: "r", ProviderID: "p", WalletAddress: "w",
		GridAmount: 1_000_000_000, FiatCurrency: "USD",
	})
	if err != nil {
		t.Fatalf("BuildRedirectURL: %v", err)
	}
	u, _ := url.Parse(out)
	if u.Query().Get("baseCurrencyAmount") != "1" {
		t.Errorf("baseCurrencyAmount=%q want '1'", u.Query().Get("baseCurrencyAmount"))
	}
}
