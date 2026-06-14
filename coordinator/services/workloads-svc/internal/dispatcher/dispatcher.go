// Package dispatcher owns the connected-daemon registry and the
// in-flight assignment lifecycle. The bidi Dispatch stream handler hands
// off DaemonHello frames to Register(); the workload-submission handlers
// hand off newly-created workloads to TryAssign().
//
// The dispatcher is the *only* component that mutates the connected-daemon
// state — the scheduler stays pure. Retry/failover is implemented here
// because it needs the live connection set.
package dispatcher

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/scheduler"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/store"
)

// Default per-attempt timeout from the task brief — if a daemon doesn't ack
// inside this window the dispatcher re-tries against the next candidate.
const DefaultAttemptTimeout = 60 * time.Second

// Connection is the live bidi-stream handle for one connected daemon.
type Connection struct {
	ProviderID string
	Send       func(assignment *Assignment) error
	Snapshot   scheduler.ProviderSnapshot
	// EndpointHint is the publicly-reachable address (host:port) the
	// proxy-gateway should dial to forward customer bytes to this
	// provider. For NAT'd daemons (founder Mac) this is set by
	// workloads-svc to its own TCP-over-DispatchFrame forwarder
	// listener; for daemons running on routable hosts it's the
	// daemon's WireGuard / public listener.
	//
	// Wired by the TCP forwarder once a listener is up (issue #222).
	// May be empty during pure-store unit tests.
	EndpointHint string
	// SessionTokenSeed is an opaque short-lived string passed to the
	// proxy-gateway alongside the chosen provider; the daemon checks
	// it on accept. Same caveat as EndpointHint — may be empty until
	// the dispatch JWT minting flow lands.
	SessionTokenSeed string
	// SendTunnelOpen pushes a TunnelOpen frame down the bidi stream,
	// instructing the daemon to dial `targetHostPort` and start
	// pumping bytes tagged with `attemptID`. The forwarder calls this
	// when proxy-gateway hands it a new TCP connection.
	SendTunnelOpen func(attemptID, targetHostPort string) error
	// SendTunnelData pushes a TunnelData frame (raw bytes) down the
	// bidi stream for an already-opened tunnel.
	SendTunnelData func(attemptID string, payload []byte) error
	// SendTunnelClose pushes a TunnelClose frame down the stream;
	// `reason` is empty for a clean EOF.
	SendTunnelClose func(attemptID, reason string) error
	connectedAt     time.Time
	disconnected    chan struct{}
	// seq identifies this exact registration so a stale concurrent stream's
	// Unregister doesn't evict a newer live stream for the same provider
	// (#806). Assigned by Register.
	seq uint64
}

// Assignment is what we push down the stream. The dispatcher.D registers
// the attempt with the store before calling Send so that out-of-band
// `GetAssignment` lookups see the same id.
type Assignment struct {
	ID         string
	WorkloadID string
	ProviderID string
	// Endpoint mirrors Connection.EndpointHint into the assignment so
	// callers of TryAssign (submission handler) can populate the
	// dispatched_provider_endpoint label without holding the dispatcher
	// lock again.
	Endpoint string
	// SessionToken mirrors Connection.SessionTokenSeed into the
	// assignment for the same reason.
	SessionToken string
	Deadline     time.Time
}

// TunnelSink is what the forwarder gives the dispatcher so daemon-side
// TunnelData / TunnelClose frames can be delivered back to the right TCP
// socket. Per-attempt; registered when the forwarder accepts an inbound
// proxy-gateway connection and torn down when that connection ends.
type TunnelSink interface {
	OnTunnelData(payload []byte)
	OnTunnelClose(reason string)
}

// D is the dispatcher. Safe for concurrent use.
type D struct {
	Store     store.Store
	Scheduler *scheduler.Scheduler
	Log       *slog.Logger

	mu          sync.RWMutex
	connections map[string]*Connection
	regSeq      uint64 // monotonically increasing registration counter (#806)

	tunMu   sync.RWMutex
	tunnels map[string]TunnelSink

	attemptTimeout time.Duration
}

