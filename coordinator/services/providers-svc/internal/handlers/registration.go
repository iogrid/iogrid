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
	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/geoip"
	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/store"
)

// RegistrationHandler implements the ProviderRegistrationService gRPC.
type RegistrationHandler struct {
	providersv1connect.UnimplementedProviderRegistrationServiceHandler
	Store store.Store
	CA    *ca.CA
	// GeoIP resolves the observed source IP into country/region for the
	// providers row. NEVER nil at runtime — main wires either the .mmdb-
	// backed reader or a NoopLookuper (see #359). The handler treats a
	// noop / miss as "leave geo columns alone" and proceeds; pairing
	// must succeed even when the database is missing.
	GeoIP geoip.Lookuper
	Log   *slog.Logger
}

// NewRegistrationHandler wires the dependencies. CA must be non-nil; the
// store may be the in-memory or pg-backed implementation. GeoIP may be
// nil — in that case we substitute geoip.NoopLookuper so the handler
// never has to nil-check on the hot path.
func NewRegistrationHandler(s store.Store, c *ca.CA, g geoip.Lookuper, log *slog.Logger) *RegistrationHandler {
	if log == nil {
		log = slog.Default()
	}
	if g == nil {
		g = geoip.NoopLookuper{}
	}
	return &RegistrationHandler{Store: s, CA: c, GeoIP: g, Log: log}
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
	netInfo := networkFromProto(in.GetNetworkInfo())
	// #359: country/region are SERVER-DERIVED from the observed source
	// IP — daemons may not supply their own. We pull the IP from the
	// Connect request's X-Forwarded-For (set by Traefik), fall back to
	// X-Real-IP, and finally to the peer addr. The lookup is best-
	// effort: any error or miss leaves the geo fields blank rather than
	// blocking pairing.
	clientIP := extractClientIP(req)
	if clientIP != "" {
		// Trust the OBSERVED IP over whatever the daemon put in the
		// proto — the daemon's self-reported public_ip is advisory and
		// can be wrong (the daemon may sit behind a different egress
		// than it thinks).
		netInfo.PublicIP = clientIP
		if res, err := h.GeoIP.Lookup(clientIP); err == nil {
			netInfo.CountryCode = res.CountryCode
			netInfo.RegionName = res.RegionName
			netInfo.RegionSlug = res.RegionSlug
			h.Log.Debug("geoip lookup ok",
				slog.String("public_ip", clientIP),
				slog.String("country_code", res.CountryCode),
				slog.String("region_slug", res.RegionSlug),
			)
		} else if !errors.Is(err, geoip.ErrNotFound) && !errors.Is(err, geoip.ErrUnavailable) {
			// Real error (malformed input, db read failure) — log but
			// don't fail the pair: a missing geo cell is recoverable on
			// the next heartbeat.
			h.Log.Warn("geoip lookup failed",
				slog.String("public_ip", clientIP),
				slog.String("error", err.Error()),
			)
		}
	}
	now := time.Now().UTC()
	p := &store.Provider{
		OwnerUserID:  pt.OwnerUserID,
		DisplayName:  display,
		Status:       store.StatusActive,
		HostInfo:     hostInfoFromProto(in.GetHostInfo()),
		NetworkInfo:  netInfo,
		Capabilities: store.Capability{}, // populated by UpdateCapabilityInventory
		PublicKey:    append([]byte(nil), in.GetDaemonPublicKey()...),
		RegisteredAt: now,
		LastSeenAt:   now,
	}

	// Re-pair dedupe — two layers, SPKI-first (#502).
	//
	// Layer 1: (owner_user_id, public_key). The daemon now persists its
	// keypair across `iogridd pair` calls (matching daemon-side fix),
	// so the SubjectPublicKey is a STABLE machine identity that survives
	// macOS hostname drift (Bonjour `-2`/`-3` neighbour collisions,
	// cold-boot `localhost`, System-Preferences renames, post-reimage
	// factory reset). Match → UPDATE in-place, refreshing display_name
	// to the current hostname so admin chrome stays accurate. Provider
	// id is preserved.
	//
	// Layer 2: (owner_user_id, display_name) — legacy back-compat for
	// older daemons that still mint a fresh keypair on every pair (or
	// for the first pair of a brand-new daemon that has just been
	// upgraded but hasn't yet flushed its old key.pem). Same UPDATE-in-
	// place semantics as before, including PublicKey overwrite.
	//
	// Layer 3: CreateProvider — only when BOTH layers miss. That's a
	// genuine first-pair (new owner OR `rm -rf ~/.iogrid` wiped the
	// keypair on purpose).
	//
	// Without layer 1, every macOS hostname drift produces a fresh
	// CreateProvider with a new UUID — the "Hatice's Mac registered
	// under cac83611 instead of 808ce330" recurrence chased manually
	// 3+ times before #502.
	matched := false
	if existing, err := h.Store.GetProviderByOwnerAndPublicKey(ctx, pt.OwnerUserID, in.GetDaemonPublicKey()); err == nil && existing != nil {
		existing.DisplayName = display
		existing.HostInfo = p.HostInfo
		existing.NetworkInfo = p.NetworkInfo
		existing.PublicKey = p.PublicKey
		existing.Status = store.StatusActive
		existing.LastSeenAt = now
		if err := h.Store.UpdateProvider(ctx, existing); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		p = existing
		matched = true
		h.Log.Info("daemon re-paired (SPKI match)",
			slog.String("provider_id", p.ID),
			slog.String("owner_user_id", p.OwnerUserID),
			slog.String("display_name", p.DisplayName),
		)
	}
	if !matched && in.GetDisplayName() != "" {
		if existing, err := h.Store.GetProviderByOwnerAndDisplayName(ctx, pt.OwnerUserID, display); err == nil && existing != nil {
			existing.HostInfo = p.HostInfo
			existing.NetworkInfo = p.NetworkInfo
			existing.PublicKey = p.PublicKey
			existing.Status = store.StatusActive
			existing.LastSeenAt = now
			if err := h.Store.UpdateProvider(ctx, existing); err != nil {
				return nil, connect.NewError(connect.CodeInternal, err)
			}
			p = existing
			matched = true
			h.Log.Info("daemon re-paired (display_name match — legacy)",
				slog.String("provider_id", p.ID),
				slog.String("owner_user_id", p.OwnerUserID),
				slog.String("display_name", p.DisplayName),
			)
		}
	}
	if !matched {
		if err := h.Store.CreateProvider(ctx, p); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
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
	if hi := req.Msg.GetHostInfo(); hi != nil {
		p.HostInfo = hostInfoFromProto(hi)
	}
	if n := req.Msg.GetNetworkInfo(); n != nil {
		p.NetworkInfo = networkFromProto(n)
	}
	// #359: same server-side override on UpdateHostInfo so a daemon's
	// egress relocation (laptop moved to a new network) refreshes the
	// geo columns. Same fail-soft policy as PairDaemon.
	clientIP := extractClientIP(req)
	if clientIP != "" {
		p.NetworkInfo.PublicIP = clientIP
		if res, err := h.GeoIP.Lookup(clientIP); err == nil {
			p.NetworkInfo.CountryCode = res.CountryCode
			p.NetworkInfo.RegionName = res.RegionName
			p.NetworkInfo.RegionSlug = res.RegionSlug
		} else if !errors.Is(err, geoip.ErrNotFound) && !errors.Is(err, geoip.ErrUnavailable) {
			h.Log.Warn("geoip lookup failed on UpdateHostInfo",
				slog.String("provider_id", id),
				slog.String("public_ip", clientIP),
				slog.String("error", err.Error()),
			)
		}
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

// SetPrimaryProvider promotes one owned provider to the per-owner
// primary slot. Closes the multi-daemon ownership ambiguity introduced
// by #305 — when Hatice paired her real Mac alongside the manual-test
// daemon, gateway-bff's "first ACTIVE" pick was undefined. See #325.
//
// Authorization model: providers-svc trusts gateway-bff to set
// owner_user_id to the authenticated principal. We re-validate inside
// the SQL UPDATE (WHERE id = $1 AND owner_user_id = $2) so a malicious
// or buggy intermediate cannot promote a row owned by someone else.
// Mismatch returns PERMISSION_DENIED (not NOT_FOUND) so the BFF can
// surface a clean 403 without leaking row existence.
func (h *RegistrationHandler) SetPrimaryProvider(
	ctx context.Context,
	req *connect.Request[providersv1.SetPrimaryProviderRequest],
) (*connect.Response[providersv1.SetPrimaryProviderResponse], error) {
	owner := uuidString(req.Msg.GetOwnerUserId())
	if owner == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("owner_user_id required"))
	}
	pid := uuidString(req.Msg.GetProviderId())
	if pid == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("provider_id required"))
	}
	prov, err := h.Store.SetPrimaryProvider(ctx, owner, pid)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Caller doesn't own the requested id (or it doesn't exist).
			// Surface as PERMISSION_DENIED so non-owners can't probe.
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("caller does not own provider"))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	h.Log.Info("primary provider set",
		slog.String("owner_user_id", owner),
		slog.String("provider_id", pid),
	)
	return connect.NewResponse(&providersv1.SetPrimaryProviderResponse{
		Provider: providerToProto(prov),
	}), nil
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

