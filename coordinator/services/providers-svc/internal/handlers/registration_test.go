package handlers

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"testing"
	"time"

	"connectrpc.com/connect"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	providersv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1"
	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/ca"
	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/store"
)

func newTestHandler(t *testing.T) *RegistrationHandler {
	t.Helper()
	c, err := ca.NewInMemory()
	if err != nil {
		t.Fatalf("ca: %v", err)
	}
	return NewRegistrationHandler(store.NewInMemory(), c, nil)
}

func newDaemonPubKey(t *testing.T) []byte {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return der
}

func TestPairDaemon_HappyPath(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	tok, _ := h.Store.IssuePairingToken(ctx, "owner-99", 0)

	resp, err := h.PairDaemon(ctx, connect.NewRequest(&providersv1.PairDaemonRequest{
		PairingToken:    tok,
		DaemonPublicKey: newDaemonPubKey(t),
		DisplayName:     "Living room iMac",
		HostInfo: &providersv1.HostInfo{
			Platform: providersv1.Platform_PLATFORM_MACOS,
		},
	}))
	if err != nil {
		t.Fatalf("PairDaemon: %v", err)
	}
	if resp.Msg.Provider.GetId().GetValue() == "" {
		t.Fatalf("expected provider id")
	}
	if len(resp.Msg.DaemonCertificate) == 0 {
		t.Fatalf("expected certificate")
	}
	if len(resp.Msg.ServerCaBundle) == 0 {
		t.Fatalf("expected ca bundle")
	}
}

func TestPairDaemon_BadToken(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	_, err := h.PairDaemon(ctx, connect.NewRequest(&providersv1.PairDaemonRequest{
		PairingToken:    "no-such-token",
		DaemonPublicKey: newDaemonPubKey(t),
	}))
	if err == nil {
		t.Fatalf("expected error")
	}
	var ce *connect.Error
	if !errorAs(err, &ce) {
		t.Fatalf("expected connect.Error, got %T", err)
	}
	if ce.Code() != connect.CodePermissionDenied {
		t.Fatalf("expected PermissionDenied, got %s", ce.Code())
	}
}

func TestPairDaemon_MissingPublicKey(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	tok, _ := h.Store.IssuePairingToken(ctx, "x", 0)
	_, err := h.PairDaemon(ctx, connect.NewRequest(&providersv1.PairDaemonRequest{
		PairingToken: tok,
	}))
	if err == nil {
		t.Fatalf("expected error")
	}
	var ce *connect.Error
	if !errorAs(err, &ce) || ce.Code() != connect.CodeInvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", err)
	}
}

func TestGetProvider_NotFound(t *testing.T) {
	h := newTestHandler(t)
	_, err := h.GetProvider(context.Background(), connect.NewRequest(&providersv1.GetProviderRequest{
		ProviderId: &commonv1.UUID{Value: "ghost"},
	}))
	if err == nil {
		t.Fatalf("expected error")
	}
	var ce *connect.Error
	if !errorAs(err, &ce) || ce.Code() != connect.CodeNotFound {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestListProviders_FilterByOwner(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	for _, owner := range []string{"a", "a", "b"} {
		_ = h.Store.CreateProvider(ctx, &store.Provider{OwnerUserID: owner})
	}
	resp, err := h.ListProviders(ctx, connect.NewRequest(&providersv1.ListProvidersRequest{
		OwnerUserId: &commonv1.UUID{Value: "a"},
	}))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(resp.Msg.Providers) != 2 {
		t.Fatalf("expected 2, got %d", len(resp.Msg.Providers))
	}
}

func TestDeactivate_NotFound(t *testing.T) {
	h := newTestHandler(t)
	_, err := h.DeactivateProvider(context.Background(), connect.NewRequest(&providersv1.DeactivateProviderRequest{
		ProviderId: &commonv1.UUID{Value: "no-such-id"},
	}))
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestUpdateHostInfo_AfterPair(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	tok, _ := h.Store.IssuePairingToken(ctx, "owner", 0)
	pair, err := h.PairDaemon(ctx, connect.NewRequest(&providersv1.PairDaemonRequest{
		PairingToken:    tok,
		DaemonPublicKey: newDaemonPubKey(t),
	}))
	if err != nil {
		t.Fatalf("pair: %v", err)
	}
	id := pair.Msg.Provider.GetId()

	resp, err := h.UpdateHostInfo(ctx, connect.NewRequest(&providersv1.UpdateHostInfoRequest{
		ProviderId: id,
		HostInfo: &providersv1.HostInfo{
			Platform:        providersv1.Platform_PLATFORM_LINUX,
			CpuLogicalCores: 16,
		},
	}))
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if resp.Msg.Provider.HostInfo.GetCpuLogicalCores() != 16 {
		t.Fatalf("update did not persist")
	}
}

func TestIssuePairingToken_HappyPath(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	resp, err := h.IssuePairingToken(ctx, connect.NewRequest(&providersv1.IssuePairingTokenRequest{
		OwnerUserId: &commonv1.UUID{Value: "owner-iss-1"},
		TtlSeconds:  120,
	}))
	if err != nil {
		t.Fatalf("IssuePairingToken: %v", err)
	}
	if resp.Msg.GetPairingToken() == "" {
		t.Fatalf("expected non-empty token")
	}
	if !resp.Msg.GetExpiresAt().IsValid() {
		t.Fatalf("expected expires_at populated")
	}
	// Round-trip: the token must be consumable and resolve to owner-iss-1.
	pt, err := h.Store.ConsumePairingToken(ctx, resp.Msg.GetPairingToken())
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if pt.OwnerUserID != "owner-iss-1" {
		t.Fatalf("owner mismatch: %q", pt.OwnerUserID)
	}
}

func TestIssuePairingToken_MissingOwner(t *testing.T) {
	h := newTestHandler(t)
	_, err := h.IssuePairingToken(context.Background(), connect.NewRequest(&providersv1.IssuePairingTokenRequest{}))
	if err == nil {
		t.Fatalf("expected error")
	}
	var ce *connect.Error
	if !errorAs(err, &ce) || ce.Code() != connect.CodeInvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", err)
	}
}

func TestIssuePairingToken_ZeroTTLDefaults(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	resp, err := h.IssuePairingToken(ctx, connect.NewRequest(&providersv1.IssuePairingTokenRequest{
		OwnerUserId: &commonv1.UUID{Value: "owner-d"},
	}))
	if err != nil {
		t.Fatalf("IssuePairingToken: %v", err)
	}
	// Default = 10 minutes; allow a generous skew window.
	delta := resp.Msg.GetExpiresAt().AsTime().Sub(time.Now())
	if delta < 9*time.Minute || delta > 11*time.Minute {
		t.Fatalf("default TTL out of range: %v", delta)
	}
}

// errorAs is a tiny helper around errors.As that returns the bool so we
// can write `if !errorAs(err, &ce)` cleanly.
func errorAs(err error, target interface{}) bool {
	type asable interface{ As(any) bool }
	if err == nil {
		return false
	}
	if c, ok := err.(*connect.Error); ok {
		if pp, ok := target.(**connect.Error); ok {
			*pp = c
			return true
		}
	}
	if a, ok := err.(asable); ok {
		return a.As(target)
	}
	return false
}