// New builds a dispatcher with sensible defaults.
func New(s store.Store, log *slog.Logger) *D {
	if log == nil {
		log = slog.Default()
	}
	return &D{
		Store:          s,
		Scheduler:      scheduler.New(),
		Log:            log,
		connections:    make(map[string]*Connection),
		tunnels:        make(map[string]TunnelSink),
		attemptTimeout: DefaultAttemptTimeout,
	}
}

// RegisterTunnel binds an attempt id to a TunnelSink. The forwarder calls
// this on accept, and the dispatch handler routes inbound TunnelData /
// TunnelClose frames through the sink. Overwrites any existing sink for
// the same attempt id (last-writer-wins; only one in-flight forwarder
// connection per attempt).
func (d *D) RegisterTunnel(attemptID string, sink TunnelSink) {
	d.tunMu.Lock()
	defer d.tunMu.Unlock()
	d.tunnels[attemptID] = sink
}

// UnregisterTunnel removes the sink for an attempt id; safe to call
// multiple times.
func (d *D) UnregisterTunnel(attemptID string) {
	d.tunMu.Lock()
	defer d.tunMu.Unlock()
	delete(d.tunnels, attemptID)
}

// DeliverTunnelData fans daemon-side bytes back into the forwarder. The
// dispatch handler calls this on every inbound TunnelData frame. Returns
// false if no sink is registered (unknown / closed attempt).
func (d *D) DeliverTunnelData(attemptID string, payload []byte) bool {
	d.tunMu.RLock()
	sink, ok := d.tunnels[attemptID]
	d.tunMu.RUnlock()
	if !ok {
		return false
	}
	sink.OnTunnelData(payload)
	return true
}

// DeliverTunnelClose fans a daemon-side TunnelClose into the forwarder.
// Returns false if no sink is registered.
func (d *D) DeliverTunnelClose(attemptID, reason string) bool {
	d.tunMu.RLock()
	sink, ok := d.tunnels[attemptID]
	d.tunMu.RUnlock()
	if !ok {
		return false
	}
	sink.OnTunnelClose(reason)
	return true
}

// ConnectionByProviderID returns the live Connection for the given
// provider id, or nil if not connected. Callers must NOT mutate the
// returned struct's internal fields; only the Send* hooks are safe to
// invoke concurrently.
func (d *D) ConnectionByProviderID(providerID string) *Connection {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.connections[providerID]
}

// LookupAssignmentProvider returns the provider id that owns a given
// attempt id, or "" if the attempt is unknown. Used by the forwarder to
// resolve attempt id → daemon stream.
func (d *D) LookupAssignmentProvider(ctx context.Context, attemptID string) string {
	a, err := d.Store.GetAssignment(ctx, attemptID)
	if err != nil || a == nil {
		return ""
	}
	return a.ProviderID
}

// Register adds a daemon to the live registry. Returns the channel the
// caller MUST close (via Unregister) when the stream ends.
//
// A single provider may briefly hold MORE THAN ONE live dispatch stream —
// e.g. a Mac that advertises bandwidth on one stream and bandwidth+IOS_BUILD
// on another (observed on the founder's Mac, #806). The map is keyed by
// provider id, so the second Register would otherwise clobber the first and
// the surviving snapshot could be the capability-poorer one — making an
// IOS_BUILD-capable provider invisible to the scheduler ("no eligible
// provider"). To be robust to stream ordering we UNION the capability flags
// of any existing connection into the incoming one, so a capability
// advertised on ANY concurrent stream sticks. Each connection is stamped with
// a monotonically increasing seq so Unregister can tell whether it owns the
// slot (a stale stream's deferred Unregister must not evict the live one).
func (d *D) Register(c *Connection) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c.connectedAt = time.Now().UTC()
	c.disconnected = make(chan struct{})
	d.regSeq++
	c.seq = d.regSeq
	if prev, ok := d.connections[c.ProviderID]; ok {
		c.Snapshot = mergeCapabilities(prev.Snapshot, c.Snapshot)
	}
	d.connections[c.ProviderID] = c
}

