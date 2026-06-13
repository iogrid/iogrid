// Package scheduler implements the multi-dimensional fit function that
// matches a queued Workload to a set of eligible Providers.
//
// The function is intentionally pure: it takes a slice of provider
// snapshots and a workload description, and returns a ranked list of
// candidates. Side-effects (dispatch frame writes, retry timers, etc.)
// live in the dispatcher package. This separation lets the entire fit
// logic be unit-tested without spinning up any I/O.
//
// Scoring is weighted sum of four signals:
//
//	score = wCap*matchCapability + wGeo*matchGeo
//	      + wOpt*matchOptIn      + wLoad*matchLoad
//
// Each match* function returns a 0..1 float; capability/opt-in are hard
// gates (return 0 ⇒ provider is dropped before scoring), geo and load are
// soft preferences.
package scheduler

import (
	"sort"
	"strings"
	"time"
)

// Weights determine how much each axis contributes to the composite
// score. Defaults align with docs/ARCHITECTURE.md §Scheduling.
type Weights struct {
	Capability float64
	Geo        float64
	OptIn      float64
	Load       float64
}

// DefaultWeights gives capability the highest weight (it's also a hard
// gate); geo and load are tie-breakers.
func DefaultWeights() Weights {
	return Weights{Capability: 1.0, Geo: 0.4, OptIn: 1.0, Load: 0.6}
}

// ProviderSnapshot is the scheduler-visible view of one provider. Filled
// by the dispatcher from its in-memory connected-daemon registry.
type ProviderSnapshot struct {
	ID               string
	OwnerUserID      string
	Status           string
	SupportedTypes   []string
	GPUEnabled       bool
	IOSBuildEnabled  bool
	GPUVRAMMiB       uint64 // best single GPU when GPUEnabled
	CPULogicalCores  uint32
	MemoryMiB        uint64
	Platform         string // macos | linux | windows
	// HostMacosVersion is the provider's host macOS MAJOR version (14 =
	// Sonoma, 15 = Sequoia); 0 = unknown / not macOS. Advertised by the
	// daemon in DaemonHello (#737). Gates iOS-build dispatch: Apple
	// Virtualization.framework requires host macOS >= guest macOS, so a
	// Sonoma host cannot run a Sequoia-Xcode Tart image.
	HostMacosVersion uint32
	RegionSlug       string
	CountryCode      string
	AllowedCategories []string
	DisallowedCategories []string
	DestinationBlocklist []string
	State            string // matches providers/v1.SchedulerState.String()

	// Load signals — coordinator-side rollups updated on each heartbeat.
	CurrentLoadPct  uint32 // 0..100; lower = preferable
	BandwidthUsedPct uint32 // 0..100 of monthly cap
	LastSeenAt      time.Time
}

// WorkloadRequest is the scheduler-visible view of one queued workload.
type WorkloadRequest struct {
	Type             string // bandwidth|docker|gpu|ios_build
	PreferredRegion  string // slug
	DestinationHost  string // for bandwidth — checked against per-provider blocklist
	Category         string // e_commerce, seo, ...
	MinCPUCores      uint32
	MinMemoryMiB     uint64
	MinGPUMemoryMiB  uint64
	RequiredPlatform string // "" = any
	// RequiredMacosVersion is the minimum host macOS MAJOR version a
	// provider needs to run this job, derived from the iOS-build job's
	// Tart image (the image's guest-macOS family — e.g. a
	// macos-sequoia-xcode image needs host macOS >= 15). 0 = no
	// constraint (any macOS host, or a non-iOS-build job). #737.
	RequiredMacosVersion uint32
}

// Candidate is the scheduler output — one provider with its score and the
// reason it was kept (useful for `iogrid scheduler explain` debugging).
type Candidate struct {
	ProviderID string
	Score      float64
	Reasons    []string
}

// Scheduler bundles the configurable knobs (weights, current time source,
// etc.) so tests can inject fixed clocks.
type Scheduler struct {
	Weights Weights
	Now     func() time.Time
}

// New builds a scheduler with the default weights and the system clock.
func New() *Scheduler {
	return &Scheduler{Weights: DefaultWeights(), Now: time.Now}
}

