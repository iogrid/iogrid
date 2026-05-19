package handlers

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	providersv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1/providersv1connect"
	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/ca"
	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/store"
)

// RegistrationHandler implements the ProviderRegistrationService gRPC.
type RegistrationHandler struct {
	providersv1connect.UnimplementedProviderRegistrationServiceHandler
	Store store.Store
	CA    *ca.CA
	Log   *slog.Logger
}

// NewRegistrationHandler wires the dependencies. CA must be non-nil; the
// store may be the in-memory or pg-backed implementation.
func NewRegistrationHandler(s store.Store, c *ca.CA, log *slog.Logger) *RegistrationHandler {
	if log == nil {
		log = slog.Default()
	}
	return &RegistrationHandler{Store: s, CA: c, Log: log}
}

// defaultPairingTTL is the issuance TTL applied when the caller passes
// ttl_seconds=0. Matches the value documented on the proto field.
const defaultPairingTTL = 10 * time.Minute

// maxPairingTTL caps how long any single token may live, regardless of
// what the caller requested. One hour is enough for slow human-driven
// install flows but short enough that a leaked token is bounded.
const maxPairingTTL = time.Hour

// IssuePairingToken mints a fresh, single-use pairing secret bound to a
// specific owner. The caller is expected to be gateway-bff, which has
// already verified that the authenticated principal equals owner_user_id
// before forwarding the request — providers-svc trusts that gate.
func (h *RegistrationHandler) IssuePairingToken(
	ctx context.Context,
	req *connect.Request[providersv1.IssuePairingTokenRequest],
) (*connect.Response[providersv1.IssuePairingTokenResponse], error) {
	owner := uuidString(req.Msg.GetOwnerUserId())
	if owner == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("owner_user_id required"))
	}
	ttl := time.Duration(req.Msg.GetTtlSeconds()) * time.Second
	if ttl <= 0 {
		ttl = defaultPairingTTL
	}
	if ttl > maxPairingTTL {
		ttl = maxPairingTTL
	}
	tok, err := h.Store.IssuePairingToken(ctx, owner, ttl)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	h.Log.Info("pairing token issued",
		slog.String("owner_user_id", owner),
		slog.Duration("ttl", ttl),
	)
	expiresAt := time.Now().UTC().Add(ttl)
	return connect.NewResponse(&providersv1.IssuePairingTokenResponse{
		PairingToken: tok,
		ExpiresAt:    timestamppb.New(expiresAt),
	}), nil
}

// PairDaemon consumes a one-time pairing token, persists a fresh Provider
// row keyed to its owner, signs a daemon client certificate, and returns
// both the certificate and the CA bundle the daemon should pin.
func (h *RegistrationHandler) PairDaemon(
	ctx context.Context,
	req *connect.Request[providersv1.PairDaemonRequest],
) (*connect.Response[providersv1.PairDaemonResponse], error) {
	in := req.Msg
	if in.GetPairingToken() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("pairing_token required"))
	}
	if len(in.GetDaemonPublicKey()) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("daemon_public_key required"))
	}

	pt, err := h.Store.ConsumePairingToken(ctx, in.GetPairingToken())
	if err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, err)
	}

	display := in.GetDisplayName()
	if display == "" {
		display = "provider-" + pt.OwnerUserID[:min(8, len(pt.OwnerUserID))]
	}
	p := &store.Provider{
		OwnerUserID:  pt.OwnerUserID,
		DisplayName:  display,
		Status:       store.StatusActive,
		HostInfo:     hostInfoFromProto(in.GetHostInfo()),
		NetworkInfo:  networkFromProto(in.GetNetworkInfo()),
		Capabilities: store.Capability{}, // populated by UpdateCapabilityInventory
		PublicKey:    append([]byte(nil), in.GetDaemonPublicKey()...),
		RegisteredAt: time.Now().UTC(),
		LastSeenAt:   time.Now().UTC(),
	}
	if err := h.Store.CreateProvider(ctx, p); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	leafPEM, err := h.CA.IssueDaemonCert(ca.IssueRequest{
		ProviderID:      p.ID,
		DaemonPublicKey: in.GetDaemonPublicKey(),
	})
	if err != nil {
		// Best effort cleanup so a failed pair doesn't leave an orphan.
		_ = h.Store.DeactivateProvider(ctx, p.ID, "cert issuance failed")
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	_ = h.Store.AppendAuditEvent(ctx, store.AuditEvent{
		ProviderID: p.ID,
		Kind:       "EVENT_KIND_SCHEDULER_TRANSITION",
		Metadata:   map[string]string{"transition": "paired"},
	})
	h.Log.Info("daemon paired",
		slog.String("provider_id", p.ID),
		slog.String("owner_user_id", p.OwnerUserID),
	)

	return connect.NewResponse(&providersv1.PairDaemonResponse{
		Provider:          providerToProto(p),
		DaemonCertificate: leafPEM,
		ServerCaBundle:    h.CA.Bundle(),
	}), nil
}

