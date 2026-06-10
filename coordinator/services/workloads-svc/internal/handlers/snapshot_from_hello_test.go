package handlers

import (
	"testing"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	workloadsv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/workloads/v1"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/scheduler"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/store"
)

// A daemon advertising IOS_BUILD must produce a snapshot that the
// scheduler's ios_build MatchCapability accepts (Platform=macos +
// IOSBuildEnabled). Regression for "no eligible provider" on a real Mac.
func TestSnapshotFromHello_IOSBuildIsSchedulable(t *testing.T) {
	dh := &workloadsv1.DaemonHello{
		ProviderId: &commonv1.UUID{Value: "c0138910-9f41-4a05-972f-c6915760e0f0"},
		EligibleTypes: []commonv1.WorkloadType{
			commonv1.WorkloadType_WORKLOAD_TYPE_BANDWIDTH,
			commonv1.WorkloadType_WORKLOAD_TYPE_IOS_BUILD,
		},
		MaxConcurrent: 4,
	}
	snap := snapshotFromHello("c0138910-9f41-4a05-972f-c6915760e0f0", dh)
	if !snap.IOSBuildEnabled {
		t.Fatal("IOSBuildEnabled must be true when IOS_BUILD advertised")
	}
	if snap.Platform != "macos" {
		t.Fatalf("Platform = %q, want macos", snap.Platform)
	}

	s := scheduler.New()
	cands := s.PickCandidates([]scheduler.ProviderSnapshot{snap},
		scheduler.WorkloadRequest{Type: store.TypeIOSBuild, RequiredPlatform: "macos"}, 5)
	if len(cands) != 1 {
		t.Fatalf("PickCandidates returned %d candidates, want 1 (the Mac)", len(cands))
	}
}

// A bandwidth-only daemon must NOT be considered an iOS-build provider.
func TestSnapshotFromHello_BandwidthOnlyNotIOSBuild(t *testing.T) {
	dh := &workloadsv1.DaemonHello{
		ProviderId:    &commonv1.UUID{Value: "11111111-1111-1111-1111-111111111111"},
		EligibleTypes: []commonv1.WorkloadType{commonv1.WorkloadType_WORKLOAD_TYPE_BANDWIDTH},
	}
	snap := snapshotFromHello("11111111-1111-1111-1111-111111111111", dh)
	if snap.IOSBuildEnabled || snap.Platform == "macos" {
		t.Fatal("bandwidth-only daemon must not be ios-build/macos")
	}
}
