package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	providersv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1/providersv1connect"
	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/geoip"
	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/store"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// SchedulingHandler implements the SchedulingService gRPC.
type SchedulingHandler struct {
	providersv1connect.UnimplementedSchedulingServiceHandler
	Store store.Store
	// GeoIP resolves the observed source IP of a heartbeat stream into
	// country/region for the providers row. NEVER nil at runtime — main
	// wires either the .mmdb-backed reader or geoip.NoopLookuper. Same
	// fail-soft policy as RegistrationHandler.
	GeoIP geoip.Lookuper
	Log   *slog.Logger

	// liveStates is the most-recent SchedulerState reported by each daemon
	// via StreamHeartbeats. GetCurrentState reads from this map.
	stateMu    sync.RWMutex
	liveStates map[string]liveState
}

type liveState struct {
	State providersv1.SchedulerState
	Usage *providersv1.CurrentUsageSnapshot
	Seq   uint64
	At    time.Time
	// LastGeoLookupAt throttles the per-stream geoip refresh. #359 wants
	// the country/region columns to repopulate when a provider's egress
	// shifts (ISP renumber, laptop on a new wifi), but doing a maxmind
	// lookup + UPDATE on every 5-second heartbeat is wasteful. Refresh
	// once per heartbeatGeoRefreshInterval per provider.
	LastGeoLookupAt time.Time
}

// heartbeatGeoRefreshInterval is how often we re-run the geoip lookup
// on the heartbeat path. 24h matches the cadence the issue describes
// ("CGN may shift"); shorter cadences add write pressure without
// improving the operator-visible signal.
const heartbeatGeoRefreshInterval = 24 * time.Hour

// NewSchedulingHandler wires the store dependency. GeoIP may be nil —
// we substitute geoip.NoopLookuper so the hot path never has to
// nil-check.
func NewSchedulingHandler(s store.Store, g geoip.Lookuper, log *slog.Logger) *SchedulingHandler {
	if log == nil {
		log = slog.Default()
	}
	if g == nil {
		g = geoip.NoopLookuper{}
	}
	return &SchedulingHandler{Store: s, GeoIP: g, Log: log, liveStates: make(map[string]liveState)}
}

func (h *SchedulingHandler) GetSchedulingConfig(
	ctx context.Context,
	req *connect.Request[providersv1.GetSchedulingConfigRequest],
) (*connect.Response[providersv1.GetSchedulingConfigResponse], error) {
	id := uuidString(req.Msg.GetProviderId())
	if id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("provider_id required"))
	}
	cfg, err := h.Store.GetSchedulingConfig(ctx, id)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	return connect.NewResponse(&providersv1.GetSchedulingConfigResponse{Config: schedulingConfigToProto(cfg)}), nil
}

// UpdateSchedulingConfig replaces the desired-state for one provider. The
// proto contract says "full replacement", so we validate the whole config
// before writing.
func (h *SchedulingHandler) UpdateSchedulingConfig(
	ctx context.Context,
	req *connect.Request[providersv1.UpdateSchedulingConfigRequest],
) (*connect.Response[providersv1.UpdateSchedulingConfigResponse], error) {
	in := req.Msg.GetConfig()
	if in == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("config required"))
	}
	id := uuidString(in.GetProviderId())
	if id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("config.provider_id required"))
	}
	cfg, err := schedulingConfigFromProto(in)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := validateConfig(cfg); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	saved, err := h.Store.UpdateSchedulingConfig(ctx, cfg)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	return connect.NewResponse(&providersv1.UpdateSchedulingConfigResponse{Config: schedulingConfigToProto(saved)}), nil
}

// GetCurrentState reads the most recent state observed via StreamHeartbeats.
// If no daemon has ever connected, we report ACTIVE — the provider just
// hasn't checked in yet.
func (h *SchedulingHandler) GetCurrentState(
	ctx context.Context,
	req *connect.Request[providersv1.GetCurrentStateRequest],
) (*connect.Response[providersv1.GetCurrentStateResponse], error) {
	id := uuidString(req.Msg.GetProviderId())
	if id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("provider_id required"))
	}
	h.stateMu.RLock()
	ls, ok := h.liveStates[id]
	h.stateMu.RUnlock()
	if !ok {
		return connect.NewResponse(&providersv1.GetCurrentStateResponse{
			State: providersv1.SchedulerState_SCHEDULER_STATE_ACTIVE,
		}), nil
	}
	return connect.NewResponse(&providersv1.GetCurrentStateResponse{
		State: ls.State,
		Usage: ls.Usage,
	}), nil
}