// mergeCapabilities returns next with any positive capability from prev folded
// in. Capabilities only ratchet UP across concurrent streams of one provider —
// IOS_BUILD/GPU/macOS-platform/host-version advertised on either stream are
// kept. Mutable runtime state (load, status) always comes from next (the
// fresher registration).
func mergeCapabilities(prev, next scheduler.ProviderSnapshot) scheduler.ProviderSnapshot {
	if prev.IOSBuildEnabled {
		next.IOSBuildEnabled = true
		if next.Platform == "" {
			next.Platform = prev.Platform
		}
	}
	if prev.GPUEnabled {
		next.GPUEnabled = true
		if next.GPUVRAMMiB == 0 {
			next.GPUVRAMMiB = prev.GPUVRAMMiB
		}
	}
	if next.HostMacosVersion == 0 && prev.HostMacosVersion > 0 {
		next.HostMacosVersion = prev.HostMacosVersion
	}
	// Union the supported-type slugs (order-independent, de-duplicated).
	seen := make(map[string]struct{}, len(next.SupportedTypes)+len(prev.SupportedTypes))
	merged := make([]string, 0, len(seen))
	for _, t := range next.SupportedTypes {
		if _, ok := seen[t]; !ok {
			seen[t] = struct{}{}
			merged = append(merged, t)
		}
	}
	for _, t := range prev.SupportedTypes {
		if _, ok := seen[t]; !ok {
			seen[t] = struct{}{}
			merged = append(merged, t)
		}
	}
	next.SupportedTypes = merged
	return next
}

// Unregister removes a daemon, signalling any in-flight TryAssign that
// the connection is gone. It is a no-op when the currently-registered
// connection is a DIFFERENT (newer) stream than the one identified by seq:
// when a provider holds two concurrent streams, the first stream's deferred
// Unregister must not evict the second, still-live connection (#806).
func (d *D) Unregister(providerID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if c, ok := d.connections[providerID]; ok {
		close(c.disconnected)
		delete(d.connections, providerID)
	}
}

// UnregisterConn removes a SPECIFIC connection, leaving a newer concurrent
// stream for the same provider in place. Falls back to a provider-id delete
// only when the slot still holds this exact connection.
func (d *D) UnregisterConn(c *Connection) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if c != nil {
		close(c.disconnected)
	}
	if cur, ok := d.connections[c.ProviderID]; ok && cur == c {
		delete(d.connections, c.ProviderID)
	}
}

// UpdateSnapshot refreshes the cached capability snapshot for a connected
// daemon. Called from the dispatch loop when a fresh DaemonHello / status
// frame arrives.
func (d *D) UpdateSnapshot(providerID string, snap scheduler.ProviderSnapshot) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if c, ok := d.connections[providerID]; ok {
		c.Snapshot = snap
	}
}

// SnapshotAll returns a copy of every connected daemon's snapshot — the
// scheduler input.
func (d *D) SnapshotAll() []scheduler.ProviderSnapshot {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]scheduler.ProviderSnapshot, 0, len(d.connections))
	for _, c := range d.connections {
		out = append(out, c.Snapshot)
	}
	return out
}

