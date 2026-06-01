package handlers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	billingv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1/billingv1connect"
	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
)

// BillingAPIKeyStore implements APIKeyStore by routing every operation
// to billing-svc's ApiKeyService Connect-RPC. This is the production
// path; the in-memory MemoryAPIKeyStore is retained only for unit
// tests that need to wire a store without standing up a billing-svc.
//
// Refs #563. Before this store landed, gateway-bff's /customer/api-keys
// CreateAPIKey wrote into MemoryAPIKeyStore — a process-local map —
// while every downstream validator (vpn-svc, proxy-gateway,
// build-gateway) authenticated against billing-svc's Postgres
// api_key table. The two stores never communicated, so EVERY
// UI-minted key was rejected by EVERY validator.
type BillingAPIKeyStore struct {
	client billingv1connect.ApiKeyServiceClient
}

// NewBillingAPIKeyStore wraps a Connect client. The client must be
// pre-configured with whatever interceptors gateway-bff uses
// elsewhere (service-token bearer, retry, etc.).
func NewBillingAPIKeyStore(client billingv1connect.ApiKeyServiceClient) *BillingAPIKeyStore {
	return &BillingAPIKeyStore{client: client}
}

// Create proxies to billing-svc.CreateApiKey + maps the response into
// the gateway-bff APIKey wire shape. The plaintext is returned
// once, exactly as the existing handler contract promises.
func (s *BillingAPIKeyStore) Create(ctx context.Context, workspaceID uuid.UUID, label string) (APIKey, error) {
	resp, err := s.client.CreateApiKey(ctx, connect.NewRequest(&billingv1.CreateApiKeyRequest{
		WorkspaceId: &commonv1.UUID{Value: workspaceID.String()},
		Label:       label,
	}))
	if err != nil {
		return APIKey{}, fmt.Errorf("billing.CreateApiKey: %w", err)
	}
	pbKey := resp.Msg.GetApiKey()
	if pbKey == nil {
		return APIKey{}, errors.New("billing.CreateApiKey returned empty api_key")
	}
	out, err := apiKeyFromProto(pbKey)
	if err != nil {
		return APIKey{}, err
	}
	out.Plaintext = resp.Msg.GetPlaintextKey()
	return out, nil
}

// List proxies to billing-svc.ListApiKeys. Pagination is best-effort:
// gateway-bff's existing handler doesn't expose page params, so we
// request the first 200 keys and let the UI live with the cap until
// a follow-up adds proper pagination.
func (s *BillingAPIKeyStore) List(ctx context.Context, workspaceID uuid.UUID) ([]APIKey, error) {
	resp, err := s.client.ListApiKeys(ctx, connect.NewRequest(&billingv1.ListApiKeysRequest{
		WorkspaceId: &commonv1.UUID{Value: workspaceID.String()},
	}))
	if err != nil {
		return nil, fmt.Errorf("billing.ListApiKeys: %w", err)
	}
	pbKeys := resp.Msg.GetApiKeys()
	out := make([]APIKey, 0, len(pbKeys))
	for _, pb := range pbKeys {
		k, err := apiKeyFromProto(pb)
		if err != nil {
			// Skip malformed rows rather than failing the whole list.
			continue
		}
		out = append(out, k)
	}
	return out, nil
}

// Delete proxies to billing-svc.RevokeApiKey. Billing semantically
// "revokes" (sets revoked_at) rather than hard-deletes; for the
// gateway-bff contract that returns ErrAPIKeyNotFound on miss this
// difference doesn't matter — a revoked-but-existing row is treated
// the same as deleted from the UI's perspective. The workspaceID
// argument is enforced by billing-svc itself (the RPC rejects when
// the caller's workspace doesn't own the key).
func (s *BillingAPIKeyStore) Delete(ctx context.Context, _ uuid.UUID, id uuid.UUID) error {
	_, err := s.client.RevokeApiKey(ctx, connect.NewRequest(&billingv1.RevokeApiKeyRequest{
		Id: &commonv1.UUID{Value: id.String()},
	}))
	if err == nil {
		return nil
	}
	if cerr := (&connect.Error{}); errors.As(err, &cerr) {
		if cerr.Code() == connect.CodeNotFound {
			return ErrAPIKeyNotFound
		}
	}
	return fmt.Errorf("billing.RevokeApiKey: %w", err)
}

// apiKeyFromProto converts billing-svc's ApiKey message into the
// gateway-bff JSON shape the existing handler / web UI expects.
// Critically: billing-svc returns `last_four` while the UI's prior
// shape carried a `prefix` field; we synthesise a 12-char prefix as
// "iog_…<last_four>" so the UI render layer (which truncates the
// prefix) keeps working. The full plaintext is only set in Create.
func apiKeyFromProto(pb *billingv1.ApiKey) (APIKey, error) {
	id, err := uuid.Parse(pb.GetId().GetValue())
	if err != nil {
		return APIKey{}, fmt.Errorf("bad api_key id: %w", err)
	}
	ws, err := uuid.Parse(pb.GetWorkspaceId().GetValue())
	if err != nil {
		return APIKey{}, fmt.Errorf("bad workspace id: %w", err)
	}
	prefix := pb.GetLastFour()
	if prefix != "" {
		prefix = "iog_…" + prefix
	}
	out := APIKey{
		ID:          id,
		WorkspaceID: ws,
		Label:       pb.GetLabel(),
		Prefix:      prefix,
	}
	if t := pb.GetCreatedAt(); t != nil {
		out.CreatedAt = t.AsTime()
	}
	if t := pb.GetLastUsedAt(); t != nil {
		u := t.AsTime()
		out.LastUsedAt = &u
	}
	// Revoked keys are filtered out by the billing-svc handler before
	// they reach List; no need to surface revoked_at in this shape.
	_ = time.Time{} // keep the time import live in case we add formatting later.
	return out, nil
}