// StreamHeartbeats is the bidi stream a daemon holds open. Each Heartbeat
// from the daemon updates our liveStates map; the ack flows the other
// direction with the latest config-change flag.
func (h *SchedulingHandler) StreamHeartbeats(
	ctx context.Context,
	stream *connect.BidiStream[providersv1.Heartbeat, providersv1.HeartbeatAck],
) error {
	// Extract the observed source IP from the stream's request header
	// once at stream-open and reuse it for every frame — the IP is
	// pinned for the lifetime of the TCP connection (Traefik does not
	// rebalance mid-stream). Used by the #359 geoip refresh path
	// below.
	clientIP := geoip.ExtractClientIP(stream.RequestHeader().Get, "")
	for {
		hb, err := stream.Receive()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		id := uuidString(hb.GetProviderId())
		if id == "" {
			return connect.NewError(connect.CodeInvalidArgument, errors.New("provider_id required"))
		}
		now := time.Now().UTC()
		h.stateMu.Lock()
		prev := h.liveStates[id]
		next := liveState{
			State:           hb.GetState(),
			Usage:           hb.GetUsage(),
			Seq:             hb.GetSequence(),
			At:              now,
			LastGeoLookupAt: prev.LastGeoLookupAt,
		}
		// #359: rate-limit the geoip refresh to once per
		// heartbeatGeoRefreshInterval per provider. The very first
		// frame (LastGeoLookupAt zero) always triggers a refresh so a
		// provider that paired before the .mmdb was loaded gets its
		// country/region populated as soon as it first connects.
		shouldRefreshGeo := clientIP != "" &&
			(prev.LastGeoLookupAt.IsZero() || now.Sub(prev.LastGeoLookupAt) >= heartbeatGeoRefreshInterval)
		if shouldRefreshGeo {
			next.LastGeoLookupAt = now
		}
		h.liveStates[id] = next
		h.stateMu.Unlock()

		// #311: bump providers.last_seen_at so /admin/providers + every
		// downstream "is this daemon alive?" check reflects reality. Was
		// missing — the row stayed frozen at registered_at, so paired
		// daemons looked offline forever even when the daemon was alive
		// and pushing heartbeats over the wire. Failure here is
		// non-fatal: the stream stays open and the next heartbeat (~5s)
		// retries.
		if err := h.Store.UpdateLastSeen(ctx, id, now); err != nil {
			h.Log.Warn("heartbeat: update last_seen_at failed",
				slog.String("provider_id", id),
				slog.String("error", err.Error()),
			)
		}

		// #359: refresh the geo columns when the stream's observed IP
		// resolves cleanly and our throttle window has elapsed. Read-
		// modify-write through GetProvider/UpdateProvider so we don't
		// stomp the host info another path (UpdateHostInfo) may have
		// written in parallel. Failure is non-fatal: a stale geo cell
		// is recoverable on the next 24h tick.
		if shouldRefreshGeo {
			res, err := h.GeoIP.Lookup(clientIP)
			switch {
			case err == nil:
				if err := h.applyHeartbeatGeo(ctx, id, clientIP, res); err != nil {
					h.Log.Warn("heartbeat: apply geo failed",
						slog.String("provider_id", id),
						slog.String("error", err.Error()),
					)
				}
			case errors.Is(err, geoip.ErrNotFound), errors.Is(err, geoip.ErrUnavailable):
				// soft miss — skip
			default:
				h.Log.Warn("heartbeat: geoip lookup failed",
					slog.String("provider_id", id),
					slog.String("public_ip", clientIP),
					slog.String("error", err.Error()),
				)
			}
		}

		// Emit an audit event on state transitions so the transparency
		// feed reflects "scheduler paused because bandwidth cap reached".
		if prev.State != hb.GetState() {
			_ = h.Store.AppendAuditEvent(ctx, store.AuditEvent{
				ProviderID: id,
				Kind:       "EVENT_KIND_SCHEDULER_TRANSITION",
				Metadata: map[string]string{
					"from": prev.State.String(),
					"to":   hb.GetState().String(),
				},
			})
		}

		if err := stream.Send(&providersv1.HeartbeatAck{}); err != nil {
			return err
		}
	}
}

