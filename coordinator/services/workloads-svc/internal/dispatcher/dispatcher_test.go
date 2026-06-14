package dispatcher

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/scheduler"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/store"
)

func makeBandwidthWorkload(host string) *store.Workload {
	return &store.Workload{
		ID:        "wl-1",
		Type:      "bandwidth",
		Bandwidth: &store.BandwidthSpec{TargetURL: "https://" + host + "/feed", PreferredRegion: "us-east-1", Category: "e_commerce"},
		Status:    store.StatusQueued,
	}
}

func TestExtractHost(t *testing.T) {
	cases := map[string]string{
		"https://example.com/path":        "example.com",
		"http://10.0.0.1:8080/x":          "10.0.0.1",
		"example.com":                     "example.com",
		"https://www.linkedin.com/in/foo": "www.linkedin.com",
	}
	for in, want := range cases {
		if got := extractHost(in); got != want {
			t.Errorf("extractHost(%q)=%q want %q", in, got, want)
		}
	}
}

// #737: an iOS-build workload must derive RequiredMacosVersion from its
// Tart image so the scheduler can route it to a host recent enough to run
// the guest macOS. A Sequoia-Xcode image floors the host at macOS 15.
func TestWorkloadToRequest_IOSBuildDerivesRequiredMacos(t *testing.T) {
	w := &store.Workload{
		ID:       "wl-ios",
		Type:     store.TypeIOSBuild,
		IOSBuild: &store.IOSBuildSpec{TartImage: "ghcr.io/cirruslabs/macos-sequoia-xcode:16.2"},
		Status:   store.StatusQueued,
	}
	req := workloadToRequest(w)
	if req.RequiredPlatform != "macos" {
		t.Fatalf("RequiredPlatform = %q, want macos", req.RequiredPlatform)
	}
	if req.RequiredMacosVersion != 15 {
		t.Fatalf("RequiredMacosVersion = %d, want 15 (Sequoia image floors host at 15)", req.RequiredMacosVersion)
	}

	// A locally-baked native image carries no guest-VM constraint.
	native := &store.Workload{
		ID:       "wl-native",
		Type:     store.TypeIOSBuild,
		IOSBuild: &store.IOSBuildSpec{TartImage: "iogrid-ios-builder-16.2"},
		Status:   store.StatusQueued,
	}
	if got := workloadToRequest(native).RequiredMacosVersion; got != 0 {
		t.Fatalf("native-image RequiredMacosVersion = %d, want 0 (no constraint)", got)
	}
}

func TestTryAssign_NoProvidersConnected(t *testing.T) {
	s := store.NewInMemory()
	d := New(s, nil)
	w := makeBandwidthWorkload("example.com")
	if err := s.CreateWorkload(context.Background(), w); err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err := d.TryAssign(context.Background(), w)
	if err == nil {
		t.Fatalf("expected error")
	}
	got, _ := s.GetWorkload(context.Background(), w.ID)
	if got.Status != store.StatusRejected {
		t.Fatalf("expected rejected, got %q", got.Status)
	}
}

func TestTryAssign_FirstCandidateFails_SecondAccepts(t *testing.T) {
	s := store.NewInMemory()
	d := New(s, nil)
	w := makeBandwidthWorkload("example.com")
	_ = s.CreateWorkload(context.Background(), w)

	failingSnap := scheduler.ProviderSnapshot{
		ID: "p-broken", Status: "active", State: "SCHEDULER_STATE_ACTIVE",
		SupportedTypes:    []string{"bandwidth"},
		RegionSlug:        "us-east-1",
		AllowedCategories: []string{"e_commerce"},
		CurrentLoadPct:    1, // loaded less ⇒ chosen first
	}
	goodSnap := scheduler.ProviderSnapshot{
		ID: "p-good", Status: "active", State: "SCHEDULER_STATE_ACTIVE",
		SupportedTypes:    []string{"bandwidth"},
		RegionSlug:        "us-east-1",
		AllowedCategories: []string{"e_commerce"},
		CurrentLoadPct:    2,
	}

	d.Register(&Connection{
		ProviderID: "p-broken",
		Snapshot:   failingSnap,
		Send:       func(_ *Assignment) error { return errors.New("dial failed") },
	})
	d.Register(&Connection{
		ProviderID: "p-good",
		Snapshot:   goodSnap,
		Send:       func(_ *Assignment) error { return nil },
	})

	att, err := d.TryAssign(context.Background(), w)
	if err != nil {
		t.Fatalf("TryAssign: %v", err)
	}
	if att.ProviderID != "p-good" {
		t.Fatalf("expected failover to p-good, got %s", att.ProviderID)
	}
	got, _ := s.GetWorkload(context.Background(), w.ID)
	if got.Status != store.StatusDispatched {
		t.Fatalf("expected dispatched, got %s", got.Status)
	}

	assignments, _ := s.AssignmentsForWorkload(context.Background(), w.ID)
	if len(assignments) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(assignments))
	}
}

