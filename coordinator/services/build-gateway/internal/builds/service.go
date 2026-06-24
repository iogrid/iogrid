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

	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/gridsettle"
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
	// ListNonTerminal returns every build (across all workspaces) whose status
	// is not terminal — i.e. still queued/dispatched/running. The stale-build
	// reaper (#811) consults this to find rows stuck past their TTL with no
	// daemon heartbeat so it can fail them rather than leak the slot forever.
	ListNonTerminal(ctx context.Context) ([]*Build, error)
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
	gridSettle gridsettle.Settler
	wallets    gridsettle.WalletResolver
	// providerWallets resolves a build's provider_id to the provider owner's
	// $GRID payout wallet so settlement pays the provider on-chain (#748).
	providerWallets gridsettle.ProviderWalletResolver
	logs            *LogHub
	logger          *slog.Logger
	now             func() time.Time
	// idGen produces a new opaque build id. Defaults to a 16-byte random
	// hex string; overridden in tests for deterministic ids.
	idGen func() string
	// presignTTL is how long pre-signed artifact GET URLs live.
	presignTTL time.Duration
}

// Options bundles dependencies for NewService.
type Options struct {
	Store           Store
	Dispatcher      workloadclient.Dispatcher
	Storage         s3artifact.Storage
	Webhooks        webhook.Dispatcher
	Metering        metering.Emitter
	GridSettle      gridsettle.Settler
	Wallets         gridsettle.WalletResolver
	ProviderWallets gridsettle.ProviderWalletResolver
	Logs            *LogHub
	Logger          *slog.Logger
	Now             func() time.Time
	IDGen           func() string
	PresignTTL      time.Duration
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
	if opts.GridSettle == nil {
		opts.GridSettle = gridsettle.Noop{}
	}
	if opts.Wallets == nil {
		opts.Wallets = gridsettle.NoopWalletResolver{}
	}
	if opts.ProviderWallets == nil {
		opts.ProviderWallets = gridsettle.NoopProviderWalletResolver{}
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
		store:           opts.Store,
		dispatcher:      opts.Dispatcher,
		storage:         opts.Storage,
		webhooks:        opts.Webhooks,
		metering:        opts.Metering,
		gridSettle:      opts.GridSettle,
		wallets:         opts.Wallets,
		providerWallets: opts.ProviderWallets,
		logs:            opts.Logs,
		logger:          opts.Logger,
		now:             opts.Now,
		idGen:           opts.IDGen,
		presignTTL:      opts.PresignTTL,
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
	// Resolve the customer's $GRID wallet so the finished build can settle
	// the provider's earnings (#718). Best-effort: an empty wallet (no
	// binding / resolver error) just makes settlement a no-op later.
	if s.wallets != nil && userID != "" {
		if wallet, werr := s.wallets.ResolveWallet(ctx, userID); werr != nil {
			s.logger.Warn("wallet resolve failed; build will not settle",
				slog.String("build_id", id), slog.String("error", werr.Error()))
		} else {
			b.CustomerWallet = wallet
		}
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
func (s *Service) UpdateStatus(ctx context.Context, id string, next Status, note, providerID string, exitCode int32) (*Build, error) {
	if !next.Valid() {
		return nil, &ErrValidation{Field: "status", Message: "unknown status " + string(next)}
	}
	// dropped flags the #811 split-brain case: a stream-origin scheduler_paused
	// rejection that arrived while the poll path already advanced this build to
	// running/dispatched. We must NOT downgrade it to rejected, and we must NOT
	// 409 the caller (workloads-svc forwards best-effort and would just log the
	// error). We silently keep the advanced status and warn. The mutator sets
	// this flag and returns nil so the store leaves the record untouched.
	var dropped bool
	updated, err := s.store.Update(ctx, id, func(curr *Build) error {
		allowed, downgrade := AllowStatusUpdate(curr.Status, next, note)
		if downgrade {
			dropped = true
			return nil
		}
		if !allowed {
			return ErrInvalidTransition
		}
		curr.Status = next
		curr.StatusNote = note
		curr.ExitCode = exitCode
		// Record the running provider (atomic with the status write so the
		// terminal emitMetering below sees it). Don't clobber a known id with
		// an empty one if a later callback omits it (#744).
		if providerID != "" {
			curr.ProviderID = providerID
		}
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
	if dropped {
		s.logger.Warn("dropped non-authoritative stream rejection (split-brain #811)",
			slog.String("build_id", id),
			slog.String("current_status", string(updated.Status)),
			slog.String("rejected_status", string(next)),
			slog.String("note", note),
		)
		// Return the unchanged build; no webhook/metering/settle — nothing
		// transitioned. The poll path remains authoritative for the terminal.
		return updated, nil
	}
	s.fireWebhook(ctx, updated, note)
	if next.Terminal() {
		s.emitMetering(ctx, updated)
		s.settleGrid(ctx, updated)
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

// DefaultStaleTTL is how long a build may sit in a non-terminal state without
// progress before the reaper fails it. An Expo SDK 54 / Xcode 26 iOS build is
// the longest workload the gateway dispatches; 60m is comfortably past a real
// cold build (clone + pod install + xcodebuild) so a still-running build is
// never reaped, but a daemon that died mid-build (or a poll assignment that was
// never picked up) no longer leaks the slot forever (#811).
const DefaultStaleTTL = 60 * time.Minute

// ReapStale fails every build that has been stuck in a non-terminal state past
// ttl, measured from its last progress timestamp (StartedAt for running builds,
// SubmittedAt for queued/dispatched ones that never started). Returns the ids
// it reaped. The transition runs through the normal lifecycle (sets FinishedAt,
// fires metering/settle/webhook) so a reaped running build still meters the
// wall-clock it occupied. Idempotent: a build already terminal is skipped.
//
// Production wires a goroutine that calls this on a ticker (see
// cmd/build-gateway/main.go); tests call it directly with a frozen clock.
func (s *Service) ReapStale(ctx context.Context, ttl time.Duration) ([]string, error) {
	if ttl <= 0 {
		ttl = DefaultStaleTTL
	}
	stale, err := s.store.ListNonTerminal(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now()
	var reaped []string
	for _, b := range stale {
		if b.Status.Terminal() {
			continue
		}
		// last-progress marker: a running build is timed from when it started;
		// a build that never started is timed from submission.
		last := b.SubmittedAt
		if b.StartedAt != nil {
			last = *b.StartedAt
		}
		if now.Sub(last) < ttl {
			continue
		}
		updated, uerr := s.UpdateStatus(ctx, b.ID, StatusFailed,
			"reaped: no provider heartbeat within "+ttl.String(), b.ProviderID, -1)
		if uerr != nil {
			// A concurrent terminal transition (ErrInvalidTransition) just
			// means the build finished under us — not an error for the reaper.
			s.logger.Warn("reap: skip build (transitioned concurrently)",
				slog.String("build_id", b.ID),
				slog.String("error", uerr.Error()))
			continue
		}
		reaped = append(reaped, updated.ID)
	}
	if len(reaped) > 0 {
		s.logger.Info("reaped stale builds",
			slog.Int("count", len(reaped)),
			slog.String("ttl", ttl.String()))
	}
	return reaped, nil
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

// settleGrid pays the provider's devnet $GRID for a finished build (#700/
// #712). It computes consumed = billable-minutes × rate and POSTs it to
// billing-svc. The customer wallet isn't resolvable from a build yet (it
// carries a WorkspaceID, not a wallet) — the HTTPSettler treats an empty
// wallet as a no-op, so this is wired + exercised ahead of the
// workspace→wallet binding + build escrow tracked in #718.
func (s *Service) settleGrid(ctx context.Context, b *Build) {
	if s.gridSettle == nil || b == nil || b.StartedAt == nil || b.FinishedAt == nil {
		return
	}
	consumed := gridsettle.BillableToAtomic(b.BillableMinutes(s.now()), gridsettle.DefaultRatePerMinuteAtomic)
	// Resolve the provider's payout wallet (provider_id → owner user → bound
	// $GRID wallet) so the settlement row carries a non-empty provider_wallet.
	// The settlement-worker only drains rows WHERE provider_wallet <> '', so
	// without this the provider is never paid on-chain (#748). Best-effort: an
	// unresolved wallet stays empty (logged) and settlement still records the
	// row for later reconciliation.
	var providerWallet string
	if s.providerWallets != nil && b.ProviderID != "" {
		if w, err := s.providerWallets.ResolveProviderWallet(ctx, b.ProviderID); err != nil {
			s.logger.Warn("provider wallet resolve failed; build settles without provider payout",
				slog.String("build_id", b.ID),
				slog.String("provider_id", b.ProviderID),
				slog.String("error", err.Error()),
			)
		} else {
			providerWallet = w
		}
	}
	in := gridsettle.BuildSettleInput{
		BuildID:        b.ID,
		AttemptID:      b.ProviderAttemptID,
		CustomerWallet: b.CustomerWallet, // empty until #718 — no-op then
		ProviderWallet: providerWallet,
		ProviderID:     b.ProviderID,
		EscrowedAtomic: consumed,
		ConsumedAtomic: consumed,
	}
	if err := s.gridSettle.SettleBuild(ctx, in); err != nil {
		s.logger.Warn("grid settle failed",
			slog.String("build_id", b.ID),
			slog.String("error", err.Error()),
		)
	}
}

func (s *Service) emitMetering(ctx context.Context, b *Build) {
	if b == nil || b.StartedAt == nil || b.FinishedAt == nil {
		return
	}
	ev := metering.Event{
		BuildID:         b.ID,
		WorkspaceID:     b.WorkspaceID,
		AttemptID:       b.ProviderAttemptID,
		ProviderID:      b.ProviderID,
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
