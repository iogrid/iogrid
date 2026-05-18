// Package filters defines the common Backend interface implemented by
// every external feed (PhishTank, OpenPhish, Google Safe Browsing,
// NCMEC PhotoDNA) and the result envelope they share.
//
// Each backend is independent and best-effort: failure of one backend
// (network blip, expired key, etc.) MUST NOT prevent other backends
// from returning their answer. The orchestrator (see filters.Orchestrator
// in orchestrator.go) fans calls out in parallel and aggregates the
// strictest decision.
package filters

import (
	"context"
	"time"

	antiabusev1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/antiabuse/v1"
)

// Backend is the minimal interface every external reputation feed
// must satisfy.
type Backend interface {
	// Name returns the backend identifier used in audit logs.
	Name() string
	// Enabled reports whether the backend is fully configured. When
	// false, CheckURL / CheckDomain MUST short-circuit to a
	// no-match Result so the surrounding orchestration can carry on.
	Enabled() bool
	// CheckURL evaluates a full URL.
	CheckURL(ctx context.Context, url string) Result
	// CheckDomain evaluates a bare hostname. Some backends (PhishTank,
	// OpenPhish) operate on URLs only; for them the implementation
	// should return a no-match Result.
	CheckDomain(ctx context.Context, domain string) Result
}

// Result is the per-backend outcome. The orchestrator aggregates these
// into a single FilterVerdict.
type Result struct {
	// Backend is the source name (e.g. "phishtank").
	Backend string
	// Match is true iff the backend has flagged the target as
	// malicious / suspicious.
	Match bool
	// Decision is the per-backend recommendation.
	Decision antiabusev1.FilterDecision
	// Reason is the machine slug surfaced in audit logs.
	Reason string
	// Explanation is the human-readable form.
	Explanation string
	// CheckedAt is when the lookup completed.
	CheckedAt time.Time
	// Err is the network / parse error if the lookup failed. When non
	// nil the caller should treat the result as ALLOW (best-effort)
	// but record the error for ops.
	Err error
}

// NewAllow returns a benign no-match Result attributed to backend.
func NewAllow(backend string) Result {
	return Result{
		Backend:   backend,
		Match:     false,
		Decision:  antiabusev1.FilterDecision_FILTER_DECISION_ALLOW,
		Reason:    "no_match",
		CheckedAt: time.Now(),
	}
}

// NewBlock returns a Result indicating the target is malicious.
func NewBlock(backend, reason, explanation string) Result {
	return Result{
		Backend:     backend,
		Match:       true,
		Decision:    antiabusev1.FilterDecision_FILTER_DECISION_BLOCK,
		Reason:      reason,
		Explanation: explanation,
		CheckedAt:   time.Now(),
	}
}

// NewError marks the lookup as failed; orchestrator treats as ALLOW.
func NewError(backend string, err error) Result {
	return Result{
		Backend:   backend,
		Match:     false,
		Decision:  antiabusev1.FilterDecision_FILTER_DECISION_ALLOW,
		Reason:    "lookup_error",
		CheckedAt: time.Now(),
		Err:       err,
	}
}