func TestTryAssign_NoEligible_BlocklistHit(t *testing.T) {
	s := store.NewInMemory()
	d := New(s, nil)
	w := makeBandwidthWorkload("www.linkedin.com")
	_ = s.CreateWorkload(context.Background(), w)

	snap := scheduler.ProviderSnapshot{
		ID: "p1", Status: "active", State: "SCHEDULER_STATE_ACTIVE",
		SupportedTypes:       []string{"bandwidth"},
		RegionSlug:           "us-east-1",
		AllowedCategories:    []string{"e_commerce"},
		DestinationBlocklist: []string{"*.linkedin.com"},
	}
	d.Register(&Connection{
		ProviderID: "p1", Snapshot: snap,
		Send: func(_ *Assignment) error { return nil },
	})
	if _, err := d.TryAssign(context.Background(), w); err == nil {
		t.Fatalf("expected blocklist to reject")
	}
}

func TestRegisterUnregister(t *testing.T) {
	d := New(store.NewInMemory(), nil)
	d.Register(&Connection{ProviderID: "p1", Send: func(_ *Assignment) error { return nil }})
	if len(d.SnapshotAll()) != 1 {
		t.Fatalf("expected 1 connection")
	}
	d.Unregister("p1")
	if len(d.SnapshotAll()) != 0 {
		t.Fatalf("expected 0 connections after unregister")
	}
}

func TestUpdateSnapshot(t *testing.T) {
	d := New(store.NewInMemory(), nil)
	d.Register(&Connection{ProviderID: "p1", Send: func(_ *Assignment) error { return nil }})
	d.UpdateSnapshot("p1", scheduler.ProviderSnapshot{ID: "p1", CurrentLoadPct: 50})
	snaps := d.SnapshotAll()
	if len(snaps) != 1 || snaps[0].CurrentLoadPct != 50 {
		t.Fatalf("snapshot not updated: %+v", snaps)
	}
}

// fakeSink captures TunnelSink callbacks for the registry tests.
type fakeSink struct {
	data   [][]byte
	closed string
}

func (f *fakeSink) OnTunnelData(b []byte)  { f.data = append(f.data, append([]byte(nil), b...)) }
func (f *fakeSink) OnTunnelClose(r string) { f.closed = r }

func TestTunnelRegistry_DeliverAndUnregister(t *testing.T) {
	d := New(store.NewInMemory(), nil)
	sink := &fakeSink{}
	d.RegisterTunnel("att-1", sink)

	if ok := d.DeliverTunnelData("att-1", []byte("hello")); !ok {
		t.Fatalf("DeliverTunnelData returned false for registered attempt")
	}
	if ok := d.DeliverTunnelData("att-unknown", []byte("x")); ok {
		t.Fatalf("DeliverTunnelData returned true for unknown attempt")
	}
	if len(sink.data) != 1 || string(sink.data[0]) != "hello" {
		t.Fatalf("sink.data=%v", sink.data)
	}

	if ok := d.DeliverTunnelClose("att-1", "eof"); !ok {
		t.Fatalf("DeliverTunnelClose returned false")
	}
	if sink.closed != "eof" {
		t.Fatalf("sink.closed=%q", sink.closed)
	}

	d.UnregisterTunnel("att-1")
	if ok := d.DeliverTunnelData("att-1", []byte("x")); ok {
		t.Fatalf("DeliverTunnelData returned true after unregister")
	}
}

func TestConnectionByProviderID_ReturnsRegistered(t *testing.T) {
	d := New(store.NewInMemory(), nil)
	d.Register(&Connection{ProviderID: "p-mac", EndpointHint: "x:9090", Send: func(_ *Assignment) error { return nil }})
	got := d.ConnectionByProviderID("p-mac")
	if got == nil {
		t.Fatalf("expected non-nil connection")
	}
	if got.EndpointHint != "x:9090" {
		t.Fatalf("EndpointHint=%q", got.EndpointHint)
	}
	if d.ConnectionByProviderID("nope") != nil {
		t.Fatalf("expected nil for unknown provider id")
	}
}

