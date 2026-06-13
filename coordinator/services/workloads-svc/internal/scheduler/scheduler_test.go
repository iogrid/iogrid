package scheduler

import (
	"testing"
)

func bandwidthProvider(id string) ProviderSnapshot {
	return ProviderSnapshot{
		ID:               id,
		Status:           "active",
		State:            "SCHEDULER_STATE_ACTIVE",
		SupportedTypes:   []string{"bandwidth"},
		RegionSlug:       "us-east-1",
		CountryCode:      "US",
		Platform:         "linux",
		CPULogicalCores:  8,
		MemoryMiB:        16 * 1024,
		CurrentLoadPct:   20,
		AllowedCategories: []string{"e_commerce", "seo"},
	}
}

func gpuProvider(id string) ProviderSnapshot {
	p := bandwidthProvider(id)
	p.SupportedTypes = []string{"gpu", "docker"}
	p.GPUEnabled = true
	p.GPUVRAMMiB = 24 * 1024
	return p
}

func iosProvider(id string) ProviderSnapshot {
	p := bandwidthProvider(id)
	p.SupportedTypes = []string{"ios_build"}
	p.IOSBuildEnabled = true
	p.Platform = "macos"
	return p
}

func TestMatchCapability_TypeMissing(t *testing.T) {
	s := New()
	got := s.MatchCapability(bandwidthProvider("p"), WorkloadRequest{Type: "docker"})
	if got != 0 {
		t.Fatalf("expected 0 for missing type, got %v", got)
	}
}

func TestMatchCapability_GPUWithoutGPU(t *testing.T) {
	s := New()
	p := bandwidthProvider("p")
	got := s.MatchCapability(p, WorkloadRequest{Type: "gpu"})
	if got != 0 {
		t.Fatalf("expected 0 when type missing")
	}
}

func TestMatchCapability_GPUVRAMTooSmall(t *testing.T) {
	s := New()
	p := gpuProvider("p")
	p.GPUVRAMMiB = 4 * 1024
	got := s.MatchCapability(p, WorkloadRequest{Type: "gpu", MinGPUMemoryMiB: 8 * 1024})
	if got != 0 {
		t.Fatalf("expected rejection on vram")
	}
}

func TestMatchCapability_IOSBuildRequiresMac(t *testing.T) {
	s := New()
	p := bandwidthProvider("p")
	p.SupportedTypes = []string{"ios_build"}
	p.IOSBuildEnabled = true // but Platform stays linux
	if s.MatchCapability(p, WorkloadRequest{Type: "ios_build"}) != 0 {
		t.Fatalf("expected linux to be rejected for ios_build")
	}
	if s.MatchCapability(iosProvider("p"), WorkloadRequest{Type: "ios_build"}) != 1 {
		t.Fatalf("expected macos to be accepted")
	}
}

// #737: an iOS-build job that needs a recent guest macOS (e.g. a
// Sequoia-Xcode image) must NOT be dispatched to a Sonoma (14) host, but a
// Sequoia (15) host must accept it.
func TestMatchCapability_IOSBuildRoutesByHostMacosVersion(t *testing.T) {
	s := New()
	// Job derived (by the dispatcher) to need host macOS >= 15.
	job := WorkloadRequest{Type: "ios_build", RequiredMacosVersion: 15}

	sonoma := iosProvider("sonoma")
	sonoma.HostMacosVersion = 14
	if got := s.MatchCapability(sonoma, job); got != 0 {
		t.Fatalf("Sonoma(14) host must be rejected for a macOS>=15 job, got %v", got)
	}

	sequoia := iosProvider("sequoia")
	sequoia.HostMacosVersion = 15
	if got := s.MatchCapability(sequoia, job); got != 1 {
		t.Fatalf("Sequoia(15) host must accept a macOS>=15 job, got %v", got)
	}

	// Exact-equal host satisfies the floor (>=, not >).
	tahoe := iosProvider("tahoe")
	tahoe.HostMacosVersion = 16
	if got := s.MatchCapability(tahoe, job); got != 1 {
		t.Fatalf("Tahoe(16) host must accept a macOS>=15 job, got %v", got)
	}
}