// MatchCapability returns 1.0 when the provider claims the workload's
// type (and any extra hard gates like GPU memory) and 0.0 otherwise.
func (s *Scheduler) MatchCapability(p ProviderSnapshot, w WorkloadRequest) float64 {
	if !contains(p.SupportedTypes, w.Type) {
		return 0
	}
	if w.RequiredPlatform != "" && !strings.EqualFold(p.Platform, w.RequiredPlatform) {
		return 0
	}
	switch w.Type {
	case "gpu":
		if !p.GPUEnabled {
			return 0
		}
		if w.MinGPUMemoryMiB > 0 && p.GPUVRAMMiB < w.MinGPUMemoryMiB {
			return 0
		}
	case "ios_build":
		if !p.IOSBuildEnabled || !strings.EqualFold(p.Platform, "macos") {
			return 0
		}
		// #737: route by host macOS version. Apple Virtualization.framework
		// requires host macOS >= guest macOS, so a Sonoma (14) host cannot
		// run a Sequoia-Xcode (15) Tart image. Reject when the provider's
		// host is demonstrably too old for the job's required guest macOS.
		//
		// Fail-open on an UNKNOWN host version (HostMacosVersion == 0): a
		// daemon that predates the host_macos_version advertisement, or one
		// where sw_vers couldn't be read, keeps today's behaviour (matched
		// on Platform=macos alone) rather than being silently de-scheduled.
		// Once the daemon advertises a real version the gate engages.
		if w.RequiredMacosVersion > 0 &&
			p.HostMacosVersion > 0 &&
			p.HostMacosVersion < w.RequiredMacosVersion {
			return 0
		}
	case "docker":
		if w.MinCPUCores > 0 && p.CPULogicalCores < w.MinCPUCores {
			return 0
		}
		if w.MinMemoryMiB > 0 && p.MemoryMiB < w.MinMemoryMiB {
			return 0
		}
	}
	return 1
}

// MatchGeo returns 1.0 for an exact region match, 0.5 for same country,
// 0.0 when no geographic preference is expressed (still eligible).
func (s *Scheduler) MatchGeo(p ProviderSnapshot, w WorkloadRequest) float64 {
	if w.PreferredRegion == "" {
		return 1 // no preference ⇒ everyone fits geo
	}
	if p.RegionSlug == w.PreferredRegion {
		return 1
	}
	if len(w.PreferredRegion) >= 2 && len(p.CountryCode) == 2 && strings.HasPrefix(w.PreferredRegion, strings.ToLower(p.CountryCode)) {
		return 0.5
	}
	return 0
}

// MatchOptIn returns 1.0 when the workload's category is on the
// provider's allow-list (or no opt-in is configured), 0.0 when it's in
// the disallow-list or the destination is blocklisted.
func (s *Scheduler) MatchOptIn(p ProviderSnapshot, w WorkloadRequest) float64 {
	if w.Category != "" {
		if contains(p.DisallowedCategories, w.Category) {
			return 0
		}
		if len(p.AllowedCategories) > 0 && !contains(p.AllowedCategories, w.Category) {
			return 0
		}
	}
	if w.DestinationHost != "" && matchesAnyGlob(p.DestinationBlocklist, w.DestinationHost) {
		return 0
	}
	return 1
}

// MatchLoad rewards providers that are less utilised. Returns 1.0 at 0%
// load and falls to 0.0 at 100%.
func (s *Scheduler) MatchLoad(p ProviderSnapshot, _ WorkloadRequest) float64 {
	if p.CurrentLoadPct >= 100 {
		return 0
	}
	if p.CurrentLoadPct == 0 {
		return 1
	}
	return 1 - float64(p.CurrentLoadPct)/100.0
}

// Score returns the weighted composite for one provider. Pure helper —
// not gated by hard filters; call MatchCapability/MatchOptIn first.
func (s *Scheduler) Score(p ProviderSnapshot, w WorkloadRequest) float64 {
	w1 := s.Weights
	return w1.Capability*s.MatchCapability(p, w) +
		w1.Geo*s.MatchGeo(p, w) +
		w1.OptIn*s.MatchOptIn(p, w) +
		w1.Load*s.MatchLoad(p, w)
}