// applyHeartbeatGeo refreshes the provider row's geo columns from a
// fresh geoip.Lookup result. Skips the UPDATE entirely when nothing
// changed so we don't churn writes on a stable IP. Returns the
// underlying store error verbatim so the caller can decide whether to
// log; never wraps in a Connect code (this is a side-channel refresh,
// not on the request path).
func (h *SchedulingHandler) applyHeartbeatGeo(ctx context.Context, providerID, ip string, res geoip.Result) error {
	p, err := h.Store.GetProvider(ctx, providerID)
	if err != nil {
		return err
	}
	changed := false
	if p.NetworkInfo.PublicIP != ip {
		p.NetworkInfo.PublicIP = ip
		changed = true
	}
	if p.NetworkInfo.CountryCode != res.CountryCode {
		p.NetworkInfo.CountryCode = res.CountryCode
		changed = true
	}
	if p.NetworkInfo.RegionName != res.RegionName {
		p.NetworkInfo.RegionName = res.RegionName
		changed = true
	}
	if p.NetworkInfo.RegionSlug != res.RegionSlug {
		p.NetworkInfo.RegionSlug = res.RegionSlug
		changed = true
	}
	if !changed {
		return nil
	}
	if err := h.Store.UpdateProvider(ctx, p); err != nil {
		return err
	}
	h.Log.Info("heartbeat: geo refreshed",
		slog.String("provider_id", providerID),
		slog.String("public_ip", ip),
		slog.String("country_code", res.CountryCode),
		slog.String("region_slug", res.RegionSlug),
	)
	return nil
}

// --- conversion + validation -----------------------------------------------

func schedulingConfigFromProto(in *providersv1.SchedulingConfig) (*store.SchedulingConfig, error) {
	out := &store.SchedulingConfig{
		ProviderID:      uuidString(in.GetProviderId()),
		UpdatedByUserID: uuidString(in.GetUpdatedByUserId()),
	}
	if caps := in.GetCaps(); caps != nil {
		out.BandwidthCapGB = caps.GetBandwidthCapGbPerMonth()
		out.CPUCapPct = caps.GetCpuCapPercent()
		out.MemoryCapPct = caps.GetMemoryCapPercent()
		out.GPUCapWhenIdlePct = caps.GetGpuCapPercentWhenIdle()
		out.GPUCapWhenActivePct = caps.GetGpuCapPercentWhenActive()
	}
	if cal := in.GetCalendar(); cal != nil {
		for _, w := range cal.GetWindows() {
			out.CalendarWindows = append(out.CalendarWindows, store.CalendarWindow{
				DaysOfWeek: append([]uint32(nil), w.GetDaysOfWeek()...),
				StartLocal: w.GetStartLocalTime(),
				EndLocal:   w.GetEndLocalTime(),
				Timezone:   w.GetTimezone(),
			})
		}
	}
	if idle := in.GetIdle(); idle != nil {
		out.IdleEnabled = idle.GetEnabled()
		out.IdleThresholdSecs = idle.GetIdleThresholdSeconds()
	}
	if cat := in.GetCategoryOptIn(); cat != nil {
		out.AllowedCategories = append([]string(nil), cat.GetAllowedCategories()...)
		out.DisallowedCategories = append([]string(nil), cat.GetDisallowedCategories()...)
	}
	if dp := in.GetDestinationPolicy(); dp != nil {
		out.DestinationBlocklist = append([]string(nil), dp.GetDestinationBlocklist()...)
		out.PerCustomerMinutesCap = dp.GetPerCustomerMinutesCap()
	}
	return out, nil
}