func (h *RegistrationHandler) UpdateHostInfo(
	ctx context.Context,
	req *connect.Request[providersv1.UpdateHostInfoRequest],
) (*connect.Response[providersv1.UpdateHostInfoResponse], error) {
	id := uuidString(req.Msg.GetProviderId())
	if id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("provider_id required"))
	}
	p, err := h.Store.GetProvider(ctx, id)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	if h := req.Msg.GetHostInfo(); h != nil {
		p.HostInfo = hostInfoFromProto(h)
	}
	if n := req.Msg.GetNetworkInfo(); n != nil {
		p.NetworkInfo = networkFromProto(n)
	}
	p.LastSeenAt = time.Now().UTC()
	if err := h.Store.UpdateProvider(ctx, p); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&providersv1.UpdateHostInfoResponse{Provider: providerToProto(p)}), nil
}

func (h *RegistrationHandler) UpdateCapabilityInventory(
	ctx context.Context,
	req *connect.Request[providersv1.UpdateCapabilityInventoryRequest],
) (*connect.Response[providersv1.UpdateCapabilityInventoryResponse], error) {
	id := uuidString(req.Msg.GetProviderId())
	if id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("provider_id required"))
	}
	p, err := h.Store.GetProvider(ctx, id)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	p.Capabilities = capabilityFromProto(req.Msg.GetCapabilities())
	p.LastSeenAt = time.Now().UTC()
	if err := h.Store.UpdateProvider(ctx, p); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&providersv1.UpdateCapabilityInventoryResponse{Provider: providerToProto(p)}), nil
}

func (h *RegistrationHandler) GetProvider(
	ctx context.Context,
	req *connect.Request[providersv1.GetProviderRequest],
) (*connect.Response[providersv1.GetProviderResponse], error) {
	id := uuidString(req.Msg.GetProviderId())
	if id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("provider_id required"))
	}
	p, err := h.Store.GetProvider(ctx, id)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	return connect.NewResponse(&providersv1.GetProviderResponse{Provider: providerToProto(p)}), nil
}

func (h *RegistrationHandler) ListProviders(
	ctx context.Context,
	req *connect.Request[providersv1.ListProvidersRequest],
) (*connect.Response[providersv1.ListProvidersResponse], error) {
	page := req.Msg.GetPage()
	opts := store.ListOptions{
		OwnerUserID: uuidString(req.Msg.GetOwnerUserId()),
		Status:      statusToStore(req.Msg.GetStatusFilter()),
	}
	if page != nil {
		opts.PageSize = int(page.GetPageSize())
		opts.PageToken = page.GetPageToken()
	}
	ps, next, err := h.Store.ListProviders(ctx, opts)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &providersv1.ListProvidersResponse{
		Providers: make([]*providersv1.Provider, 0, len(ps)),
		Page:      &commonv1.PageResponse{NextPageToken: next},
	}
	for _, p := range ps {
		out.Providers = append(out.Providers, providerToProto(p))
	}
	return connect.NewResponse(out), nil
}

func (h *RegistrationHandler) DeactivateProvider(
	ctx context.Context,
	req *connect.Request[providersv1.DeactivateProviderRequest],
) (*connect.Response[providersv1.DeactivateProviderResponse], error) {
	id := uuidString(req.Msg.GetProviderId())
	if id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("provider_id required"))
	}
	if err := h.Store.DeactivateProvider(ctx, id, req.Msg.GetReason()); err != nil {
		return nil, mapStoreErr(err)
	}
	return connect.NewResponse(&providersv1.DeactivateProviderResponse{}), nil
}

func mapStoreErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, store.ErrTokenInvalid):
		return connect.NewError(connect.CodePermissionDenied, err)
	case errors.Is(err, store.ErrAlreadyExists):
		return connect.NewError(connect.CodeAlreadyExists, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