// PickCandidates filters out ineligible providers, scores the rest, and
// returns the top-N sorted candidates (descending by score, ties broken
// by lowest CurrentLoadPct). Eligible = capability+opt-in both 1.0 AND
// scheduler state ACTIVE AND status not DEACTIVATED/SUSPENDED.
func (s *Scheduler) PickCandidates(providers []ProviderSnapshot, w WorkloadRequest, topN int) []Candidate {
	if topN <= 0 {
		topN = 3
	}
	type scored struct {
		p     ProviderSnapshot
		score float64
		why   []string
	}
	pool := make([]scored, 0, len(providers))
	for _, p := range providers {
		if !isEligible(p) {
			continue
		}
		why := []string{}
		if s.MatchCapability(p, w) == 0 {
			continue
		}
		why = append(why, "capability ok")
		if s.MatchOptIn(p, w) == 0 {
			continue
		}
		why = append(why, "opt-in ok")
		sc := s.Score(p, w)
		pool = append(pool, scored{p: p, score: sc, why: why})
	}
	sort.SliceStable(pool, func(i, j int) bool {
		if pool[i].score == pool[j].score {
			return pool[i].p.CurrentLoadPct < pool[j].p.CurrentLoadPct
		}
		return pool[i].score > pool[j].score
	})
	if len(pool) > topN {
		pool = pool[:topN]
	}
	out := make([]Candidate, 0, len(pool))
	for _, c := range pool {
		out = append(out, Candidate{ProviderID: c.p.ID, Score: c.score, Reasons: c.why})
	}
	return out
}

func isEligible(p ProviderSnapshot) bool {
	if p.Status == "deactivated" || p.Status == "suspended" || p.Status == "offline" {
		return false
	}
	if p.State == "" {
		return true // assume active until we hear otherwise
	}
	return p.State == "SCHEDULER_STATE_ACTIVE"
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// RequiredMacosForTartImage derives the minimum host macOS MAJOR version a
// provider needs to run an iOS-build job, from the job's Tart image name.
//
// The cirruslabs image names encode the guest macOS family, and Apple
// Virtualization.framework requires host macOS >= guest macOS (ADR 0001
// Addendum 10), so the guest family IS the host minimum:
//
//	ghcr.io/cirruslabs/macos-sequoia-xcode:16.2 → 15 (Sequoia guest)
//	ghcr.io/cirruslabs/macos-sonoma-xcode:15.4   → 14 (Sonoma guest)
//	ghcr.io/cirruslabs/macos-tahoe-*             → 16 (Tahoe guest)
//
// Returns 0 (no constraint) for an empty image, an unrecognised family
// (so we don't accidentally de-schedule a custom/locally-baked image such
// as the native-runner `iogrid-ios-builder-16.2`, which runs host-direct
// with no guest VM), or a non-iOS-build job. The map is intentionally
// permissive: an unknown image falls through to today's Platform=macos
// gate rather than blocking dispatch.
func RequiredMacosForTartImage(image string) uint32 {
	img := strings.ToLower(strings.TrimSpace(image))
	if img == "" {
		return 0
	}
	// Newest-first so a name that (improbably) contained two family tokens
	// resolves to the newer requirement.
	families := []struct {
		token string
		major uint32
	}{
		{"tahoe", 16},
		{"sequoia", 15},
		{"sonoma", 14},
		{"ventura", 13},
	}
	for _, f := range families {
		if strings.Contains(img, "macos-"+f.token) || strings.Contains(img, "macos_"+f.token) {
			return f.major
		}
	}
	return 0
}

// matchesAnyGlob does case-insensitive suffix matching with one
// supported wildcard: a leading "*." that matches "any subdomain". This
// is the documented destination-blocklist syntax (e.g. "*.linkedin.com").
func matchesAnyGlob(patterns []string, host string) bool {
	h := strings.ToLower(host)
	for _, pat := range patterns {
		p := strings.ToLower(pat)
		if strings.HasPrefix(p, "*.") {
			suffix := p[1:] // ".linkedin.com"
			if strings.HasSuffix(h, suffix) || h == suffix[1:] {
				return true
			}
			continue
		}
		if h == p {
			return true
		}
	}
	return false
}
