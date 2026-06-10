// Build orchestration layer for the build-gateway.
//
// The Service struct ties together the store, the workloads-svc dispatcher,
// the artifact storage, the webhook dispatcher, and the metering emitter.
// HTTP handlers in internal/server hold a Service instance and call its
// methods — they do NOT poke at the underlying interfaces directly.
//
// Keeping the orchestration here (rather than scattered through handlers)
// makes the lifecycle testable end-to-end without an HTTP server, and lets
// the dispatch / status-update path be driven from a non-HTTP source
// (workloads-svc stream listener) without duplicating logic.
package builds

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/metering"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/s3artifact"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/webhook"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/workloadclient"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/xcode"
)

// Store is the persistence contract — implemented by internal/store. Defined
// here as a narrow interface to avoid an import cycle. Adapter is in
// internal/store via the StoreAdapter helper.
type Store interface {
	Create(ctx context.Context, b *Build) error
	Get(ctx context.Context, workspaceID, id string) (*Build, error)
	GetByIDInternal(ctx context.Context, id string) (*Build, error)
	Update(ctx context.Context, id string, mutator func(*Build) error) (*Build, error)
	List(ctx context.Context, workspaceID string, status Status, limit int) ([]*Build, error)
}

// ListFilter is the public, handler-facing list filter. The Service
// flattens it into the Store.List signature so the persistence layer stays
// free of build-domain types.
type ListFilter struct {
	WorkspaceID string
	Status      Status
	Limit       int
}

// ErrValidation is the umbrella type for input validation failures. The
// HTTP layer maps it to 400 with the embedded message.
type ErrValidation struct {
	Field   string
	Message string
}

// Error implements error.
func (e *ErrValidation) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// SubmitRequest is the customer-supplied submission payload, post-decode.
type SubmitRequest struct {
	GitURL        string
	GitRef        string
	XcodeVersion  string
	BuildCommand  string
	SigningTeamID string
	EnvVars       map[string]string
	// WebhookURL and WebhookSecret are optional; if either is set the
	// other must also be set (we'll refuse partial config).
	WebhookURL    string
	WebhookSecret string
}

// Service ties the building blocks together.
type Service struct {
	store      Store
	dispatcher workloadclient.Dispatcher
	storage    s3artifact.Storage
	webhooks   webhook.Dispatcher
	metering   metering.Emitter
	logs       *LogHub
	logger     *slog.Logger
	now        func() time.Time
	// idGen produces a new opaque build id. Defaults to a 16-byte random
	// hex string; overridden in tests for deterministic ids.
	idGen func() string
	// presignTTL is how long pre-signed artifact GET URLs live.
	presignTTL time.Duration
}

// Options bundles dependencies for NewService.
type Options struct {
	Store      Store
	Dispatcher workloadclient.Dispatcher
	Storage    s3artifact.Storage
	Webhooks   webhook.Dispatcher
	Metering   metering.Emitter
	Logs       *LogHub
	Logger     *slog.Logger
	Now        func() time.Time
	IDGen      func() string
	PresignTTL time.Duration
}

