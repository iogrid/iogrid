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

// --- #325 — primary-provider election ---------------------------------

// TestPairDaemon_AutoPromotesFirstProviderToPrimary asserts that the
// first daemon paired by an owner is marked is_primary=true so the
// single-daemon happy path (the vast majority of users) never has to
// touch the picker. Closes the #325 multi-daemon ambiguity.
func TestPairDaemon_AutoPromotesFirstProviderToPrimary(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	tok, _ := h.Store.IssuePairingToken(ctx, "owner-pri-1", 0)

	resp, err := h.PairDaemon(ctx, connect.NewRequest(&providersv1.PairDaemonRequest{
		PairingToken:    tok,
		DaemonPublicKey: newDaemonPubKey(t),
		DisplayName:     "my first mac",
	}))
	if err != nil {
		t.Fatalf("PairDaemon: %v", err)
	}
	if !resp.Msg.GetProvider().GetIsPrimary() {
		t.Fatalf("first pair must auto-promote to primary, got is_primary=false")
	}
}

// TestPairDaemon_SecondProviderNotPrimary asserts that subsequent pairs
// stay non-primary — the owner explicitly elects via SetPrimaryProvider.
// Second daemon silently stealing the primary slot would re-introduce
// the same wrong-daemon ambiguity #325 closes.
func TestPairDaemon_SecondProviderNotPrimary(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	tok1, _ := h.Store.IssuePairingToken(ctx, "owner-pri-2", 0)
	first, err := h.PairDaemon(ctx, connect.NewRequest(&providersv1.PairDaemonRequest{
		PairingToken:    tok1,
		DaemonPublicKey: newDaemonPubKey(t),
		DisplayName:     "first",
	}))
	if err != nil {
		t.Fatalf("first pair: %v", err)
	}
	if !first.Msg.GetProvider().GetIsPrimary() {
		t.Fatalf("first daemon must be primary")
	}

	tok2, _ := h.Store.IssuePairingToken(ctx, "owner-pri-2", 0)
	second, err := h.PairDaemon(ctx, connect.NewRequest(&providersv1.PairDaemonRequest{
		PairingToken:    tok2,
		DaemonPublicKey: newDaemonPubKey(t),
		DisplayName:     "second",
	}))
	if err != nil {
		t.Fatalf("second pair: %v", err)
	}
	if second.Msg.GetProvider().GetIsPrimary() {
		t.Fatalf("second daemon must NOT be primary on pair; owner re-elects via SetPrimaryProvider")
	}
}

// TestSetPrimaryProvider_AtomicSwap exercises the happy-path swap and
// verifies the prior primary is cleared in the same transaction.
func TestSetPrimaryProvider_AtomicSwap(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	tok1, _ := h.Store.IssuePairingToken(ctx, "owner-swap", 0)
	first, err := h.PairDaemon(ctx, connect.NewRequest(&providersv1.PairDaemonRequest{
		PairingToken:    tok1,
		DaemonPublicKey: newDaemonPubKey(t),
	}))
	if err != nil {
		t.Fatalf("first pair: %v", err)
	}
	firstID := first.Msg.GetProvider().GetId().GetValue()

	tok2, _ := h.Store.IssuePairingToken(ctx, "owner-swap", 0)
	second, err := h.PairDaemon(ctx, connect.NewRequest(&providersv1.PairDaemonRequest{
		PairingToken:    tok2,
		DaemonPublicKey: newDaemonPubKey(t),
	}))
	if err != nil {
		t.Fatalf("second pair: %v", err)
	}
	secondID := second.Msg.GetProvider().GetId().GetValue()

	resp, err := h.SetPrimaryProvider(ctx, connect.NewRequest(&providersv1.SetPrimaryProviderRequest{
		OwnerUserId: &commonv1.UUID{Value: "owner-swap"},
		ProviderId:  &commonv1.UUID{Value: secondID},
	}))
	if err != nil {
		t.Fatalf("SetPrimaryProvider: %v", err)
	}
	if resp.Msg.GetProvider().GetId().GetValue() != secondID || !resp.Msg.GetProvider().GetIsPrimary() {
		t.Fatalf("returned provider should be promoted second: %+v", resp.Msg.GetProvider())
	}

	// Verify the prior primary was cleared.
	gotFirst, err := h.Store.GetProvider(ctx, firstID)
	if err != nil {
		t.Fatalf("get first: %v", err)
	}
	if gotFirst.IsPrimary {
		t.Fatalf("prior primary should be cleared, still IsPrimary=true")
	}
	// And the new primary is set.
	gotSecond, err := h.Store.GetProvider(ctx, secondID)
	if err != nil {
		t.Fatalf("get second: %v", err)
	}
	if !gotSecond.IsPrimary {
		t.Fatalf("new primary should be set")
	}
}

// TestSetPrimaryProvider_PermissionDeniedForNonOwner is the regression
// receipt for the "Hatice cannot promote someone else's daemon" surface.
// providers-svc validates ownership in the WHERE clause; a mismatch
// translates to PermissionDenied so non-owners cannot probe by id.
func TestSetPrimaryProvider_PermissionDeniedForNonOwner(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	tok, _ := h.Store.IssuePairingToken(ctx, "owner-A", 0)
	pair, _ := h.PairDaemon(ctx, connect.NewRequest(&providersv1.PairDaemonRequest{
		PairingToken:    tok,
		DaemonPublicKey: newDaemonPubKey(t),
	}))
	pid := pair.Msg.GetProvider().GetId().GetValue()

	_, err := h.SetPrimaryProvider(ctx, connect.NewRequest(&providersv1.SetPrimaryProviderRequest{
		OwnerUserId: &commonv1.UUID{Value: "owner-B"},
		ProviderId:  &commonv1.UUID{Value: pid},
	}))
	if err == nil {
		t.Fatalf("expected error when caller does not own the provider")
	}
	var ce *connect.Error
	if !errorAs(err, &ce) || ce.Code() != connect.CodePermissionDenied {
		t.Fatalf("expected PermissionDenied, got %v", err)
	}
}

func TestSetPrimaryProvider_RequiresOwnerAndProvider(t *testing.T) {
	h := newTestHandler(t)
	cases := map[string]*providersv1.SetPrimaryProviderRequest{
		"missing owner_user_id": {ProviderId: &commonv1.UUID{Value: "x"}},
		"missing provider_id":   {OwnerUserId: &commonv1.UUID{Value: "x"}},
		"both missing":          {},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := h.SetPrimaryProvider(context.Background(), connect.NewRequest(req))
			if err == nil {
				t.Fatalf("expected error")
			}
			var ce *connect.Error
			if !errorAs(err, &ce) || ce.Code() != connect.CodeInvalidArgument {
				t.Fatalf("expected InvalidArgument, got %v", err)
			}
		})
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
