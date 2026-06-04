// api_keys.go implements the Connect-RPC ApiKeyService.
//
// proxy-gateway + build-gateway call ValidateApiKey on every connection;
// gateway-bff calls Create/List/Revoke on behalf of authenticated users.
// The wire shape is defined in proto/iogrid/billing/v1/api_keys.proto.
//
// Plaintext key generation uses crypto/rand: 32 bytes of entropy hex-encoded
// to a 64-char ASCII token prefixed with "iog_". The plaintext is returned
// once on CreateApiKey and never stored — we persist sha256(plaintext)
// only. ValidateApiKey recomputes the hash on every call and does a
// single indexed lookup.
package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	billingv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1/billingv1connect"
	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/store"
)

// ApiKeyHandler implements billingv1connect.ApiKeyServiceHandler.
type ApiKeyHandler struct {
	billingv1connect.UnimplementedApiKeyServiceHandler
	Store *store.Store
}

// NewApiKeyHandler wires the dependency.
func NewApiKeyHandler(s *store.Store) *ApiKeyHandler {
	return &ApiKeyHandler{Store: s}
}

// keyPrefix is the human-recognisable identifier on every iogrid API key.
const keyPrefix = "iog_"

// keyEntropyBytes is the random material per key; 32 bytes = 256 bits.
const keyEntropyBytes = 32

// mintPlaintextKey returns a fresh "iog_<64-hex>" token. The hash is the
// hex-encoded SHA-256 of the entire string (including the prefix).
func mintPlaintextKey() (plaintext, keyHash, lastFour string, err error) {
	buf := make([]byte, keyEntropyBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", "", err
	}
	plaintext = keyPrefix + hex.EncodeToString(buf)
	h := sha256.Sum256([]byte(plaintext))
	keyHash = hex.EncodeToString(h[:])
	lastFour = plaintext[len(plaintext)-4:]
	return plaintext, keyHash, lastFour, nil
}

// hashKey returns the hex-encoded SHA-256 of an existing plaintext token.
func hashKey(plaintext string) string {
	h := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(h[:])
}

// CreateApiKey mints a new key and returns the plaintext (one-time).
func (h *ApiKeyHandler) CreateApiKey(
	ctx context.Context,
	req *connect.Request[billingv1.CreateApiKeyRequest],
) (*connect.Response[billingv1.CreateApiKeyResponse], error) {
	wsID, err := parseUUID(req.Msg.GetWorkspaceId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	label := strings.TrimSpace(req.Msg.GetLabel())
	if label == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("label required"))
	}

	plaintext, keyHash, lastFour, err := mintPlaintextKey()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Inherit tier from the workspace's current subscription. Best-effort:
	// if there is no subscription row yet, we default to PAYG.
	tier := "PAYG"
	if sub, err := h.Store.GetSubscriptionByWorkspace(ctx, wsID); err == nil && sub != nil {
		tier = sub.Tier
	}

	row := store.ApiKey{
		WorkspaceID: wsID,
		Label:       label,
		KeyHash:     keyHash,
		LastFour:    lastFour,
		Tier:        tier,
		CreatedAt:   time.Now().UTC(),
	}
	if err := h.Store.InsertApiKey(ctx, row); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	// Re-fetch so the returned id reflects whatever the DB minted.
	got, err := h.Store.LookupApiKeyByHash(ctx, keyHash)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&billingv1.CreateApiKeyResponse{
		ApiKey:       apiKeyToProto(*got),
		PlaintextKey: plaintext,
	}), nil
}