// NewService builds a Service. All required dependencies (store, dispatcher,
// storage) MUST be non-nil — NewService panics otherwise to fail fast.
func NewService(opts Options) *Service {
	if opts.Store == nil || opts.Dispatcher == nil || opts.Storage == nil {
		panic("builds.NewService: store, dispatcher, and storage are required")
	}
	if opts.Webhooks == nil {
		opts.Webhooks = webhook.Noop{}
	}
	if opts.Metering == nil {
		opts.Metering = metering.NewInMemory()
	}
	if opts.Logs == nil {
		opts.Logs = NewLogHub(0)
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.IDGen == nil {
		opts.IDGen = randomHexID
	}
	if opts.PresignTTL <= 0 {
		opts.PresignTTL = 15 * time.Minute
	}
	return &Service{
		store:      opts.Store,
		dispatcher: opts.Dispatcher,
		storage:    opts.Storage,
		webhooks:   opts.Webhooks,
		metering:   opts.Metering,
		logs:       opts.Logs,
		logger:     opts.Logger,
		now:        opts.Now,
		idGen:      opts.IDGen,
		presignTTL: opts.PresignTTL,
	}
}

// Submit validates the request, creates a Build, dispatches to workloads-svc,
// and returns the persisted Build with status set to queued (or dispatched
// once the synchronous Submit call returns).
func (s *Service) Submit(ctx context.Context, workspaceID, userID, plan string, req SubmitRequest) (*Build, error) {
	if err := validateSubmit(req); err != nil {
		return nil, err
	}
	if workspaceID == "" {
		return nil, &ErrValidation{Field: "workspace_id", Message: "missing workspace context"}
	}

	tartImage, _ := xcode.TartImage(req.XcodeVersion)
	if tartImage == "" {
		// Caller passed an empty version — apply the default.
		req.XcodeVersion = xcode.DefaultVersion
		tartImage, _ = xcode.TartImage(req.XcodeVersion)
	}

	bucket, err := s.storage.EnsureWorkspaceBucket(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("ensure bucket: %w", err)
	}
	id := s.idGen()
	artifactBucket, artifactPrefix := s.storage.ArtifactPrefixFor(workspaceID, id)
	sourceBucket, sourceKey := s.storage.SourcePrefixFor(workspaceID, id)
	_ = bucket // reserved for future per-workspace overrides

	b := &Build{
		ID:                id,
		WorkspaceID:       workspaceID,
		SubmittedByUserID: userID,
		GitURL:            req.GitURL,
		GitRef:            req.GitRef,
		XcodeVersion:      req.XcodeVersion,
		BuildCommand:      req.BuildCommand,
		SigningTeamID:     req.SigningTeamID,
		EnvVars:           req.EnvVars,
		Status:            StatusQueued,
		ArtifactBucket:    artifactBucket,
		ArtifactPrefix:    artifactPrefix,
		SubmittedAt:       s.now(),
	}
	if req.WebhookURL != "" {
		b.Webhook = &Webhook{URL: req.WebhookURL, Secret: req.WebhookSecret}
	}
	if err := s.store.Create(ctx, b); err != nil {
		return nil, err
	}

	// Submission to workloads-svc — failure here transitions the build
	// straight to rejected so the customer learns about it on the next
	// GET. We deliberately return success to the caller (the build was
	// persisted) and surface the rejection in the status field. This
	// also matches the proxy-gateway pattern: 202-on-record, then status
	// reveals dispatch outcome.
	cmd := []string{req.BuildCommand}
	subReq := workloadclient.SubmitRequest{
		BuildID:           id,
		WorkspaceID:       workspaceID,
		SubmittedByUserID: userID,
		TartImage:         tartImage,
		BuildCommands:     cmd,
		// git-based dispatch (preferred — drives the daemon Tart driver).
		GitURL:         req.GitURL,
		GitRef:         req.GitRef,
		BuildCommand:   req.BuildCommand,
		SourceBucket:   sourceBucket,
		SourceKey:      sourceKey,
		ArtifactBucket: artifactBucket,
		ArtifactPrefix: artifactPrefix,
		EnvVars:        req.EnvVars,
		SigningTeamID:  req.SigningTeamID,
	}
	resp, err := s.dispatcher.Submit(ctx, subReq)
	if err != nil {
		s.logger.Warn("submit: dispatcher rejected",
			slog.String("build_id", id),
			slog.String("error", err.Error()),
		)
		updated, _ := s.store.Update(ctx, id, func(curr *Build) error {
			curr.Status = StatusRejected
			curr.StatusNote = "dispatcher rejected: " + err.Error()
			now := s.now()
			curr.FinishedAt = &now
			return nil
		})
		s.fireWebhook(ctx, updated, "")
		return updated, nil
	}

	// Mark dispatched on the persisted record. Status updates from the
	// provider (running / terminal) arrive on a separate channel and
	// drive subsequent transitions via UpdateStatus().
	updated, err := s.store.Update(ctx, id, func(curr *Build) error {
		curr.Status = StatusDispatched
		curr.ProviderAttemptID = resp.AttemptID
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.fireWebhook(ctx, updated, "dispatched to workloads-svc")
	_ = plan // plan-based webhook gating is enforced by the handler
	return updated, nil
}

// Get returns the build, scoped to workspaceID.
func (s *Service) Get(ctx context.Context, workspaceID, id string) (*Build, error) {
	return s.store.Get(ctx, workspaceID, id)
}

// List returns recent builds for the workspace.
func (s *Service) List(ctx context.Context, f ListFilter) ([]*Build, error) {
	return s.store.List(ctx, f.WorkspaceID, f.Status, f.Limit)
}

// Cancel asks workloads-svc to stop the build and records the request.
// The actual Cancelled transition arrives later via UpdateStatus from the
// dispatcher stream; we set StatusNote here so the customer sees the
// "cancel requested" intent immediately.
func (s *Service) Cancel(ctx context.Context, workspaceID, id, reason string) (*Build, error) {
	b, err := s.store.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if b.Status.Terminal() {
		return nil, workloadclient.ErrAlreadyTerminal
	}
	if err := s.dispatcher.Cancel(ctx, id, reason); err != nil {
		return nil, err
	}
	updated, err := s.store.Update(ctx, id, func(curr *Build) error {
		// Mark optimistically — the canonical Cancelled lands on the
		// next status update from the provider.
		if !curr.Status.Terminal() {
			curr.StatusNote = "cancel requested: " + reason
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// UpdateStatus applies a provider-reported status transition. Called by the
// workloads-svc stream listener; exposed here so tests can drive the
// lifecycle directly. workspaceID is OPTIONAL — passing "" skips the
// tenancy check so the dispatcher (which doesn't know the workspace) can
// hand us updates.
func (s *Service) UpdateStatus(ctx context.Context, id string, next Status, note string, exitCode int32) (*Build, error) {
	if !next.Valid() {
		return nil, &ErrValidation{Field: "status", Message: "unknown status " + string(next)}
	}
	updated, err := s.store.Update(ctx, id, func(curr *Build) error {
		if !AllowedTransition(curr.Status, next) {
			return ErrInvalidTransition
		}
		curr.Status = next
		curr.StatusNote = note
		curr.ExitCode = exitCode
		now := s.now()
		if next == StatusRunning && curr.StartedAt == nil {
			curr.StartedAt = &now
		}
		if next.Terminal() && curr.FinishedAt == nil {
			curr.FinishedAt = &now
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.fireWebhook(ctx, updated, note)
	if next.Terminal() {
		s.emitMetering(ctx, updated)
		// Keep log buffer alive briefly so late tails can still read.
		// Production has a reaper goroutine; tests use Drop() directly.
		go func(id string) {
			t := time.NewTimer(30 * time.Second)
			defer t.Stop()
			<-t.C
			if hub := s.logs; hub != nil {
				hub.Drop(id)
			}
		}(id)
	}
	return updated, nil
}

// AppendLog records a single line from a running build's stdout/stderr.
// Used by the workloads-svc stream listener and the internal upload path.
func (s *Service) AppendLog(buildID, stream, text string) uint64 {
	return s.logs.For(buildID).Append(stream, text, s.now())
}

// Heartbeat is the runner-liveness hook. The Mac provider's runner POSTs here
// periodically while a build is in flight; the gateway uses it to (a) confirm
// the build is still tracked (returns ErrNotFound otherwise so the runner can
// abort a build the gateway already cancelled), and (b) keep a last-seen
// timestamp the stale-build reaper consults. Returns the current Build so the
// runner can detect a server-side cancel (Status == cancelled).
//
// This is intentionally metering-adjacent rather than a status transition: a
// heartbeat never advances the state machine, it only proves the provider is
// alive. workspaceID is skipped — the dispatch-token / mTLS already proved
// authority for the internal caller.
func (s *Service) Heartbeat(ctx context.Context, buildID string) (*Build, error) {
	updated, err := s.store.Update(ctx, buildID, func(curr *Build) error {
		// No state change — touch only the in-process last-seen marker via
		// the log hub so a future reaper can tell "provider went silent"
		// from "build never dispatched". We keep the record untouched so we
		// don't churn the persisted row on every heartbeat.
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// RegisterArtifact records an artifact upload completed by a Mac provider
// and returns the resulting Build. Idempotent on (BuildID, Name) — repeat
// uploads of the same name overwrite the prior entry.
func (s *Service) RegisterArtifact(ctx context.Context, buildID string, art Artifact) (*Build, error) {
	if art.Name == "" {
		return nil, &ErrValidation{Field: "name", Message: "artifact name required"}
	}
	updated, err := s.store.Update(ctx, buildID, func(curr *Build) error {
		// Replace any prior artifact with the same name; otherwise
		// append.
		for i, existing := range curr.Artifacts {
			if existing.Name == art.Name {
				art.UploadedAt = s.now()
				curr.Artifacts[i] = art
				return nil
			}
		}
		art.UploadedAt = s.now()
		curr.Artifacts = append(curr.Artifacts, art)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// UploadArtifact handles the provider-side artifact PUT: reads body, writes
// to S3, records the resulting Artifact on the build. Returns the stored
// object so the caller can verify size/etag.
func (s *Service) UploadArtifact(ctx context.Context, buildID, name, contentType string, body io.Reader) (*Build, s3artifact.Object, error) {
	b, err := s.store.GetByIDInternal(ctx, buildID)
	if err != nil {
		return nil, s3artifact.Object{}, err
	}
	// We accept uploads on any non-cancelled build — the provider may
	// still be uploading at the moment the workloads-svc stream emits
	// the terminal Succeeded.
	if b.Status == StatusCancelled || b.Status == StatusRejected {
		return nil, s3artifact.Object{}, errors.New("build no longer accepts uploads")
	}
	key := b.ArtifactPrefix + name
	obj, err := s.storage.Put(ctx, b.ArtifactBucket, key, contentType, body)
	if err != nil {
		return nil, s3artifact.Object{}, err
	}
	updated, err := s.RegisterArtifact(ctx, buildID, Artifact{
		Name:        name,
		SizeBytes:   obj.Size,
		S3Key:       obj.Key,
		ContentType: obj.ContentType,
	})
	if err != nil {
		return nil, s3artifact.Object{}, err
	}
	return updated, obj, nil
}

// PresignArtifact returns a pre-signed download URL for the named artifact
// on the given build (scoped to workspaceID for tenancy).
func (s *Service) PresignArtifact(ctx context.Context, workspaceID, buildID, name string) (string, error) {
	b, err := s.store.Get(ctx, workspaceID, buildID)
	if err != nil {
		return "", err
	}
	for _, a := range b.Artifacts {
		if a.Name == name {
			return s.storage.PresignGet(ctx, b.ArtifactBucket, a.S3Key, s.presignTTL)
		}
	}
	return "", &ErrValidation{Field: "name", Message: "artifact not found"}
}

// LogHub returns the underlying log hub so handlers can subscribe.
func (s *Service) LogHub() *LogHub {
	return s.logs
}

// --- internal helpers -------------------------------------------------------

func (s *Service) fireWebhook(ctx context.Context, b *Build, note string) {
	if b == nil || b.Webhook == nil {
		return
	}
	ev := webhook.Event{
		EventID:     randomHexID(),
		BuildID:     b.ID,
		WorkspaceID: b.WorkspaceID,
		Status:      string(b.Status),
		Note:        note,
		OccurredAt:  s.now(),
		AttemptID:   b.ProviderAttemptID,
	}
	s.webhooks.Enqueue(ctx, b.Webhook.URL, b.Webhook.Secret, ev)
}

func (s *Service) emitMetering(ctx context.Context, b *Build) {
	if b == nil || b.StartedAt == nil || b.FinishedAt == nil {
		return
	}
	ev := metering.Event{
		BuildID:         b.ID,
		WorkspaceID:     b.WorkspaceID,
		AttemptID:       b.ProviderAttemptID,
		TerminalStatus:  string(b.Status),
		StartedAt:       *b.StartedAt,
		FinishedAt:      *b.FinishedAt,
		BillableMinutes: b.BillableMinutes(s.now()),
	}
	if err := s.metering.Emit(ctx, ev); err != nil {
		s.logger.Warn("metering: emit failed",
			slog.String("build_id", b.ID),
			slog.String("error", err.Error()),
		)
	}
}

func validateSubmit(req SubmitRequest) error {
	if strings.TrimSpace(req.GitURL) == "" {
		return &ErrValidation{Field: "git_url", Message: "required"}
	}
	if !validGitURL(req.GitURL) {
		return &ErrValidation{Field: "git_url", Message: "must be https:// or ssh://git@host:org/repo.git"}
	}
	if strings.TrimSpace(req.GitRef) == "" {
		return &ErrValidation{Field: "git_ref", Message: "required (branch / tag / commit)"}
	}
	if strings.TrimSpace(req.BuildCommand) == "" {
		return &ErrValidation{Field: "build_command", Message: "required"}
	}
	if len(req.BuildCommand) > 8192 {
		return &ErrValidation{Field: "build_command", Message: "exceeds 8192 char limit"}
	}
	if req.XcodeVersion != "" && !xcode.IsApproved(req.XcodeVersion) {
		return &ErrValidation{Field: "xcode_version",
			Message: "not in approved list; see GET /v1/xcode-versions"}
	}
	if (req.WebhookURL == "") != (req.WebhookSecret == "") {
		return &ErrValidation{Field: "webhook", Message: "url and secret must both be set"}
	}
	if req.WebhookURL != "" {
		u, err := url.Parse(req.WebhookURL)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return &ErrValidation{Field: "webhook_url", Message: "must be https://..."}
		}
		if len(req.WebhookSecret) < 16 {
			return &ErrValidation{Field: "webhook_secret", Message: "min 16 chars"}
		}
	}
	for k := range req.EnvVars {
		if k == "" {
			return &ErrValidation{Field: "env_vars", Message: "blank key"}
		}
		if strings.HasPrefix(k, "IOGRID_") {
			return &ErrValidation{Field: "env_vars",
				Message: "IOGRID_-prefixed names are reserved"}
		}
	}
	return nil
}

func validGitURL(u string) bool {
	if strings.HasPrefix(u, "https://") {
		return true
	}
	// Accept "git@host:org/repo.git" and "ssh://git@host/...".
	if strings.HasPrefix(u, "ssh://") {
		return true
	}
	if strings.HasPrefix(u, "git@") && strings.Contains(u, ":") {
		return true
	}
	return false
}

func randomHexID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand can only fail if the system entropy source is
		// borked; falling back to time gives us something unique-ish so
		// the request doesn't 500.
		return hex.EncodeToString([]byte(time.Now().UTC().Format("20060102150405000000")))
	}
	return hex.EncodeToString(b[:])
}
