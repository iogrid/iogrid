package handlers

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	billingv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1/billingv1connect"
	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
)

// fakeBillingClient is a hand-rolled stub of the billingv1connect
// ApiKeyServiceClient interface — small enough to inline rather than
// pulling a mock-gen dependency. Each handler captures the request and
// returns the canned response / error the test set.
//
// Why hand-rolled: gateway-bff has zero mock-gen deps; staying with
// stdlib keeps the test surface flat + fast (~5ms per test) without
// adding a new build-time dep.
type fakeBillingClient struct {
	billingv1connect.ApiKeyServiceClient // embed to satisfy interface for unused methods

	lastCreate *billingv1.CreateApiKeyRequest
	createResp *billingv1.CreateApiKeyResponse
	createErr  error

	lastList *billingv1.ListApiKeysRequest
	listResp *billingv1.ListApiKeysResponse
	listErr  error

	lastRevoke *billingv1.RevokeApiKeyRequest
	revokeResp *billingv1.RevokeApiKeyResponse
	revokeErr  error
}

func (f *fakeBillingClient) CreateApiKey(_ context.Context, req *connect.Request[billingv1.CreateApiKeyRequest]) (*connect.Response[billingv1.CreateApiKeyResponse], error) {
	f.lastCreate = req.Msg
	if f.createErr != nil {
		return nil, f.createErr
	}
	return connect.NewResponse(f.createResp), nil
}

func (f *fakeBillingClient) ListApiKeys(_ context.Context, req *connect.Request[billingv1.ListApiKeysRequest]) (*connect.Response[billingv1.ListApiKeysResponse], error) {
	f.lastList = req.Msg
	if f.listErr != nil {
		return nil, f.listErr
	}
	return connect.NewResponse(f.listResp), nil
}

func (f *fakeBillingClient) RevokeApiKey(_ context.Context, req *connect.Request[billingv1.RevokeApiKeyRequest]) (*connect.Response[billingv1.RevokeApiKeyResponse], error) {
	f.lastRevoke = req.Msg
	if f.revokeErr != nil {
		return nil, f.revokeErr
	}
	if f.revokeResp == nil {
		return connect.NewResponse(&billingv1.RevokeApiKeyResponse{}), nil
	}
	return connect.NewResponse(f.revokeResp), nil
}

// --- Create -------------------------------------------------------------

func TestBillingAPIKeyStore_CreateHappyPath(t *testing.T) {
	keyID := uuid.New()
	wsID := uuid.New()
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	fake := &fakeBillingClient{
		createResp: &billingv1.CreateApiKeyResponse{
			ApiKey: &billingv1.ApiKey{
				Id:          &commonv1.UUID{Value: keyID.String()},
				WorkspaceId: &commonv1.UUID{Value: wsID.String()},
				Label:       "deploy bot",
				LastFour:    "ab12",
				CreatedAt:   timestamppb.New(now),
			},
			PlaintextKey: "iog_sk_abcdef1234567890ab12",
		},
	}
	store := NewBillingAPIKeyStore(fake)

	got, err := store.Create(context.Background(), wsID, "deploy bot")
	if err != nil {
		t.Fatalf("Create: unexpected error %v", err)
	}
	if got.ID != keyID {
		t.Fatalf("id: want %s got %s", keyID, got.ID)
	}
	if got.WorkspaceID != wsID {
		t.Fatalf("workspace_id: want %s got %s", wsID, got.WorkspaceID)
	}
	if got.Label != "deploy bot" {
		t.Fatalf("label: want %q got %q", "deploy bot", got.Label)
	}
	if got.Prefix != "iog_…ab12" {
		t.Fatalf("prefix: want %q got %q", "iog_…ab12", got.Prefix)
	}
	if got.Plaintext != "iog_sk_abcdef1234567890ab12" {
		t.Fatalf("plaintext: want full key got %q", got.Plaintext)
	}
	if !got.CreatedAt.Equal(now) {
		t.Fatalf("created_at: want %v got %v", now, got.CreatedAt)
	}

	// Wire-shape: workspace ID + label propagated to billing-svc.
	if fake.lastCreate.GetWorkspaceId().GetValue() != wsID.String() {
		t.Fatalf("wire ws_id mismatch: %s", fake.lastCreate.GetWorkspaceId().GetValue())
	}
	if fake.lastCreate.GetLabel() != "deploy bot" {
		t.Fatalf("wire label mismatch: %q", fake.lastCreate.GetLabel())
	}
}

func TestBillingAPIKeyStore_CreatePropagatesError(t *testing.T) {
	fake := &fakeBillingClient{
		createErr: connect.NewError(connect.CodePermissionDenied, errors.New("not owner of workspace")),
	}
	store := NewBillingAPIKeyStore(fake)
	_, err := store.Create(context.Background(), uuid.New(), "x")
	if err == nil {
		t.Fatal("Create: expected error from underlying client, got nil")
	}
}

func TestBillingAPIKeyStore_CreateNilApiKeyIsError(t *testing.T) {
	fake := &fakeBillingClient{
		createResp: &billingv1.CreateApiKeyResponse{ApiKey: nil, PlaintextKey: "iog_sk_…"},
	}
	store := NewBillingAPIKeyStore(fake)
	_, err := store.Create(context.Background(), uuid.New(), "x")
	if err == nil {
		t.Fatal("Create: expected error on nil ApiKey in response, got nil")
	}
}

