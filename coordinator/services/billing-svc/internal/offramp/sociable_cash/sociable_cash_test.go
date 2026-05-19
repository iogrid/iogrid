package sociable_cash_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"testing"

	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/offramp"
	socash "github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/offramp/sociable_cash"
)

func newAdapter(t *testing.T, secret string) *socash.Adapter {
	t.Helper()
	a, err := socash.New(socash.Config{
		WebhookSecret: secret,
		BaseURL:       "https://cash.sociable.cloud",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

func TestAdapter_Name(t *testing.T) {
	if newAdapter(t, "x").Name() != "sociable-cash" {
		t.Fatalf("Name mismatch")
	}
}

func TestBuildRedirectURL_DocumentedContractShape(t *testing.T) {
	a := newAdapter(t, "x")
	out, err := a.BuildRedirectURL(offramp.OffRampRequest{
		RequestID:     "req-zzz",
		WalletAddress: "Sol111aaa",
		GridAmount:    1_500_000_000,
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
	if u.Host != "cash.sociable.cloud" {
		t.Errorf("host=%s", u.Host)
	}
	if u.Path != "/off-ramp" {
		t.Errorf("path=%s", u.Path)
	}
	q := u.Query()
	if q.Get("from") != "GRID" {
		t.Errorf("from=%s", q.Get("from"))
	}
	if q.Get("amount") != "1500000000" {
		t.Errorf("amount=%s", q.Get("amount"))
	}
	if q.Get("signer") != "Sol111aaa" {
		t.Errorf("signer=%s", q.Get("signer"))
	}
	if q.Get("ref") != "req-zzz" {
		t.Errorf("ref=%s", q.Get("ref"))
	}
	if q.Get("return_url") != "https://app.iogrid.org/provide/earnings" {
		t.Errorf("return_url=%s", q.Get("return_url"))
	}
	if q.Get("currency") != "USD" {
		t.Errorf("currency=%s", q.Get("currency"))
	}
}

func TestBuildRedirectURL_RejectsEmptyInputs(t *testing.T) {
	a := newAdapter(t, "x")
	cases := []offramp.OffRampRequest{
		{RequestID: ""},
		{RequestID: "x", WalletAddress: ""},
		{RequestID: "x", WalletAddress: "w", GridAmount: 0},
	}
	for i, c := range cases {
		if _, err := a.BuildRedirectURL(c); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}

func TestVerifyWebhookSignature_HMACHex(t *testing.T) {
	a := newAdapter(t, "shhh")
	body := []byte(`{"ref":"r","status":"completed"}`)
	mac := hmac.New(sha256.New, []byte("shhh"))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	if !a.VerifyWebhookSignature(body, sig) {
		t.Fatalf("expected sig to verify")
	}
	if a.VerifyWebhookSignature(body, "00") {
		t.Fatalf("expected bad sig to fail")
	}
}

func TestVerifyWebhookSignature_EmptySecretFailsClosed(t *testing.T) {
	a := newAdapter(t, "")
	body := []byte(`{"ref":"r"}`)
	if a.VerifyWebhookSignature(body, "deadbeef") {
		t.Fatalf("expected empty-secret adapter to reject")
	}
}

func TestParseWebhook_CompletedPayload(t *testing.T) {
	a := newAdapter(t, "x")
	body := []byte(`{
		"offramp_id":"cash-tx-456",
		"ref":"req-zzz",
		"provider_id":"user-9",
		"status":"completed",
		"grid_amount":"1.5",
		"fiat_amount":"150.00",
		"fiat_currency":"USD",
		"txn_signature":"sig-deadbeef",
		"completed_at":"2026-05-19T12:00:00Z"
	}`)
	got, err := a.ParseWebhook(body)
	if err != nil {
		t.Fatalf("ParseWebhook: %v", err)
	}
	if got.RequestID != "req-zzz" {
		t.Errorf("RequestID=%q", got.RequestID)
	}
	if got.ProviderID != "user-9" {
		t.Errorf("ProviderID=%q", got.ProviderID)
	}
	if got.Status != offramp.StatusCompleted {
		t.Errorf("Status=%q", got.Status)
	}
	if got.GridAmount != 1_500_000_000 {
		t.Errorf("GridAmount=%d want 1500000000", got.GridAmount)
	}
	if got.FiatAmount != "150.00" {
		t.Errorf("FiatAmount=%q", got.FiatAmount)
	}
	if got.FiatCurrency != "USD" {
		t.Errorf("FiatCurrency=%q", got.FiatCurrency)
	}
	if got.TxnSignature != "sig-deadbeef" {
		t.Errorf("TxnSignature=%q", got.TxnSignature)
	}
	if got.ProviderRefID != "cash-tx-456" {
		t.Errorf("ProviderRefID=%q", got.ProviderRefID)
	}
	if got.CompletedAt == nil || got.CompletedAt.Year() != 2026 {
		t.Errorf("CompletedAt=%v", got.CompletedAt)
	}
}

func TestParseWebhook_StatusNormalisation(t *testing.T) {
	a := newAdapter(t, "x")
	cases := map[string]string{
		"pending":     offramp.StatusPending,
		"swapping":    offramp.StatusSwapping,
		"swap":        offramp.StatusSwapping,
		"settling":    offramp.StatusOffRamping,
		"off-ramping": offramp.StatusOffRamping,
		"offramping":  offramp.StatusOffRamping,
		"completed":   offramp.StatusCompleted,
		"settled":     offramp.StatusCompleted,
		"failed":      offramp.StatusFailed,
		"error":       offramp.StatusFailed,
		"":            offramp.StatusPending,
		"bogus":       offramp.StatusPending,
	}
	for in, want := range cases {
		body := []byte(`{"ref":"r","status":"` + in + `"}`)
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
	a := newAdapter(t, "x")
	if _, err := a.ParseWebhook([]byte(`{"status":"pending"}`)); err == nil {
		t.Fatalf("expected missing ref to error")
	}
}
