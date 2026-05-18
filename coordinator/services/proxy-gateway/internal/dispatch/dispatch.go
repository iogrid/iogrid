// Package dispatch picks a bandwidth provider for an inbound customer
// connection.
//
// In production the proxy-gateway calls workloads-svc.SubmitWorkload
// with a BandwidthRequest oneof (workload_type = bandwidth, geo_target,
// destination_country) and receives back an Assignment containing the
// chosen provider's WireGuard tunnel endpoint plus a short-lived session
// token. workloads-svc actually wires that up via the dispatch.proto
// long-lived bidi stream to the chosen daemon; from the proxy's POV the
// outcome is a (provider_id, endpoint, token) triple.
//
// This package is the proxy-side seam: a small Dispatcher interface +
// a Static implementation for tests + an in-memory pool that picks the
// next eligible provider for failover. The Connect-RPC implementation
// that talks to a live workloads-svc is wired in by main once
// WORKLOADS_SVC_URL is set; for the SOCKS5/HTTP-CONNECT integration
// tests an in-memory pool of dial-able echo servers is plenty.
package dispatch

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/durationpb"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	workloadsv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/workloads/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/workloads/v1/workloadsv1connect"
)

// ErrNoEligibleProvider is returned when no provider matches the filters.
var ErrNoEligibleProvider = errors.New("no eligible provider")

// Request captures everything dispatch needs to pick a provider.
type Request struct {
	CustomerID         string
	WorkspaceID        string
	SessionID          string
	GeoTarget          string
	DestinationHost    string
	DestinationPort    uint32
	DestinationCountry string
	Category           string
	// StickyProviderID, when non-empty, is honoured if it's still eligible
	// (we round-trip through workloads-svc but pass the hint to it).
	StickyProviderID string
	// Excluded providers (set by failover loop after a provider drops).
	Excluded map[string]struct{}
}

// Assignment is what dispatch returns to the proxy.
type Assignment struct {
	// ProviderID — UUID of the chosen provider; used for sticky-session
	// pinning and audit/billing emission.
	ProviderID string
	// Endpoint — TCP address the proxy dials to forward bytes. In the
	// final WireGuard wiring this is the WG-tunnel-side relay endpoint
	// inside the coordinator's mesh; for the customer-facing seam the
	// proxy treats it as an opaque dialable host:port string.
	Endpoint string
	// SessionToken — short-lived JWT/opaque-string proving dispatch
	// authorised this attempt. The provider daemon checks it on accept.
	SessionToken string
	// WorkloadID — UUID assigned by workloads-svc; flows into the
	// metering BillingEvent for cross-reference.
	WorkloadID string
	// AttemptID — UUID for the per-attempt audit trail.
	AttemptID string
	// ExpiresAt — deadline after which the proxy must abort + re-dispatch.
	ExpiresAt time.Time
}

// Dispatcher is the contract.
type Dispatcher interface {
	// Dispatch returns an Assignment or an error. ErrNoEligibleProvider
	// is the canonical "tried everyone we could, nothing matched" signal.
	Dispatch(ctx context.Context, req Request) (*Assignment, error)
}

// ProviderEntry is an in-memory test pool entry.
type ProviderEntry struct {
	ID       string
	Endpoint string
	Geo      string
	Online   bool
	Token    string
}

// StaticPool is a Dispatcher for tests + local dev. It walks the entries
// in deterministic order, skipping Excluded + offline providers, and
// preferring the StickyProviderID when present.
type StaticPool struct {
	mu      sync.Mutex
	entries []ProviderEntry
	// rrCursor advances on each Dispatch to spread load.
	rrCursor int
}

// NewStaticPool returns a StaticPool seeded with entries.
func NewStaticPool(entries []ProviderEntry) *StaticPool {
	return &StaticPool{entries: append([]ProviderEntry(nil), entries...)}
}

// Add appends a new entry.
func (p *StaticPool) Add(e ProviderEntry) {
	p.mu.Lock()
	p.entries = append(p.entries, e)
	p.mu.Unlock()
}

// SetOnline flips an entry's Online state.
func (p *StaticPool) SetOnline(id string, online bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.entries {
		if p.entries[i].ID == id {
			p.entries[i].Online = online
			return
		}
	}
}