func TestBillingAPIKeyStore_CreateBadIDIsError(t *testing.T) {
	fake := &fakeBillingClient{
		createResp: &billingv1.CreateApiKeyResponse{
			ApiKey: &billingv1.ApiKey{
				Id:          &commonv1.UUID{Value: "not-a-uuid"},
				WorkspaceId: &commonv1.UUID{Value: uuid.New().String()},
			},
		},
	}
	store := NewBillingAPIKeyStore(fake)
	_, err := store.Create(context.Background(), uuid.New(), "x")
	if err == nil {
		t.Fatal("Create: expected parse error on malformed id, got nil")
	}
}

// --- List ---------------------------------------------------------------

func TestBillingAPIKeyStore_ListHappyPath(t *testing.T) {
	ws := uuid.New()
	id1, id2 := uuid.New(), uuid.New()
	fake := &fakeBillingClient{
		listResp: &billingv1.ListApiKeysResponse{
			ApiKeys: []*billingv1.ApiKey{
				{
					Id:          &commonv1.UUID{Value: id1.String()},
					WorkspaceId: &commonv1.UUID{Value: ws.String()},
					Label:       "ci",
					LastFour:    "1111",
				},
				{
					Id:          &commonv1.UUID{Value: id2.String()},
					WorkspaceId: &commonv1.UUID{Value: ws.String()},
					Label:       "prod",
					LastFour:    "2222",
				},
			},
		},
	}
	store := NewBillingAPIKeyStore(fake)
	got, err := store.List(context.Background(), ws)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len: want 2 got %d", len(got))
	}
	if got[0].ID != id1 || got[0].Prefix != "iog_…1111" {
		t.Fatalf("[0] mismatch: %+v", got[0])
	}
	if got[1].ID != id2 || got[1].Prefix != "iog_…2222" {
		t.Fatalf("[1] mismatch: %+v", got[1])
	}
}

// Revoked rows must be hidden: billing-svc deliberately returns them
// (revoked_at set), but this surface feeds the UI's "Active keys" list —
// and the post-revoke refresh reads it, so an unfiltered revoked row
// makes a successful revoke LOOK failed (#676 residual).
func TestBillingAPIKeyStore_ListHidesRevokedRows(t *testing.T) {
	ws := uuid.New()
	active, revoked := uuid.New(), uuid.New()
	fake := &fakeBillingClient{
		listResp: &billingv1.ListApiKeysResponse{
			ApiKeys: []*billingv1.ApiKey{
				{
					Id:          &commonv1.UUID{Value: active.String()},
					WorkspaceId: &commonv1.UUID{Value: ws.String()},
					Label:       "live",
					LastFour:    "3333",
				},
				{
					Id:          &commonv1.UUID{Value: revoked.String()},
					WorkspaceId: &commonv1.UUID{Value: ws.String()},
					Label:       "killed",
					LastFour:    "4444",
					RevokedAt:   timestamppb.Now(),
				},
			},
		},
	}
	store := NewBillingAPIKeyStore(fake)
	got, err := store.List(context.Background(), ws)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len: want 1 (revoked hidden) got %d", len(got))
	}
	if got[0].ID != active {
		t.Fatalf("surviving row should be the active key, got %+v", got[0])
	}
}

func TestBillingAPIKeyStore_ListSkipsMalformedRows(t *testing.T) {
	ws := uuid.New()
	good := uuid.New()
	fake := &fakeBillingClient{
		listResp: &billingv1.ListApiKeysResponse{
			ApiKeys: []*billingv1.ApiKey{
				{
					Id:          &commonv1.UUID{Value: "bad-uuid"},
					WorkspaceId: &commonv1.UUID{Value: ws.String()},
					Label:       "broken",
				},
				{
					Id:          &commonv1.UUID{Value: good.String()},
					WorkspaceId: &commonv1.UUID{Value: ws.String()},
					Label:       "ok",
					LastFour:    "9999",
				},
			},
		},
	}
	store := NewBillingAPIKeyStore(fake)
	got, err := store.List(context.Background(), ws)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].ID != good {
		t.Fatalf("expected 1 valid row, got %+v", got)
	}
}

func TestBillingAPIKeyStore_ListPropagatesError(t *testing.T) {
	fake := &fakeBillingClient{
		listErr: connect.NewError(connect.CodeUnavailable, errors.New("billing-svc 503")),
	}
	store := NewBillingAPIKeyStore(fake)
	_, err := store.List(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("List: expected error, got nil")
	}
}

// --- Delete -------------------------------------------------------------

func TestBillingAPIKeyStore_DeleteHappyPath(t *testing.T) {
	keyID := uuid.New()
	fake := &fakeBillingClient{}
	store := NewBillingAPIKeyStore(fake)
	if err := store.Delete(context.Background(), uuid.New(), keyID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if fake.lastRevoke == nil || fake.lastRevoke.GetId().GetValue() != keyID.String() {
		t.Fatalf("wire id mismatch: %v", fake.lastRevoke)
	}
}

func TestBillingAPIKeyStore_DeleteNotFoundMapsToErrAPIKeyNotFound(t *testing.T) {
	fake := &fakeBillingClient{
		revokeErr: connect.NewError(connect.CodeNotFound, errors.New("no such key")),
	}
	store := NewBillingAPIKeyStore(fake)
	err := store.Delete(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, ErrAPIKeyNotFound) {
		t.Fatalf("Delete: want ErrAPIKeyNotFound, got %v", err)
	}
}

func TestBillingAPIKeyStore_DeleteOtherErrorsBubbleUp(t *testing.T) {
	fake := &fakeBillingClient{
		revokeErr: connect.NewError(connect.CodeInternal, errors.New("db down")),
	}
	store := NewBillingAPIKeyStore(fake)
	err := store.Delete(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("Delete: expected non-nil error for internal failure")
	}
	if errors.Is(err, ErrAPIKeyNotFound) {
		t.Fatalf("Delete: internal error should NOT map to ErrAPIKeyNotFound, got %v", err)
	}
}