// ListApiKeys returns redacted rows for a workspace, newest first.
func (h *ApiKeyHandler) ListApiKeys(
	ctx context.Context,
	req *connect.Request[billingv1.ListApiKeysRequest],
) (*connect.Response[billingv1.ListApiKeysResponse], error) {
	wsID, err := parseUUID(req.Msg.GetWorkspaceId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	limit, offset := pageOpts(req.Msg.GetPage())
	rows, err := h.Store.ListApiKeysByWorkspace(ctx, wsID, limit, offset)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &billingv1.ListApiKeysResponse{
		ApiKeys: make([]*billingv1.ApiKey, 0, len(rows)),
		Page:    &commonv1.PageResponse{},
	}
	for _, r := range rows {
		out.ApiKeys = append(out.ApiKeys, apiKeyToProto(r))
	}
	return connect.NewResponse(out), nil
}

// RevokeApiKey marks a key revoked. Idempotent.
func (h *ApiKeyHandler) RevokeApiKey(
	ctx context.Context,
	req *connect.Request[billingv1.RevokeApiKeyRequest],
) (*connect.Response[billingv1.RevokeApiKeyResponse], error) {
	id, err := parseUUID(req.Msg.GetId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := h.Store.RevokeApiKey(ctx, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	got, err := h.Store.GetApiKey(ctx, id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&billingv1.RevokeApiKeyResponse{
		ApiKey: apiKeyToProto(*got),
	}), nil
}

// ValidateApiKey is the hot-path call from proxy-gateway + build-gateway.
// It must be O(1) — a single hashed-index lookup. The proxy is expected
// to cache positive results for ~5 minutes (see proxy-gateway/internal/auth).
func (h *ApiKeyHandler) ValidateApiKey(
	ctx context.Context,
	req *connect.Request[billingv1.ValidateApiKeyRequest],
) (*connect.Response[billingv1.ValidateApiKeyResponse], error) {
	plaintext := req.Msg.GetApiKey()
	if plaintext == "" {
		return connect.NewResponse(&billingv1.ValidateApiKeyResponse{
			Valid:      false,
			ResolvedAt: timestamppb.Now(),
		}), nil
	}
	row, err := h.Store.LookupApiKeyByHash(ctx, hashKey(plaintext))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return connect.NewResponse(&billingv1.ValidateApiKeyResponse{
				Valid:      false,
				ResolvedAt: timestamppb.Now(),
			}), nil
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Resolve the workspace's subscription status. A row exists, the key
	// is not revoked, but the workspace can still be suspended (billing
	// past-due, AUP violation, etc.) — bubble that to the caller.
	suspended := false
	if sub, err := h.Store.GetSubscriptionByWorkspace(ctx, row.WorkspaceID); err == nil && sub != nil {
		switch strings.ToLower(sub.Status) {
		case "past_due", "unpaid", "canceled", "suspended":
			suspended = true
		}
	}

	// Best-effort bump last_used_at. Failure here must not block auth.
	go func() {
		bg, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = h.Store.TouchApiKeyLastUsed(bg, row.ID)
	}()

	return connect.NewResponse(&billingv1.ValidateApiKeyResponse{
		Valid:             true,
		WorkspaceId:       &commonv1.UUID{Value: row.WorkspaceID.String()},
		CustomerId:        &commonv1.UUID{Value: row.WorkspaceID.String()}, // alias for now
		Tier:              tierFromString(row.Tier),
		AllowedCategories: splitCSV(row.AllowedCategories),
		GeoTarget:         row.GeoTarget,
		KycVerified:       row.KYCVerified,
		Suspended:         suspended,
		ResolvedAt:        timestamppb.Now(),
	}), nil
}

// ── helpers ─────────────────────────────────────────────────────────

func apiKeyToProto(k store.ApiKey) *billingv1.ApiKey {
	out := &billingv1.ApiKey{
		Id:          &commonv1.UUID{Value: k.ID.String()},
		WorkspaceId: &commonv1.UUID{Value: k.WorkspaceID.String()},
		Label:       k.Label,
		LastFour:    k.LastFour,
		Tier:        tierFromString(k.Tier),
		CreatedAt:   timestamppb.New(k.CreatedAt),
	}
	if k.LastUsedAt != nil {
		out.LastUsedAt = timestamppb.New(*k.LastUsedAt)
	}
	if k.RevokedAt != nil {
		out.RevokedAt = timestamppb.New(*k.RevokedAt)
	}
	return out
}

func parseUUID(u *commonv1.UUID) (uuid.UUID, error) {
	if u == nil {
		return uuid.Nil, errors.New("uuid required")
	}
	v := strings.TrimSpace(u.GetValue())
	if v == "" {
		return uuid.Nil, errors.New("uuid required")
	}
	parsed, err := uuid.Parse(v)
	if err != nil {
		return uuid.Nil, errors.New("invalid uuid: " + v)
	}
	return parsed, nil
}

func pageOpts(p *commonv1.PageRequest) (limit, offset int) {
	const defaultLimit, maxLimit = 25, 200
	limit = defaultLimit
	if p != nil && p.GetPageSize() > 0 {
		limit = int(p.GetPageSize())
		if limit > maxLimit {
			limit = maxLimit
		}
	}
	// PageToken is treated as a numeric offset for now; once we add
	// keyset pagination this becomes a base64-encoded cursor.
	return limit, 0
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// tierFromString maps the textual tier stored in the DB to the proto enum.
// Unknown tiers fall back to UNSPECIFIED so the proxy can decide whether
// to deny or treat as a permissive default.
func tierFromString(s string) billingv1.SubscriptionTier {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "PAYG":
		return billingv1.SubscriptionTier_SUBSCRIPTION_TIER_PAYG
	case "STARTER":
		return billingv1.SubscriptionTier_SUBSCRIPTION_TIER_STARTER
	case "GROWTH":
		return billingv1.SubscriptionTier_SUBSCRIPTION_TIER_GROWTH
	case "ENTERPRISE":
		return billingv1.SubscriptionTier_SUBSCRIPTION_TIER_ENTERPRISE
	default:
		return billingv1.SubscriptionTier_SUBSCRIPTION_TIER_UNSPECIFIED
	}
}

// consumerAccountNumberRe: exactly 16 digits — the format account.ts's
// generateAccountNumber produces (#690, the Mullvad model per #569).
var consumerAccountNumberRe = regexp.MustCompile(`^[0-9]{16}$`)

// consumerWorkspaceNamespace seeds the deterministic UUIDv5 derivation of
// a consumer account's synthetic workspace id. Fixed forever — changing
// it would orphan every registered consumer account.
var consumerWorkspaceNamespace = uuid.MustParse("6f3b27aa-9b41-5f10-8d6e-1c2a90b4e8d3")

// deriveConsumerWorkspaceID maps an account number onto its synthetic
// workspace id (UUIDv5-style SHA1 derivation). Deterministic forever —
// the pinned test vector guards the namespace.
func deriveConsumerWorkspaceID(accountNumber string) uuid.UUID {
	return uuid.NewSHA1(consumerWorkspaceNamespace, []byte(accountNumber))
}

// RegisterConsumerAccount accepts a CLIENT-generated 16-digit account
// number on first use (#690 D1). Idempotent: an already-registered
// number returns its existing identity with created=false. The account
// row reuses the api_key table — key_hash = sha256(account_number),
// workspace_id = UUIDv5(namespace, account_number) so the NOT NULL
// constraint holds without a migration and per-workspace quota tracking
// works unchanged. Tier stays UNSPECIFIED = vpn-svc's free tier
// (isFreeTier, #548). Per-IP mint rate-limiting lives at the EDGE
// caller (vpn-svc's mobile endpoint owns the client IP); this handler
// enforces format + idempotency only.
func (h *ApiKeyHandler) RegisterConsumerAccount(
	ctx context.Context,
	req *connect.Request[billingv1.RegisterConsumerAccountRequest],
) (*connect.Response[billingv1.RegisterConsumerAccountResponse], error) {
	num := strings.TrimSpace(req.Msg.GetAccountNumber())
	if !consumerAccountNumberRe.MatchString(num) {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("account_number must be exactly 16 digits"))
	}
	keyHash := hashKey(num)

	// Idempotent fast-path: already registered.
	if row, err := h.Store.LookupApiKeyByHash(ctx, keyHash); err == nil {
		return connect.NewResponse(&billingv1.RegisterConsumerAccountResponse{
			CustomerId: &commonv1.UUID{Value: row.WorkspaceID.String()},
			Tier:       tierFromString(row.Tier),
			Created:    false,
		}), nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	wsID := deriveConsumerWorkspaceID(num)
	k := store.ApiKey{
		WorkspaceID: wsID,
		Label:       "consumer:self-registered",
		KeyHash:     keyHash,
		LastFour:    num[len(num)-4:],
		Tier:        billingv1.SubscriptionTier_SUBSCRIPTION_TIER_UNSPECIFIED.String(),
	}
	if err := h.Store.InsertApiKey(ctx, k); err != nil {
		// A concurrent register can win the race — resolve idempotently.
		if row, lerr := h.Store.LookupApiKeyByHash(ctx, keyHash); lerr == nil {
			return connect.NewResponse(&billingv1.RegisterConsumerAccountResponse{
				CustomerId: &commonv1.UUID{Value: row.WorkspaceID.String()},
				Tier:       tierFromString(row.Tier),
				Created:    false,
			}), nil
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&billingv1.RegisterConsumerAccountResponse{
		CustomerId: &commonv1.UUID{Value: wsID.String()},
		Tier:       billingv1.SubscriptionTier_SUBSCRIPTION_TIER_UNSPECIFIED,
		Created:    true,
	}), nil
}