func schedulingConfigToProto(c *store.SchedulingConfig) *providersv1.SchedulingConfig {
	out := &providersv1.SchedulingConfig{
		ProviderId: &commonv1.UUID{Value: c.ProviderID},
		Caps: &providersv1.ResourceCaps{
			BandwidthCapGbPerMonth:    c.BandwidthCapGB,
			CpuCapPercent:             c.CPUCapPct,
			MemoryCapPercent:          c.MemoryCapPct,
			GpuCapPercentWhenIdle:     c.GPUCapWhenIdlePct,
			GpuCapPercentWhenActive:   c.GPUCapWhenActivePct,
		},
		Idle: &providersv1.IdleDetection{
			Enabled:              c.IdleEnabled,
			IdleThresholdSeconds: c.IdleThresholdSecs,
		},
		CategoryOptIn: &providersv1.CategoryOptIn{
			AllowedCategories:    append([]string(nil), c.AllowedCategories...),
			DisallowedCategories: append([]string(nil), c.DisallowedCategories...),
		},
		DestinationPolicy: &providersv1.DestinationPolicy{
			DestinationBlocklist:  append([]string(nil), c.DestinationBlocklist...),
			PerCustomerMinutesCap: c.PerCustomerMinutesCap,
		},
		UpdatedAt: timestamppb.New(c.UpdatedAt),
	}
	if c.UpdatedByUserID != "" {
		out.UpdatedByUserId = &commonv1.UUID{Value: c.UpdatedByUserID}
	}
	cal := &providersv1.CalendarSchedule{}
	for _, w := range c.CalendarWindows {
		cal.Windows = append(cal.Windows, &providersv1.CalendarWindow{
			DaysOfWeek:     append([]uint32(nil), w.DaysOfWeek...),
			StartLocalTime: w.StartLocal,
			EndLocalTime:   w.EndLocal,
			Timezone:       w.Timezone,
		})
	}
	out.Calendar = cal
	return out
}

// validateConfig enforces the documented bounds from docs/TECH.md. Calls
// out the field name in the error so the UI can highlight it.
func validateConfig(c *store.SchedulingConfig) error {
	if c.BandwidthCapGB > 100000 {
		return errors.New("bandwidth_cap_gb_per_month exceeds 100 TB sanity ceiling")
	}
	if c.CPUCapPct > 100 {
		return fmt.Errorf("cpu_cap_percent %d > 100", c.CPUCapPct)
	}
	if c.MemoryCapPct > 100 {
		return fmt.Errorf("memory_cap_percent %d > 100", c.MemoryCapPct)
	}
	if c.GPUCapWhenIdlePct > 100 {
		return fmt.Errorf("gpu_cap_percent_when_idle %d > 100", c.GPUCapWhenIdlePct)
	}
	if c.GPUCapWhenActivePct > 100 {
		return fmt.Errorf("gpu_cap_percent_when_active %d > 100", c.GPUCapWhenActivePct)
	}
	if c.IdleThresholdSecs > 24*60*60 {
		return errors.New("idle_threshold_seconds must be ≤ 86400 (1 day)")
	}
	for i, w := range c.CalendarWindows {
		if err := validateTimeHHMM(w.StartLocal); err != nil {
			return fmt.Errorf("windows[%d].start_local_time: %w", i, err)
		}
		if err := validateTimeHHMM(w.EndLocal); err != nil {
			return fmt.Errorf("windows[%d].end_local_time: %w", i, err)
		}
		for _, d := range w.DaysOfWeek {
			if d < 1 || d > 7 {
				return fmt.Errorf("windows[%d].days_of_week=%d must be 1..7 (Mon..Sun)", i, d)
			}
		}
		if w.Timezone == "" {
			return fmt.Errorf("windows[%d].timezone required", i)
		}
		if _, err := time.LoadLocation(w.Timezone); err != nil {
			return fmt.Errorf("windows[%d].timezone %q: %w", i, w.Timezone, err)
		}
	}
	return nil
}

func validateTimeHHMM(s string) error {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return fmt.Errorf("time %q not HH:MM", s)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return fmt.Errorf("hour out of range in %q", s)
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return fmt.Errorf("minute out of range in %q", s)
	}
	return nil
}