// Dispatch implements Dispatcher.
func (p *StaticPool) Dispatch(_ context.Context, req Request) (*Assignment, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pick := func(e ProviderEntry) *Assignment {
		return &Assignment{
			ProviderID:   e.ID,
			Endpoint:     e.Endpoint,
			SessionToken: e.Token,
			WorkloadID:   "wl-" + req.SessionID,
			AttemptID:    "att-" + e.ID + "-" + req.SessionID,
			ExpiresAt:    time.Now().Add(30 * time.Minute),
		}
	}
	// 1. Sticky preferred.
	if req.StickyProviderID != "" {
		for _, e := range p.entries {
			if e.ID != req.StickyProviderID {
				continue
			}
			if !e.Online {
				break
			}
			if _, skip := req.Excluded[e.ID]; skip {
				break
			}
			if req.GeoTarget != "" && e.Geo != "" && !strings.EqualFold(e.Geo, req.GeoTarget) {
				break
			}
			return pick(e), nil
		}
	}
	// 2. Geo-targeted round-robin.
	n := len(p.entries)
	if n == 0 {
		return nil, ErrNoEligibleProvider
	}
	for i := 0; i < n; i++ {
		idx := (p.rrCursor + i) % n
		e := p.entries[idx]
		if !e.Online {
			continue
		}
		if _, skip := req.Excluded[e.ID]; skip {
			continue
		}
		if req.GeoTarget != "" && e.Geo != "" && !strings.EqualFold(e.Geo, req.GeoTarget) {
			continue
		}
		p.rrCursor = (idx + 1) % n
		return pick(e), nil
	}
	return nil, ErrNoEligibleProvider
}

// ConnectDispatcher calls workloads-svc.SubmitWorkload over Connect-RPC
// with a BandwidthRequest payload. It expects the response Workload's
// labels map to carry the chosen provider id + endpoint + session token
// — workloads-svc handles the actual provider selection internally; the
// proxy is happy to receive whatever it picked.
type ConnectDispatcher struct {
	Client workloadsv1connect.WorkloadSubmissionServiceClient
	// DefaultBudget — placeholder Money cap submitted with every request
	// when the customer hasn't set one explicitly. Zero == no cap.
	DefaultBudget *commonv1.Money
	// Timeout applied to each SubmitWorkload call.
	Timeout time.Duration
}

// NewConnectDispatcher returns a Dispatcher talking to workloads-svc.
func NewConnectDispatcher(baseURL string, httpClient connect.HTTPClient) *ConnectDispatcher {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &ConnectDispatcher{
		Client:  workloadsv1connect.NewWorkloadSubmissionServiceClient(httpClient, baseURL),
		Timeout: 10 * time.Second,
	}
}

// Dispatch implements Dispatcher.
func (d *ConnectDispatcher) Dispatch(ctx context.Context, req Request) (*Assignment, error) {
	if d == nil || d.Client == nil {
		return nil, errors.New("workloads dispatcher nil")
	}
	if d.Timeout > 0 {
		c, cancel := context.WithTimeout(ctx, d.Timeout)
		defer cancel()
		ctx = c
	}
	wl := &workloadsv1.Workload{
		Type:     commonv1.WorkloadType_WORKLOAD_TYPE_BANDWIDTH,
		Priority: workloadsv1.WorkloadPriority_WORKLOAD_PRIORITY_NORMAL,
		Labels: map[string]string{
			"sticky_provider_hint": req.StickyProviderID,
			"destination_country":  req.DestinationCountry,
		},
		Payload: &workloadsv1.Workload_Bandwidth{
			Bandwidth: &workloadsv1.BandwidthRequest{
				TargetUrl:       "tcp://" + req.DestinationHost,
				SessionId:       req.SessionID,
				Category:        req.Category,
				PreferredRegion: regionFromString(req.GeoTarget),
				MaxSpend:        d.DefaultBudget,
			},
		},
	}
	if req.WorkspaceID != "" {
		wl.WorkspaceId = &commonv1.UUID{Value: req.WorkspaceID}
	}
	resp, err := d.Client.SubmitWorkload(ctx, connect.NewRequest(&workloadsv1.SubmitWorkloadRequest{Workload: wl}))
	if err != nil {
		return nil, err
	}
	out := resp.Msg.GetWorkload()
	if out == nil {
		return nil, errors.New("workloads-svc returned empty workload")
	}
	labels := out.Labels
	if labels == nil {
		return nil, errors.New("workloads-svc returned no dispatch labels")
	}
	asg := &Assignment{
		ProviderID:   labels["dispatched_provider_id"],
		Endpoint:     labels["dispatched_provider_endpoint"],
		SessionToken: labels["dispatched_session_token"],
		AttemptID:    labels["dispatched_attempt_id"],
		ExpiresAt:    time.Now().Add(30 * time.Minute),
	}
	if out.Id != nil {
		asg.WorkloadID = out.Id.Value
	}
	if asg.ProviderID == "" || asg.Endpoint == "" {
		return nil, ErrNoEligibleProvider
	}
	_ = durationpb.New(0) // keep import for future deadline plumbing
	return asg, nil
}

// regionFromString builds a Region message from a slug. CountryCode is
// inferred from the slug prefix; workloads-svc resolves the final
// region routing using the slug as the authoritative key.
func regionFromString(s string) *commonv1.Region {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return nil
	}
	cc := ""
	switch {
	case strings.HasPrefix(s, "us"):
		cc = "US"
	case strings.HasPrefix(s, "eu"):
		cc = "EU"
	case strings.HasPrefix(s, "ap"), strings.HasPrefix(s, "apac"), strings.HasPrefix(s, "asia"):
		cc = "AP"
	}
	return &commonv1.Region{Slug: s, CountryCode: cc}
}