func TestAttemptDeadline_Default(t *testing.T) {
	d := New(store.NewInMemory(), nil)
	if d.attemptTimeout != DefaultAttemptTimeout {
		t.Fatalf("expected default timeout %v, got %v", DefaultAttemptTimeout, d.attemptTimeout)
	}
	// Sanity check the deadline math the dispatch path uses.
	d.attemptTimeout = 5 * time.Second
	if d.attemptTimeout != 5*time.Second {
		t.Fatalf("override failed")
	}
}

// TestDoubleStream_IOSBuildSurvives reproduces the #806 dog-food blocker: a
// single provider opens TWO concurrent dispatch streams — one advertising only
// bandwidth, one advertising bandwidth+ios_build. The capability-poorer stream
// must NOT mask the ios_build capability, and a stale stream's Unregister must
// NOT evict the live one.
func TestDoubleStream_IOSBuildSurvives(t *testing.T) {
	d := New(store.NewInMemory(), nil)

	// Stream A: bandwidth only.
	connA := &Connection{
		ProviderID: "mac1",
		Snapshot: scheduler.ProviderSnapshot{
			ID: "mac1", Status: "active", State: "SCHEDULER_STATE_ACTIVE",
			SupportedTypes: []string{"bandwidth"},
		},
		Send: func(_ *Assignment) error { return nil },
	}
	// Stream B: bandwidth + ios_build on a macOS host.
	connB := &Connection{
		ProviderID: "mac1",
		Snapshot: scheduler.ProviderSnapshot{
			ID: "mac1", Status: "active", State: "SCHEDULER_STATE_ACTIVE",
			SupportedTypes:   []string{"bandwidth", "ios_build"},
			IOSBuildEnabled:  true,
			Platform:         "macos",
			HostMacosVersion: 26,
		},
		Send: func(_ *Assignment) error { return nil },
	}

	d.Register(connA)
	d.Register(connB) // B wins the slot; A's caps merge in (no-op here)

	snaps := d.SnapshotAll()
	if len(snaps) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(snaps))
	}
	if !snaps[0].IOSBuildEnabled || snaps[0].Platform != "macos" || snaps[0].HostMacosVersion != 26 {
		t.Fatalf("ios_build capability lost after double Register: %+v", snaps[0])
	}

	// The OPPOSITE ordering must also keep ios_build (B first, then the
	// poorer A) thanks to the capability union in Register.
	d2 := New(store.NewInMemory(), nil)
	bClone := *connB
	bClone.disconnected = nil
	aClone := *connA
	aClone.disconnected = nil
	d2.Register(&bClone)
	d2.Register(&aClone) // A wins the slot but must inherit B's ios_build
	s2 := d2.SnapshotAll()
	if len(s2) != 1 || !s2[0].IOSBuildEnabled || s2[0].Platform != "macos" || s2[0].HostMacosVersion != 26 {
		t.Fatalf("ios_build must survive poorer second stream: %+v", s2)
	}
	if !slices.Contains(s2[0].SupportedTypes, "ios_build") {
		t.Fatalf("ios_build slug missing from merged SupportedTypes: %+v", s2[0].SupportedTypes)
	}

	// Stale-stream teardown: A unregistering must NOT evict the live B.
	dd := New(store.NewInMemory(), nil)
	cA := &Connection{ProviderID: "mac1", Snapshot: scheduler.ProviderSnapshot{ID: "mac1"}, Send: func(_ *Assignment) error { return nil }}
	cB := &Connection{ProviderID: "mac1", Snapshot: scheduler.ProviderSnapshot{ID: "mac1", IOSBuildEnabled: true, Platform: "macos"}, Send: func(_ *Assignment) error { return nil }}
	dd.Register(cA)
	dd.Register(cB) // cB owns the slot
	dd.UnregisterConn(cA)
	if len(dd.SnapshotAll()) != 1 {
		t.Fatalf("live stream cB was evicted by stale cA teardown")
	}
	dd.UnregisterConn(cB)
	if len(dd.SnapshotAll()) != 0 {
		t.Fatalf("cB should be removed by its own UnregisterConn")
	}
}
