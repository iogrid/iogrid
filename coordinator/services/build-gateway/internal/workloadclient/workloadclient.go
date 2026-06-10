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
	"fmt"
	"net/http"
	"sync"
	"time"

	"connectrpc.com/connect"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	workloadsv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/workloads/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/workloads/v1/workloadsv1connect"
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
	// GitURL and GitRef drive the preferred git-based dispatch — the VM
	// `git clone`s GitURL, checks out GitRef, then runs BuildCommand. This
	// is what the daemon's Tart driver actually executes today (the legacy
	// tarball mode via SourceBucket/SourceKey is kept for back-compat).
	GitURL string
	GitRef string
	// BuildCommand is the single shell/xcodebuild command run after
	// clone+checkout in git mode (BuildCommands[0] when set).
	BuildCommand string
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

// --- ConnectClient implementation -------------------------------------------

// ConnectClient is the production Dispatcher: it submits builds to
// workloads-svc over Connect-Go (iogrid.workloads.v1.WorkloadSubmissionService)
// as an IosBuildRequest workload, and asks workloads-svc to cancel them.
//
// The seam between the gateway's build id and the workloads-svc workload id is
// the "build_id" label: we stamp it on every submitted Workload so that when
// workloads-svc later forwards a daemon status update back to the gateway it
// can name the build directly (see workloads-svc dispatch.go forwarder).
type ConnectClient struct {
	client  workloadsv1connect.WorkloadSubmissionServiceClient
	timeout time.Duration
}

// NewConnect dials workloads-svc at baseURL. httpClient may be nil (a 30s
// client is used by default — iOS build submission is a fast enqueue call,
// but we leave generous headroom for a cold workloads-svc).
func NewConnect(baseURL string, httpClient connect.HTTPClient) *ConnectClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &ConnectClient{
		client:  workloadsv1connect.NewWorkloadSubmissionServiceClient(httpClient, baseURL),
		timeout: 30 * time.Second,
	}
}

// Submit implements Dispatcher: translates the build-shaped SubmitRequest into
// an IosBuildRequest workload and submits it to workloads-svc. The returned
// AttemptID is read from the dispatched_attempt_id label workloads-svc stamps
// on the response Workload after the scheduler picks a provider.
func (c *ConnectClient) Submit(ctx context.Context, req SubmitRequest) (SubmitResponse, error) {
	if c == nil || c.client == nil {
		return SubmitResponse{}, errors.New("workloadclient: connect client not initialised")
	}
	if c.timeout > 0 {
		cc, cancel := context.WithTimeout(ctx, c.timeout)
		defer cancel()
		ctx = cc
	}

	// Prefer the git-based command (the daemon's Tart driver runs git
	// clone+checkout+build); fall back to the legacy tarball command list.
	buildCommand := req.BuildCommand
	if buildCommand == "" && len(req.BuildCommands) > 0 {
		buildCommand = req.BuildCommands[0]
	}

	wl := &workloadsv1.Workload{
		Type:     commonv1.WorkloadType_WORKLOAD_TYPE_IOS_BUILD,
		Priority: workloadsv1.WorkloadPriority_WORKLOAD_PRIORITY_NORMAL,
		Labels: map[string]string{
			// build_id is the routing key for the status forwarder.
			"build_id": req.BuildID,
		},
		Payload: &workloadsv1.Workload_IosBuild{
			IosBuild: &workloadsv1.IosBuildRequest{
				TartImage:        req.TartImage,
				BuildCommands:    req.BuildCommands,
				ArtifactS3Bucket: req.ArtifactBucket,
				ArtifactS3Prefix: req.ArtifactPrefix,
				// git-based dispatch (preferred path).
				RepoUrl:      req.GitURL,
				GitRef:       req.GitRef,
				BuildCommand: buildCommand,
				// legacy tarball mode (set only when no git URL given).
				SourceTarballS3Key: tarballKey(req),
			},
		},
	}
	if req.WorkspaceID != "" {
		wl.WorkspaceId = &commonv1.UUID{Value: req.WorkspaceID}
	}
	if req.SubmittedByUserID != "" {
		wl.SubmittedByUserId = &commonv1.UUID{Value: req.SubmittedByUserID}
	}

	resp, err := c.client.SubmitWorkload(ctx, connect.NewRequest(&workloadsv1.SubmitWorkloadRequest{Workload: wl}))
	if err != nil {
		return SubmitResponse{}, fmt.Errorf("workloadclient: submit to workloads-svc: %w", err)
	}
	out := resp.Msg.GetWorkload()
	if out == nil {
		return SubmitResponse{}, errors.New("workloadclient: workloads-svc returned empty workload")
	}
	attemptID := ""
	if out.Labels != nil {
		attemptID = out.Labels["dispatched_attempt_id"]
	}
	if attemptID == "" {
		// Scheduler accepted the workload but no provider attempt was bound
		// yet (no eligible Mac connected). Surface the workload id so the
		// status forwarder can still correlate; the build stays dispatched
		// and the daemon attempt id arrives with the first status update.
		if out.Id != nil {
			attemptID = out.Id.Value
		}
	}
	return SubmitResponse{AttemptID: attemptID}, nil
}

// Cancel implements Dispatcher: asks workloads-svc to cancel the workload
// whose build_id label matches. workloads-svc keys its own workloads by their
// assigned id, so we cancel by the gateway build id only when the workload id
// equals it; otherwise this is a best-effort no-op the daemon resolves on its
// next heartbeat. For the devnet milestone the gateway-side optimistic
// transition is authoritative and the daemon stops the VM on TunnelClose.
func (c *ConnectClient) Cancel(ctx context.Context, buildID, reason string) error {
	if c == nil || c.client == nil {
		return errors.New("workloadclient: connect client not initialised")
	}
	if c.timeout > 0 {
		cc, cancel := context.WithTimeout(ctx, c.timeout)
		defer cancel()
		ctx = cc
	}
	_, err := c.client.CancelWorkload(ctx, connect.NewRequest(&workloadsv1.CancelWorkloadRequest{
		Id:     &commonv1.UUID{Value: buildID},
		Reason: reason,
	}))
	if err != nil {
		// A not-found cancel is benign — the build may already be terminal
		// on the workloads-svc side. Don't fail the gateway request on it.
		if connect.CodeOf(err) == connect.CodeNotFound {
			return nil
		}
		return fmt.Errorf("workloadclient: cancel workload: %w", err)
	}
	return nil
}

// tarballKey returns the legacy tarball source key only when no git URL is
// set — git mode is preferred and the two source modes are mutually exclusive
// on the wire.
func tarballKey(req SubmitRequest) string {
	if req.GitURL != "" {
		return ""
	}
	if req.SourceKey == "" {
		return ""
	}
	return req.SourceKey
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
