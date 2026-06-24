// Package builds defines the customer-visible build job model and the
// state-machine that the build-gateway maintains.
//
// The build-gateway is the only owner of this state. Mac providers report
// progress via the workloads-svc dispatch stream, which the gateway
// translates into Status transitions here.
package builds

import (
	"errors"
	"strings"
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

// SchedulerPausedReason is the rejection-note slug the daemon emits when the
// dispatch STREAM path declines a workload because the provider's scheduler is
// paused (an idle-/bandwidth-sharing policy). For iOS builds this rejection is
// NOT authoritative: the build is delivered and driven by the POLL path
// (build_poller, #705/#715), and the scheduler-pause gate has no business
// terminating a customer-paid build the poll path is concurrently running.
//
// The daemon already suppresses this for iOS builds on its side (#742) by
// returning early from the stream Assignment handler, but ordering on the
// daemon lost that race once (#806 recorded rejected/-1 while the poll path
// ran the build to exit 0). This server-side constant lets the gateway refuse
// the downgrade regardless of daemon ordering — the authoritative fix for the
// gateway-side recurrence (#811).
const SchedulerPausedReason = "scheduler_paused"

// IsStreamSchedulerRejection reports whether an incoming status update is a
// dispatch-stream scheduler-pause rejection: a `rejected` status whose note
// carries the scheduler_paused slug. These are the ONLY rejections the gateway
// treats as non-authoritative against a poll-advanced build — a genuine
// never-dispatched rejection (e.g. "dispatcher rejected: ..." emitted by
// Submit when workloads-svc has no eligible provider) carries a different note
// and stays authoritative.
func IsStreamSchedulerRejection(next Status, note string) bool {
	if next != StatusRejected {
		return false
	}
	return strings.Contains(note, SchedulerPausedReason)
}

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

// AllowStatusUpdate is the source-aware transition guard the Service applies on
// every status update. It layers the split-brain protection (#811) on top of
// the plain AllowedTransition monotonicity:
//
//   - A stream-origin scheduler_paused rejection (IsStreamSchedulerRejection)
//     must NOT downgrade a build the POLL path has already advanced to
//     `running` or `dispatched`. A build cannot be both 'never started' and
//     'running'; the path that actually executed work wins. The update is
//     dropped (allowed=false, downgrade=true) so the caller keeps the advanced
//     status and logs a warn rather than 409-ing.
//   - Every other transition defers to AllowedTransition.
//
// downgrade is true only for the split-brain case, so the Service can
// distinguish "ignore this non-authoritative rejection" (warn, keep state, no
// error) from a genuine illegal transition (ErrInvalidTransition / 409).
func AllowStatusUpdate(cur, next Status, note string) (allowed, downgrade bool) {
	if IsStreamSchedulerRejection(next, note) {
		// The poll path owns iOS-build lifecycle: a `dispatched` iOS build is
		// awaiting/being claimed by the poll path, and `running` means it has
		// claimed and is executing. A stream scheduler-pause rejection arriving
		// in either state is the #811 split-brain — drop it.
		if cur == StatusRunning || cur == StatusDispatched {
			return false, true
		}
	}
	return AllowedTransition(cur, next), false
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
	ID                string `json:"id"`
	WorkspaceID       string `json:"workspace_id"`
	SubmittedByUserID string `json:"submitted_by_user_id,omitempty"`
	// CustomerWallet is the devnet $GRID address the provider's build
	// earnings settle against. Resolved from the workspace→wallet binding
	// (iogrid/iogrid#718); empty until then, which makes settlement a
	// no-op rather than an error.
	CustomerWallet string            `json:"customer_wallet,omitempty"`
	GitURL         string            `json:"git_url"`
	GitRef         string            `json:"git_ref"`
	XcodeVersion   string            `json:"xcode_version"`
	BuildCommand   string            `json:"build_command"`
	SigningTeamID  string            `json:"signing_team_id,omitempty"`
	EnvVars        map[string]string `json:"env_vars,omitempty"`
	Status         Status            `json:"status"`
	StatusNote     string            `json:"status_note,omitempty"`
	ExitCode       int32             `json:"exit_code"`
	ArtifactBucket string            `json:"-"`
	ArtifactPrefix string            `json:"-"`
	Artifacts      []Artifact        `json:"artifacts,omitempty"`
	Webhook        *Webhook          `json:"webhook,omitempty"`
	SubmittedAt    time.Time         `json:"submitted_at"`
	StartedAt      *time.Time        `json:"started_at,omitempty"`
	FinishedAt     *time.Time        `json:"finished_at,omitempty"`
	// ProviderAttemptID is the attempt_id workloads-svc gave us when we
	// dispatched. Embedded so /artifacts uploads can be tied back to the
	// originating run.
	ProviderAttemptID string `json:"-"`
	// ProviderID is the daemon that ran the build, learned from the
	// workloads-svc status callback. Needed to attribute the build's metered
	// minutes to the right provider's earnings (#744). "" until a status
	// callback that carries it arrives.
	ProviderID string `json:"-"`
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
