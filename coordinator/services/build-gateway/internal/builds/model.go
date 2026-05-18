// Package builds defines the customer-visible build job model and the
// state-machine that the build-gateway maintains.
//
// The build-gateway is the only owner of this state. Mac providers report
// progress via the workloads-svc dispatch stream, which the gateway
// translates into Status transitions here.
package builds

import (
	"errors"
	"time"
)

// Status is the customer-facing lifecycle of a build job. Mirrors the
// dispatch-side iogrid.workloads.v1.WorkloadStatus enum but is kept as a
// stable string in our public API so the JSON shape doesn't drift if the
// proto enum is renumbered.
type Status string

const (
	StatusQueued     Status = "queued"
	StatusDispatched Status = "dispatched"
	StatusRunning    Status = "running"
	StatusSucceeded  Status = "succeeded"
	StatusFailed     Status = "failed"
	StatusTimedOut   Status = "timed_out"
	StatusCancelled  Status = "cancelled"
	StatusRejected   Status = "rejected"
)

// Terminal reports whether the status is final (the gateway will stop
// emitting updates and stop streaming logs once a build reaches one of
// these states).
func (s Status) Terminal() bool {
	switch s {
	case StatusSucceeded, StatusFailed, StatusTimedOut, StatusCancelled, StatusRejected:
		return true
	default:
		return false
	}
}

// Valid reports whether s is a known status value.
func (s Status) Valid() bool {
	switch s {
	case StatusQueued, StatusDispatched, StatusRunning,
		StatusSucceeded, StatusFailed, StatusTimedOut,
		StatusCancelled, StatusRejected:
		return true
	default:
		return false
	}
}

// ErrInvalidTransition is returned when a caller tries to advance a build
// from a terminal state, or to walk it backwards. The state machine is
// strictly monotonic.
var ErrInvalidTransition = errors.New("invalid build status transition")

// AllowedTransition reports whether moving from cur to next is permitted.
// Specifically:
//
//   - Any non-terminal status may move to any other status.
//   - A terminal status is sticky — no further transitions are accepted.
//   - A no-op transition (cur == next) is also permitted so idempotent
//     status updates from the provider don't 409.
func AllowedTransition(cur, next Status) bool {
	if cur == next {
		return true
	}
	if cur.Terminal() {
		return false
	}
	return next.Valid()
}

// Artifact describes a single file produced by a successful build.
type Artifact struct {
	// Name is the basename the provider chose for the artifact (e.g.
	// "MyApp.ipa"). Customer-supplied download paths reference this.
	Name string `json:"name"`
	// SizeBytes is the on-S3 object length. 0 if not yet known.
	SizeBytes int64 `json:"size_bytes"`
	// S3Key is the canonical object key inside the workspace bucket.
	// Never exposed to the customer; used internally to mint pre-signed
	// download URLs.
	S3Key string `json:"-"`
	// ContentType is the MIME type the provider claimed when uploading.
	ContentType string `json:"content_type,omitempty"`
	// SHA256 is the hex-encoded digest the provider attached so the
	// customer can verify the download.
	SHA256 string `json:"sha256,omitempty"`
	// UploadedAt is when the artifact was registered with the gateway.
	UploadedAt time.Time `json:"uploaded_at"`
}

// Webhook is a per-build delivery target customers register at submission
// time. Triggered on every status transition.
type Webhook struct {
	// URL must be an https:// endpoint. Plain http is refused at
	// validation time.
	URL string `json:"url"`
	// Secret is the HMAC-SHA256 key used to sign the body. Set on submit,
	// never returned in responses.
	Secret string `json:"-"`
}

// Build is the canonical persisted record of an in-flight or completed iOS
// build job.
type Build struct {
	ID                string            `json:"id"`
	WorkspaceID       string            `json:"workspace_id"`
	SubmittedByUserID string            `json:"submitted_by_user_id,omitempty"`
	GitURL            string            `json:"git_url"`
	GitRef            string            `json:"git_ref"`
	XcodeVersion      string            `json:"xcode_version"`
	BuildCommand      string            `json:"build_command"`
	SigningTeamID     string            `json:"signing_team_id,omitempty"`
	EnvVars           map[string]string `json:"env_vars,omitempty"`
	Status            Status            `json:"status"`
	StatusNote        string            `json:"status_note,omitempty"`
	ExitCode          int32             `json:"exit_code"`
	ArtifactBucket    string            `json:"-"`
	ArtifactPrefix    string            `json:"-"`
	Artifacts         []Artifact        `json:"artifacts,omitempty"`
	Webhook           *Webhook          `json:"webhook,omitempty"`
	SubmittedAt       time.Time         `json:"submitted_at"`
	StartedAt         *time.Time        `json:"started_at,omitempty"`
	FinishedAt        *time.Time        `json:"finished_at,omitempty"`
	// ProviderAttemptID is the attempt_id workloads-svc gave us when we
	// dispatched. Embedded so /artifacts uploads can be tied back to the
	// originating run.
	ProviderAttemptID string `json:"-"`
}

// BillableMinutes is the wall-clock duration the build occupied a provider,
// rounded up to the nearest whole minute. Used by the metering hook.
//
// Builds that never started bill zero. Builds that started but haven't
// finished yet bill against "now" — useful for in-progress quota
// projections, never recorded as a billing event.
func (b *Build) BillableMinutes(now time.Time) int64 {
	if b.StartedAt == nil {
		return 0
	}
	end := now
	if b.FinishedAt != nil {
		end = *b.FinishedAt
	}
	d := end.Sub(*b.StartedAt)
	if d <= 0 {
		return 0
	}
	minutes := int64(d / time.Minute)
	if d%time.Minute > 0 {
		minutes++
	}
	return minutes
}