// TryAssign drives one workload through the candidate list. For each
// candidate it creates an Assignment row, sends the frame, and waits up
// to attemptTimeout for the daemon to update the workload to RUNNING. On
// timeout (or send-failure) it moves to the next candidate. Returns the
// successful Assignment, or an error if no candidate accepted.
func (d *D) TryAssign(ctx context.Context, w *store.Workload) (*Assignment, error) {
	req := workloadToRequest(w)
	candidates := d.Scheduler.PickCandidates(d.SnapshotAll(), req, 5)
	if len(candidates) == 0 {
		_ = d.Store.UpdateWorkloadStatus(ctx, w.ID, store.StatusRejected, "no eligible provider")
		return nil, errors.New("dispatcher: no eligible provider")
	}

	for _, cand := range candidates {
		d.mu.RLock()
		conn, ok := d.connections[cand.ProviderID]
		d.mu.RUnlock()
		if !ok {
			continue
		}

		attempt := &Assignment{
			ID:           uuid.NewString(),
			WorkloadID:   w.ID,
			ProviderID:   cand.ProviderID,
			Endpoint:     conn.EndpointHint,
			SessionToken: conn.SessionTokenSeed,
			Deadline:     time.Now().Add(d.attemptTimeout),
		}
		_ = d.Store.CreateAssignment(ctx, &store.Assignment{
			ID:           attempt.ID,
			WorkloadID:   w.ID,
			ProviderID:   cand.ProviderID,
			CreatedAt:    time.Now().UTC(),
			Deadline:     attempt.Deadline,
			LatestStatus: store.StatusDispatched,
		})
		if err := conn.Send(attempt); err != nil {
			d.Log.Warn("dispatch send failed",
				slog.String("provider_id", cand.ProviderID),
				slog.String("workload_id", w.ID),
				slog.String("error", err.Error()))
			_ = d.Store.UpdateAssignment(ctx, &store.Assignment{
				ID:              attempt.ID,
				WorkloadID:      w.ID,
				ProviderID:      cand.ProviderID,
				CreatedAt:       time.Now().UTC(),
				Deadline:        attempt.Deadline,
				LatestStatus:    store.StatusRejected,
				RejectionReason: "send failed: " + err.Error(),
			})
			continue
		}
		_ = d.Store.UpdateWorkloadStatus(ctx, w.ID, store.StatusDispatched, "dispatched to "+cand.ProviderID)
		return attempt, nil
	}
	_ = d.Store.UpdateWorkloadStatus(ctx, w.ID, store.StatusRejected, "all candidates failed to ack")
	return nil, errors.New("dispatcher: every candidate failed")
}

// workloadToRequest projects a stored Workload into the scheduler's
// WorkloadRequest, populating type-specific minimums.
func workloadToRequest(w *store.Workload) scheduler.WorkloadRequest {
	req := scheduler.WorkloadRequest{Type: w.Type}
	switch {
	case w.Bandwidth != nil:
		req.PreferredRegion = w.Bandwidth.PreferredRegion
		req.Category = w.Bandwidth.Category
		req.DestinationHost = extractHost(w.Bandwidth.TargetURL)
	case w.Docker != nil:
		req.MinCPUCores = w.Docker.MinCPUCores
		req.MinMemoryMiB = w.Docker.MinMemoryMiB
		req.MinGPUMemoryMiB = w.Docker.MinGPUMemoryMiB
	case w.GPU != nil:
		req.MinGPUMemoryMiB = w.GPU.MinVRAMMiB
	case w.IOSBuild != nil:
		req.RequiredPlatform = "macos"
		// #737: derive the minimum host macOS version from the job's Tart
		// image (the image's guest-macOS family is the host floor under
		// Apple Virtualization.framework). 0 for an unrecognised/locally-
		// baked image → no extra constraint beyond Platform=macos.
		req.RequiredMacosVersion = scheduler.RequiredMacosForTartImage(w.IOSBuild.TartImage)
	}
	return req
}

// extractHost is a tiny URL → host shim that doesn't drag in net/url for
// such a hot path. Accepts both "https://host/path" and plain "host:port".
func extractHost(rawURL string) string {
	s := rawURL
	if i := indexOfAfterScheme(s); i >= 0 {
		s = s[i:]
	}
	if i := indexOf(s, '/'); i >= 0 {
		s = s[:i]
	}
	if i := indexOf(s, ':'); i >= 0 {
		s = s[:i]
	}
	return s
}

func indexOfAfterScheme(s string) int {
	for i := 0; i+2 < len(s); i++ {
		if s[i] == ':' && s[i+1] == '/' && s[i+2] == '/' {
			return i + 3
		}
	}
	return -1
}

func indexOf(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
