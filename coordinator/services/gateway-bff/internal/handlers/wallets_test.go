// Tests for /api/v1/account/wallets surface (issue #326).
package handlers

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	identityv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/identity/v1"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/clients"
)

func TestListWallets_RequiresAuth(t *testing.T) {
	api := newAPI(t, &clients.Set{})
	r := httptest.NewRequest(http.MethodGet, "/api/v1/account/wallets", nil)
	w := httptest.NewRecorder()
	api.ListWallets(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestListWallets_ReturnsWalletEnvelope(t *testing.T) {
	called := false
	set := &clients.Set{
		Auth: &mockAuth{
			listBoundWallets: func(ctx context.Context, _ *identityv1.ListBoundWalletsRequest) (*identityv1.ListBoundWalletsResponse, error) {
				called = true
				if _, ok := clients.CallerClaims(ctx); !ok {
					t.Errorf("expected caller claims on outbound ctx")
				}
				return &identityv1.ListBoundWalletsResponse{
					Bindings: []*identityv1.WalletBinding{
						{
							Id:        &commonv1.UUID{Value: "11111111-1111-1111-1111-111111111111"},
							UserId:    &commonv1.UUID{Value: "22222222-2222-2222-2222-222222222222"},
							Address:   "9xQeWvG816bUx9EPjHmaT23yvVM2ZWbrrpZb9PusVFin",
							CreatedAt: timestamppb.Now(),
						},
					},
				}, nil
			},
		},
	}
	api := newAPI(t, set)
	r := withAuth(httptest.NewRequest(http.MethodGet, "/api/v1/account/wallets", nil))
	w := httptest.NewRecorder()
	api.ListWallets(w, r)

	if !called {
		t.Fatal("auth.ListBoundWallets not called")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Wallets []struct {
			WalletAddress string `json:"walletAddress"`
			Chain         string `json:"chain"`
			BoundAt       string `json:"boundAt"`
		} `json:"wallets"`
	}
	mustReadJSON(t, w.Body, &resp)
	if len(resp.Wallets) != 1 {
		t.Fatalf("want 1 wallet, got %d", len(resp.Wallets))
	}
	if resp.Wallets[0].WalletAddress != "9xQeWvG816bUx9EPjHmaT23yvVM2ZWbrrpZb9PusVFin" {
		t.Errorf("wrong wallet address: %q", resp.Wallets[0].WalletAddress)
	}
	if resp.Wallets[0].Chain != "solana" {
		t.Errorf("want chain=solana, got %q", resp.Wallets[0].Chain)
	}
	if resp.Wallets[0].BoundAt == "" {
		t.Error("boundAt is empty")
	}
}

func TestIssueWalletChallenge_RequiresAuth(t *testing.T) {
	api := newAPI(t, &clients.Set{})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/account/wallets/challenge",
		bytes.NewBufferString(`{"walletAddress":"abc"}`))
	w := httptest.NewRecorder()
	api.IssueWalletChallenge(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestIssueWalletChallenge_ForwardsAndExtractsNonce(t *testing.T) {
	gotAddr := ""
	set := &clients.Set{
		Auth: &mockAuth{
			startSiwsBinding: func(ctx context.Context, req *identityv1.StartSiwsBindingRequest) (*identityv1.StartSiwsBindingResponse, error) {
				gotAddr = req.WalletAddress
				if _, ok := clients.CallerClaims(ctx); !ok {
					t.Errorf("expected caller claims on outbound ctx")
				}
				return &identityv1.StartSiwsBindingResponse{
					Challenge: "iogrid.org wants you to sign in with your Solana account: ABC\n\nNonce: deadbeefcafebabe",
					ExpiresAt: timestamppb.Now(),
				}, nil
			},
		},
	}
	api := newAPI(t, set)
	body := bytes.NewBufferString(`{"walletAddress":"9xQeWvG816bUx9EPjHmaT23yvVM2ZWbrrpZb9PusVFin"}`)
	r := withAuth(httptest.NewRequest(http.MethodPost, "/api/v1/account/wallets/challenge", body))
	w := httptest.NewRecorder()
	api.IssueWalletChallenge(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if gotAddr != "9xQeWvG816bUx9EPjHmaT23yvVM2ZWbrrpZb9PusVFin" {
		t.Errorf("upstream got wrong address: %q", gotAddr)
	}
	var resp struct {
		Nonce     string `json:"nonce"`
		Challenge string `json:"challenge"`
		ExpiresAt string `json:"expiresAt"`
	}
	mustReadJSON(t, w.Body, &resp)
	if resp.Nonce != "deadbeefcafebabe" {
		t.Errorf("nonce extraction failed: %q", resp.Nonce)
	}
	if resp.Challenge == "" || resp.ExpiresAt == "" {
		t.Errorf("missing fields in response: %#v", resp)
	}
}

func TestIssueWalletChallenge_RejectsEmptyAddress(t *testing.T) {
	api := newAPI(t, &clients.Set{Auth: &mockAuth{}})
	body := bytes.NewBufferString(`{"walletAddress":""}`)
	r := withAuth(httptest.NewRequest(http.MethodPost, "/api/v1/account/wallets/challenge", body))
	w := httptest.NewRecorder()
	api.IssueWalletChallenge(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestBindWallet_RequiresAuth(t *testing.T) {
	api := newAPI(t, &clients.Set{})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/account/wallets",
		bytes.NewBufferString(`{"walletAddress":"abc","signature":"sig"}`))
	w := httptest.NewRecorder()
	api.BindWallet(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestBindWallet_ForwardsAndReturnsBoundEnvelope(t *testing.T) {
	gotAddr, gotSig := "", ""
	set := &clients.Set{
		Auth: &mockAuth{
			completeSiwsBinding: func(ctx context.Context, req *identityv1.CompleteSiwsBindingRequest) (*identityv1.CompleteSiwsBindingResponse, error) {
				gotAddr = req.WalletAddress
				gotSig = req.Signature
				if _, ok := clients.CallerClaims(ctx); !ok {
					t.Errorf("expected caller claims on outbound ctx")
				}
				return &identityv1.CompleteSiwsBindingResponse{
					Binding: &identityv1.WalletBinding{
						Id:        &commonv1.UUID{Value: "11111111-1111-1111-1111-111111111111"},
						UserId:    &commonv1.UUID{Value: "22222222-2222-2222-2222-222222222222"},
						Address:   req.WalletAddress,
						CreatedAt: timestamppb.Now(),
					},
				}, nil
			},
		},
	}
	api := newAPI(t, set)
	body := bytes.NewBufferString(`{"walletAddress":"WALLET","nonce":"NN","signature":"SIG"}`)
	r := withAuth(httptest.NewRequest(http.MethodPost, "/api/v1/account/wallets", body))
	w := httptest.NewRecorder()
	api.BindWallet(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if gotAddr != "WALLET" || gotSig != "SIG" {
		t.Errorf("upstream got addr=%q sig=%q", gotAddr, gotSig)
	}
	var resp struct {
		WalletAddress string `json:"walletAddress"`
		Chain         string `json:"chain"`
		BoundAt       string `json:"boundAt"`
	}
	mustReadJSON(t, w.Body, &resp)
	if resp.WalletAddress != "WALLET" {
		t.Errorf("response addr %q", resp.WalletAddress)
	}
	if resp.Chain != "solana" {
		t.Errorf("response chain %q", resp.Chain)
	}
}

func TestBindWallet_MapsInvalidSignature(t *testing.T) {
	set := &clients.Set{
		Auth: &mockAuth{
			completeSiwsBinding: func(_ context.Context, _ *identityv1.CompleteSiwsBindingRequest) (*identityv1.CompleteSiwsBindingResponse, error) {
				return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("siws: invalid signature"))
			},
		},
	}
	api := newAPI(t, set)
	body := bytes.NewBufferString(`{"walletAddress":"W","signature":"bad"}`)
	r := withAuth(httptest.NewRequest(http.MethodPost, "/api/v1/account/wallets", body))
	w := httptest.NewRecorder()
	api.BindWallet(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBindWallet_MapsChallengeNotFoundToPreconditionFailed(t *testing.T) {
	set := &clients.Set{
		Auth: &mockAuth{
			completeSiwsBinding: func(_ context.Context, _ *identityv1.CompleteSiwsBindingRequest) (*identityv1.CompleteSiwsBindingResponse, error) {
				return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("siws: challenge not found or expired"))
			},
		},
	}
	api := newAPI(t, set)
	body := bytes.NewBufferString(`{"walletAddress":"W","signature":"S"}`)
	r := withAuth(httptest.NewRequest(http.MethodPost, "/api/v1/account/wallets", body))
	w := httptest.NewRecorder()
	api.BindWallet(w, r)
	if w.Code != http.StatusPreconditionFailed {
		t.Fatalf("want 412, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBindWallet_MapsCrossUserToForbidden(t *testing.T) {
	set := &clients.Set{
		Auth: &mockAuth{
			completeSiwsBinding: func(_ context.Context, _ *identityv1.CompleteSiwsBindingRequest) (*identityv1.CompleteSiwsBindingResponse, error) {
				return nil, connect.NewError(connect.CodePermissionDenied, errors.New("siws: wallet already bound to another user"))
			},
		},
	}
	api := newAPI(t, set)
	body := bytes.NewBufferString(`{"walletAddress":"W","signature":"S"}`)
	r := withAuth(httptest.NewRequest(http.MethodPost, "/api/v1/account/wallets", body))
	w := httptest.NewRecorder()
	api.BindWallet(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestUnbindWallet_RequiresAuth(t *testing.T) {
	api := newAPI(t, &clients.Set{})
	r := httptest.NewRequest(http.MethodDelete, "/api/v1/account/wallets/ADDR", nil)
	w := httptest.NewRecorder()
	api.UnbindWallet(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestUnbindWallet_ForwardsAddressFromPath(t *testing.T) {
	got := ""
	set := &clients.Set{
		Auth: &mockAuth{
			unbindWallet: func(ctx context.Context, req *identityv1.UnbindWalletRequest) (*identityv1.UnbindWalletResponse, error) {
				got = req.WalletAddress
				if _, ok := clients.CallerClaims(ctx); !ok {
					t.Errorf("expected caller claims on outbound ctx")
				}
				return &identityv1.UnbindWalletResponse{}, nil
			},
		},
	}
	api := newAPI(t, set)
	srv := chi.NewRouter()
	srv.Delete("/api/v1/account/wallets/{address}", api.UnbindWallet)
	r := withAuth(httptest.NewRequest(http.MethodDelete, "/api/v1/account/wallets/MyAddr", nil))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if got != "MyAddr" {
		t.Errorf("upstream got %q", got)
	}
}

func TestUnbindWallet_MapsNotFound(t *testing.T) {
	set := &clients.Set{
		Auth: &mockAuth{
			unbindWallet: func(_ context.Context, _ *identityv1.UnbindWalletRequest) (*identityv1.UnbindWalletResponse, error) {
				return nil, connect.NewError(connect.CodeNotFound, errors.New("not found"))
			},
		},
	}
	api := newAPI(t, set)
	srv := chi.NewRouter()
	srv.Delete("/api/v1/account/wallets/{address}", api.UnbindWallet)
	r := withAuth(httptest.NewRequest(http.MethodDelete, "/api/v1/account/wallets/MyAddr", nil))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestExtractNonce(t *testing.T) {
	cases := []struct {
		challenge string
		want      string
	}{
		{"iogrid.org wants you to sign in with your Solana account: ABC\n\nNonce: abc123", "abc123"},
		{"x\n\nNonce: ", ""},
		{"no marker", ""},
		// Multiple "Nonce: " — last one wins (defensive against scope copy in domain text).
		{"Nonce: stale\n\nReal payload\n\nNonce: fresh", "fresh"},
	}
	for _, c := range cases {
		got := extractNonce(c.challenge)
		if got != c.want {
			t.Errorf("extractNonce(%q) = %q, want %q", c.challenge, got, c.want)
		}
	}
}