// extractClientIP returns the most-trustworthy observed-source IP for a
// Connect-RPC request. Order of preference:
//
//  1. X-Forwarded-For (left-most) — what Traefik sets on every
//     ingress. This is the trusted chain because daemons cannot forge
//     it; Traefik replaces whatever the client supplied.
//  2. X-Real-Ip / X-Real-IP — also set by Traefik on some routes.
//  3. X-Forwarded-Remote-Addr — synthetic header populated by the
//     REST shim (PairDaemonREST) so the in-process Connect call can
//     still see the real RemoteAddr of the underlying HTTP request.
//  4. req.Peer().Addr — fallback for callers that ride directly over
//     the Connect surface without an intervening REST shim.
//
// See geoip.ExtractClientIP for the lower-level header walker; this
// wrapper just plumbs in the request-scoped headers + the synthetic
// REST hop hint.
func extractClientIP[T any](req *connect.Request[T]) string {
	getHeader := req.Header().Get
	if ip := geoip.ExtractClientIP(getHeader, ""); ip != "" {
		return ip
	}
	if syn := req.Header().Get("X-Forwarded-Remote-Addr"); syn != "" {
		return geoip.ExtractClientIP(nil, syn)
	}
	if peer := req.Peer(); peer.Addr != "" {
		return geoip.ExtractClientIP(nil, peer.Addr)
	}
	return ""
}