// #737 back-compat: a daemon that doesn't advertise its host macOS version
// (HostMacosVersion==0, e.g. a build predating the field) must keep today's
// behaviour — matched on Platform=macos alone, NOT silently de-scheduled.
// Likewise a job with no derived requirement (RequiredMacosVersion==0, e.g.
// a locally-baked image) accepts any macOS host.
func TestMatchCapability_IOSBuildVersionGateFailsOpen(t *testing.T) {
	s := New()

	// Unknown host version + a versioned job → fail open (accept).
	unknownHost := iosProvider("unknown")
	unknownHost.HostMacosVersion = 0
	if got := s.MatchCapability(unknownHost, WorkloadRequest{Type: "ios_build", RequiredMacosVersion: 15}); got != 1 {
		t.Fatalf("unknown host version must fail open and be accepted, got %v", got)
	}

	// Known (old) host + a job with no requirement → accept.
	sonoma := iosProvider("sonoma")
	sonoma.HostMacosVersion = 14
	if got := s.MatchCapability(sonoma, WorkloadRequest{Type: "ios_build", RequiredMacosVersion: 0}); got != 1 {
		t.Fatalf("no-requirement job must accept any macOS host, got %v", got)
	}
}

func TestRequiredMacosForTartImage(t *testing.T) {
	cases := map[string]uint32{
		"ghcr.io/cirruslabs/macos-sequoia-xcode:16.2": 15,
		"ghcr.io/cirruslabs/macos-sequoia-xcode:latest": 15,
		"ghcr.io/cirruslabs/macos-sonoma-xcode:15.4": 14,
		"ghcr.io/cirruslabs/macos-tahoe-xcode:26.0": 16,
		"GHCR.IO/CIRRUSLABS/MACOS-SEQUOIA-XCODE:16.0": 15, // case-insensitive
		"":                          0, // empty → no constraint
		"iogrid-ios-builder-16.2":   0, // locally-baked native image → no guest-VM constraint
		"some/unknown-image:latest": 0, // unrecognised → fall through to Platform=macos
	}
	for img, want := range cases {
		if got := RequiredMacosForTartImage(img); got != want {
			t.Errorf("RequiredMacosForTartImage(%q) = %d, want %d", img, got, want)
		}
	}
}

func TestMatchCapability_DockerCPUMemory(t *testing.T) {
	s := New()
	p := bandwidthProvider("p")
	p.SupportedTypes = []string{"docker"}
	w := WorkloadRequest{Type: "docker", MinCPUCores: 16, MinMemoryMiB: 4 * 1024}
	if s.MatchCapability(p, w) != 0 {
		t.Fatalf("expected rejection on cpu cores")
	}
	p.CPULogicalCores = 32
	if s.MatchCapability(p, w) != 1 {
		t.Fatalf("expected acceptance, got %v", s.MatchCapability(p, w))
	}
}

func TestMatchGeo_ExactAndCountryAndMiss(t *testing.T) {
	s := New()
	p := bandwidthProvider("p")
	if s.MatchGeo(p, WorkloadRequest{PreferredRegion: ""}) != 1 {
		t.Fatalf("no preference should be 1")
	}
	if s.MatchGeo(p, WorkloadRequest{PreferredRegion: "us-east-1"}) != 1 {
		t.Fatalf("exact match should be 1")
	}
	// Country match (us-west-1 vs CountryCode US — region prefix "us")
	if got := s.MatchGeo(p, WorkloadRequest{PreferredRegion: "us-west-1"}); got != 0.5 {
		t.Fatalf("country match should be 0.5 got %v", got)
	}
	if got := s.MatchGeo(p, WorkloadRequest{PreferredRegion: "eu-west-1"}); got != 0 {
		t.Fatalf("foreign country should be 0 got %v", got)
	}
}

func TestMatchOptIn_Allowed(t *testing.T) {
	s := New()
	p := bandwidthProvider("p")
	if s.MatchOptIn(p, WorkloadRequest{Category: "e_commerce"}) != 1 {
		t.Fatalf("allowed category should pass")
	}
	if s.MatchOptIn(p, WorkloadRequest{Category: "lead_gen"}) != 0 {
		t.Fatalf("non-allowed category should be 0")
	}
}

