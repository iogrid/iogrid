// Package workloadclient is the build-gateway's adapter onto workloads-svc.
//
// The gateway never talks to providers directly — it asks workloads-svc to
// dispatch an IosBuildRequest to a matching Mac provider, and learns about
// progress from the same service via the events stream.
//
// In production this is a Connect-Go client against
// iogrid.workloads.v1.WorkloadSubmissionService. The Dispatcher interface
// here is the seam — tests use the InMemory implementation which captures
// submissions in a slice and lets the test loop drive status updates.
package workloadclient

import (
	"context"
	"errors"
	"sync"
	"time"
)

// SubmitRequest is the build-gateway-shaped dispatch payload. We don't pass
// the proto type around so the rest of the service stays free of a hard
// dependency on the generated pb code.
type SubmitRequest struct {
	// BuildID is the gateway's id for the build, reused as the workload
	// id once the proto crosses the wire.
	BuildID string
	// WorkspaceID owns the build.
	WorkspaceID string
	// SubmittedByUserID is the customer user who fired the submission.
	// May be empty for service-tier keys.
	SubmittedByUserID string
	// TartImage is the Cirrus Labs macOS image slug the provider must
	// spawn (e.g. "ghcr.io/cirruslabs/macos-sequoia-xcode:16.2").
	TartImage string
	// BuildCommands is the shell command list the VM runs after cloning
	// source.
	BuildCommands []string
	// SourceBucket and SourceKey point at the customer-uploaded source
	// tarball. Empty for git-clone-mode builds (rare; we still resolve
	// the git checkout server-side and stage a tarball).
	SourceBucket string
	SourceKey    string
	// ArtifactBucket and ArtifactPrefix tell the provider where to push
	// build outputs.
	ArtifactBucket string
	ArtifactPrefix string
	// EnvVars are exported into the VM before BuildCommands run.
	EnvVars map[string]string
	// SigningTeamID is the Apple Developer Program team to sign with.
	// Empty for unsigned (CI-only) builds.
	SigningTeamID string
}

// SubmitResponse carries back the workloads-svc-assigned attempt id.
type SubmitResponse struct {
	// AttemptID is per-attempt, not per-workload. Retries on a different
	// provider get a fresh AttemptID.
	AttemptID string
}

// Dispatcher is the seam against workloads-svc.
type Dispatcher interface {
	// Submit dispatches the build to workloads-svc. Returns the
	// attempt-id assigned for the first attempt.
	Submit(ctx context.Context, req SubmitRequest) (SubmitResponse, error)
	// Cancel asks workloads-svc to terminate the build. Returns nil if
	// the request was accepted (the build will then transition through
	// Cancelled once the provider acks).
	Cancel(ctx context.Context, buildID, reason string) error
}

// ErrAlreadyTerminal is what Cancel returns if the build has already
// reached a terminal status by the time the request lands. The HTTP layer
// translates it to 409.
var ErrAlreadyTerminal = errors.New("workloadclient: build already terminal")

// --- InMemory implementation ------------------------------------------------

// InMemory captures dispatch calls in-process. Suitable for unit tests and
// integration tests that run the gateway against a synthetic dispatcher.
type InMemory struct {
	mu          sync.Mutex
	submissions []SubmitRequest
	cancels     []string
	// nextAttemptID is incremented monotonically so each submission gets
	// a unique id without pulling in uuid.
	nextAttemptID int
	// now is the clock source for synthesised attempt ids.
	now func() time.Time
	// onSubmit, if set, runs after each successful Submit — used by
	// tests to drive the build through subsequent state transitions.
	onSubmit func(req SubmitRequest, attemptID string)
	// shouldFail, if set, makes the next Submit fail with this error.
	shouldFail error
}

// NewInMemory builds an empty dispatcher.
func NewInMemory(now func() time.Time) *InMemory {
	if now == nil {
		now = time.Now
	}
	return &InMemory{now: now}
}

// OnSubmit installs a callback fired after every successful Submit. Useful
// for test code that wants to immediately mark the build running /
// succeeded.
func (m *InMemory) OnSubmit(fn func(req SubmitRequest, attemptID string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onSubmit = fn
}

// FailNextSubmit makes the NEXT Submit return err. Used to test error paths.
func (m *InMemory) FailNextSubmit(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldFail = err
}

// Submissions returns a defensive copy of all captured submissions.
func (m *InMemory) Submissions() []SubmitRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]SubmitRequest, len(m.submissions))
	copy(out, m.submissions)
	return out
}

// Cancels returns the build ids that had Cancel called.
func (m *InMemory) Cancels() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.cancels))
	copy(out, m.cancels)
	return out
}

// Submit implements Dispatcher.
func (m *InMemory) Submit(_ context.Context, req SubmitRequest) (SubmitResponse, error) {
	m.mu.Lock()
	if m.shouldFail != nil {
		err := m.shouldFail
		m.shouldFail = nil
		m.mu.Unlock()
		return SubmitResponse{}, err
	}
	m.nextAttemptID++
	attemptID := "attempt-stub-" + m.now().UTC().Format("20060102150405")
	// Append a monotonically-rising counter so attempt ids are unique
	// even when two submissions land in the same wallclock second.
	attemptID += "-" + intToStr(m.nextAttemptID)
	m.submissions = append(m.submissions, req)
	cb := m.onSubmit
	m.mu.Unlock()
	if cb != nil {
		cb(req, attemptID)
	}
	return SubmitResponse{AttemptID: attemptID}, nil
}

// Cancel implements Dispatcher.
func (m *InMemory) Cancel(_ context.Context, buildID, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cancels = append(m.cancels, buildID)
	return nil
}

// intToStr is a tiny stdlib-free int formatter so this package doesn't
// drag strconv into a hot path.
func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}