func TestMatchOptIn_DisallowedExplicit(t *testing.T) {
	s := New()
	p := bandwidthProvider("p")
	p.DisallowedCategories = []string{"e_commerce"}
	if s.MatchOptIn(p, WorkloadRequest{Category: "e_commerce"}) != 0 {
		t.Fatalf("explicit disallow should win")
	}
}

func TestMatchOptIn_DestinationBlocklist(t *testing.T) {
	s := New()
	p := bandwidthProvider("p")
	p.DestinationBlocklist = []string{"*.linkedin.com", "evil.example"}
	if s.MatchOptIn(p, WorkloadRequest{DestinationHost: "www.linkedin.com"}) != 0 {
		t.Fatalf("wildcard suffix should block")
	}
	if s.MatchOptIn(p, WorkloadRequest{DestinationHost: "evil.example"}) != 0 {
		t.Fatalf("exact match should block")
	}
	if s.MatchOptIn(p, WorkloadRequest{DestinationHost: "good.example"}) != 1 {
		t.Fatalf("unrelated host should be 1")
	}
}

func TestMatchLoad_ScalesInverse(t *testing.T) {
	s := New()
	p := bandwidthProvider("p")
	p.CurrentLoadPct = 0
	if s.MatchLoad(p, WorkloadRequest{}) != 1 {
		t.Fatalf("0%% load should score 1")
	}
	p.CurrentLoadPct = 100
	if s.MatchLoad(p, WorkloadRequest{}) != 0 {
		t.Fatalf("100%% load should score 0")
	}
	p.CurrentLoadPct = 50
	if got := s.MatchLoad(p, WorkloadRequest{}); got <= 0.49 || got >= 0.51 {
		t.Fatalf("50%% should be ~0.5 got %v", got)
	}
}

func TestPickCandidates_FiltersIneligible(t *testing.T) {
	s := New()
	w := WorkloadRequest{Type: "bandwidth", PreferredRegion: "us-east-1", Category: "e_commerce"}
	providers := []ProviderSnapshot{
		bandwidthProvider("a"),
		bandwidthProvider("b"),
		// Deactivated — should be skipped.
		func() ProviderSnapshot {
			p := bandwidthProvider("c")
			p.Status = "deactivated"
			return p
		}(),
		// Paused — should be skipped.
		func() ProviderSnapshot {
			p := bandwidthProvider("d")
			p.State = "SCHEDULER_STATE_PAUSED_BANDWIDTH_CAP"
			return p
		}(),
		// Wrong type — capability gate.
		gpuProvider("e"),
	}
	cands := s.PickCandidates(providers, w, 10)
	if len(cands) != 2 {
		t.Fatalf("expected 2 candidates, got %d (%+v)", len(cands), cands)
	}
}

func TestPickCandidates_LowestLoadWinsTies(t *testing.T) {
	s := New()
	p1 := bandwidthProvider("loaded")
	p1.CurrentLoadPct = 80
	p2 := bandwidthProvider("idle")
	p2.CurrentLoadPct = 5
	w := WorkloadRequest{Type: "bandwidth", PreferredRegion: "us-east-1", Category: "e_commerce"}
	cands := s.PickCandidates([]ProviderSnapshot{p1, p2}, w, 10)
	if len(cands) != 2 {
		t.Fatalf("len = %d", len(cands))
	}
	if cands[0].ProviderID != "idle" {
		t.Fatalf("expected idle first, got %s", cands[0].ProviderID)
	}
}

func TestPickCandidates_TopNCap(t *testing.T) {
	s := New()
	w := WorkloadRequest{Type: "bandwidth", Category: "e_commerce"}
	provs := []ProviderSnapshot{
		bandwidthProvider("a"), bandwidthProvider("b"), bandwidthProvider("c"), bandwidthProvider("d"),
	}
	cands := s.PickCandidates(provs, w, 2)
	if len(cands) != 2 {
		t.Fatalf("expected topN=2, got %d", len(cands))
	}
}
