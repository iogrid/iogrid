package builds_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/builds"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/metering"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/s3artifact"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/store"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/webhook"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/workloadclient"
)

// newTestService stands up the entire dependency graph against the
// in-memory implementations. We freeze the clock so BillableMinutes
// assertions don't depend on test wall-clock.
func newTestService(t *testing.T) (*builds.Service, *fixture) {
	t.Helper()
	clock := newClock(time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC))
	st := store.NewInMemory(clock.Now)
	disp := workloadclient.NewInMemory(clock.Now)
	storage := s3artifact.NewInMemory(clock.Now, "")
	recorder := webhook.NewRecorder()
	mete := metering.NewInMemory()
	hub := builds.NewLogHub(64)
	svc := builds.NewService(builds.Options{
		Store:      st,
		Dispatcher: disp,
		Storage:    storage,
		Webhooks:   recorder,
		Metering:   mete,
		Logs:       hub,
		Now:        clock.Now,
	})
	return svc, &fixture{
		clock:    clock,
		store:    st,
		disp:     disp,
		storage:  storage,
		webhook:  recorder,
		metering: mete,
		hub:      hub,
	}
}

type fixture struct {
	clock    *fakeClock
	store    *store.InMemory
	disp     *workloadclient.InMemory
	storage  *s3artifact.InMemory
	webhook  *webhook.Recorder
	metering *metering.InMemory
	hub      *builds.LogHub
}

type fakeClock struct {
	now time.Time
}

func newClock(t time.Time) *fakeClock { return &fakeClock{now: t} }
func (c *fakeClock) Now() time.Time   { return c.now }
func (c *fakeClock) Advance(d time.Duration) {
	c.now = c.now.Add(d)
}

func validSubmit() builds.SubmitRequest {
	return builds.SubmitRequest{
		GitURL:       "https://github.com/example/ios-app.git",
		GitRef:       "main",
		XcodeVersion: "16.2",
		BuildCommand: "xcodebuild -scheme App -destination 'generic/platform=iOS' archive",
	}
}

func TestSubmit_Validation(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	ctx := context.Background()

	cases := []struct {
		name  string
		req   builds.SubmitRequest
		field string
	}{
		{"missing git_url", builds.SubmitRequest{GitRef: "x", BuildCommand: "y"}, "git_url"},
		{"http git_url", builds.SubmitRequest{GitURL: "http://example.com/foo.git", GitRef: "x", BuildCommand: "y"}, "git_url"},
		{"missing git_ref", builds.SubmitRequest{GitURL: "https://x/y.git", BuildCommand: "y"}, "git_ref"},
		{"missing build_command", builds.SubmitRequest{GitURL: "https://x/y.git", GitRef: "main"}, "build_command"},
		{"bad xcode version", builds.SubmitRequest{GitURL: "https://x/y.git", GitRef: "main", BuildCommand: "y", XcodeVersion: "12.0"}, "xcode_version"},
		{"reserved env", builds.SubmitRequest{GitURL: "https://x/y.git", GitRef: "main", BuildCommand: "y", EnvVars: map[string]string{"IOGRID_FOO": "1"}}, "env_vars"},
		{"webhook url without secret", builds.SubmitRequest{GitURL: "https://x/y.git", GitRef: "main", BuildCommand: "y", WebhookURL: "https://example.com/hook"}, "webhook"},
		{"webhook secret too short", builds.SubmitRequest{GitURL: "https://x/y.git", GitRef: "main", BuildCommand: "y", WebhookURL: "https://example.com/hook", WebhookSecret: "shrt"}, "webhook_secret"},
		{"webhook http", builds.SubmitRequest{GitURL: "https://x/y.git", GitRef: "main", BuildCommand: "y", WebhookURL: "http://example.com/hook", WebhookSecret: "0123456789abcdef0123"}, "webhook_url"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := svc.Submit(ctx, "ws-1", "u-1", "free", tc.req)
			if err == nil {
				t.Fatalf("expected validation error for %s, got nil", tc.name)
			}
			var ve *builds.ErrValidation
			if !errors.As(err, &ve) {
				t.Fatalf("expected *ErrValidation, got %T: %v", err, err)
			}
			if !strings.Contains(ve.Error(), tc.field) {
				t.Fatalf("error %q to mention field %q", ve.Error(), tc.field)
			}
		})
	}
}

func TestSubmit_HappyPath(t *testing.T) {
	t.Parallel()
	svc, fx := newTestService(t)
	ctx := context.Background()

	b, err := svc.Submit(ctx, "ws-1", "u-1", "free", validSubmit())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if b.ID == "" {
		t.Fatal("expected build id assigned")
	}
	if b.Status != builds.StatusDispatched {
		t.Fatalf("expected dispatched, got %s", b.Status)
	}
	if b.WorkspaceID != "ws-1" {
		t.Fatalf("workspace mismatch: %s", b.WorkspaceID)
	}
	if b.ProviderAttemptID == "" {
		t.Fatal("expected attempt id assigned")
	}
	subs := fx.disp.Submissions()
	if len(subs) != 1 {
		t.Fatalf("expected 1 submission, got %d", len(subs))
	}
	if subs[0].TartImage == "" || !strings.Contains(subs[0].TartImage, "macos") {
		t.Fatalf("unexpected tart image: %s", subs[0].TartImage)
	}
	if subs[0].ArtifactBucket == "" || subs[0].ArtifactPrefix == "" {
		t.Fatalf("expected artifact bucket+prefix, got %+v", subs[0])
	}
}

func TestSubmit_DefaultsXcodeVersion(t *testing.T) {
	t.Parallel()
	svc, fx := newTestService(t)
	ctx := context.Background()

	req := validSubmit()
	req.XcodeVersion = ""
	b, err := svc.Submit(ctx, "ws-1", "u-1", "free", req)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if b.XcodeVersion != "latest" {
		t.Fatalf("expected default xcode version, got %q", b.XcodeVersion)
	}
	subs := fx.disp.Submissions()
	if !strings.Contains(subs[0].TartImage, "latest") {
		t.Fatalf("expected latest tart image, got %s", subs[0].TartImage)
	}
}

func TestSubmit_DispatcherRejection(t *testing.T) {
	t.Parallel()
	svc, fx := newTestService(t)
	ctx := context.Background()

	fx.disp.FailNextSubmit(errors.New("no providers available"))
	b, err := svc.Submit(ctx, "ws-1", "u-1", "free", validSubmit())
	if err != nil {
		t.Fatalf("expected nil error (rejection persisted), got %v", err)
	}
	if b.Status != builds.StatusRejected {
		t.Fatalf("expected rejected, got %s", b.Status)
	}
	if !strings.Contains(b.StatusNote, "no providers available") {
		t.Fatalf("status note missing reason: %q", b.StatusNote)
	}
}

func TestSubmit_WebhookFires(t *testing.T) {
	t.Parallel()
	svc, fx := newTestService(t)
	ctx := context.Background()
	req := validSubmit()
	req.WebhookURL = "https://example.com/hook"
	req.WebhookSecret = "this-is-a-long-enough-secret"

	b, err := svc.Submit(ctx, "ws-1", "u-1", "pro", req)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	events := fx.webhook.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 webhook event, got %d", len(events))
	}
	if events[0].BuildID != b.ID {
		t.Fatalf("event for wrong build: %+v", events[0])
	}
	if events[0].Status != "dispatched" {
		t.Fatalf("expected dispatched event, got %s", events[0].Status)
	}
}

func TestLifecycle_RunSucceed(t *testing.T) {
	t.Parallel()
	svc, fx := newTestService(t)
	ctx := context.Background()

	b, err := svc.Submit(ctx, "ws-1", "u-1", "free", validSubmit())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	// Provider goes running.
	fx.clock.Advance(5 * time.Second)
	if _, err := svc.UpdateStatus(ctx, b.ID, builds.StatusRunning, "vm-booted", 0); err != nil {
		t.Fatalf("running update: %v", err)
	}
	// Append some log lines.
	for i := 0; i < 3; i++ {
		svc.AppendLog(b.ID, "stdout", "line")
	}
	// Provider succeeds 7m later.
	fx.clock.Advance(7 * time.Minute)
	final, err := svc.UpdateStatus(ctx, b.ID, builds.StatusSucceeded, "ok", 0)
	if err != nil {
		t.Fatalf("succeed update: %v", err)
	}
	if final.Status != builds.StatusSucceeded {
		t.Fatalf("expected succeeded, got %s", final.Status)
	}
	if final.StartedAt == nil || final.FinishedAt == nil {
		t.Fatal("expected started+finished timestamps")
	}
	if got := final.BillableMinutes(fx.clock.Now()); got != 7 {
		t.Fatalf("expected 7 billable minutes, got %d", got)
	}
	events := fx.metering.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 metering event, got %d", len(events))
	}
	if events[0].BillableMinutes != 7 {
		t.Fatalf("metering minutes wrong: %d", events[0].BillableMinutes)
	}
}

func TestLifecycle_TerminalIsSticky(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	ctx := context.Background()
	b, err := svc.Submit(ctx, "ws-1", "u-1", "free", validSubmit())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := svc.UpdateStatus(ctx, b.ID, builds.StatusSucceeded, "", 0); err != nil {
		t.Fatalf("succeed: %v", err)
	}
	if _, err := svc.UpdateStatus(ctx, b.ID, builds.StatusFailed, "", 1); !errors.Is(err, builds.ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestCancel_BlocksOnceTerminal(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	ctx := context.Background()
	b, err := svc.Submit(ctx, "ws-1", "u-1", "free", validSubmit())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := svc.UpdateStatus(ctx, b.ID, builds.StatusSucceeded, "", 0); err != nil {
		t.Fatalf("succeed: %v", err)
	}
	if _, err := svc.Cancel(ctx, "ws-1", b.ID, "test"); !errors.Is(err, workloadclient.ErrAlreadyTerminal) {
		t.Fatalf("expected ErrAlreadyTerminal, got %v", err)
	}
}

func TestArtifactUpload_RoundTrip(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	ctx := context.Background()
	b, err := svc.Submit(ctx, "ws-1", "u-1", "free", validSubmit())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	updated, obj, err := svc.UploadArtifact(ctx, b.ID, "App.ipa", "application/octet-stream", strings.NewReader("BINARY-IPA-PAYLOAD"))
	if err != nil {
		t.Fatalf("UploadArtifact: %v", err)
	}
	if len(updated.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(updated.Artifacts))
	}
	if updated.Artifacts[0].Name != "App.ipa" {
		t.Fatalf("artifact name mismatch: %s", updated.Artifacts[0].Name)
	}
	if obj.Size != int64(len("BINARY-IPA-PAYLOAD")) {
		t.Fatalf("size mismatch: %d", obj.Size)
	}
	url, err := svc.PresignArtifact(ctx, "ws-1", b.ID, "App.ipa")
	if err != nil {
		t.Fatalf("PresignArtifact: %v", err)
	}
	if !strings.HasPrefix(url, "https://") {
		t.Fatalf("expected https url, got %q", url)
	}
	if !strings.Contains(url, "X-Amz-Signature=") {
		t.Fatalf("expected pre-signed style url, got %q", url)
	}
}

func TestArtifactPresign_TenancyEnforced(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	ctx := context.Background()
	b, err := svc.Submit(ctx, "ws-1", "u-1", "free", validSubmit())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	_, _, err = svc.UploadArtifact(ctx, b.ID, "App.ipa", "", strings.NewReader("x"))
	if err != nil {
		t.Fatalf("UploadArtifact: %v", err)
	}
	if _, err := svc.PresignArtifact(ctx, "ws-OTHER", b.ID, "App.ipa"); err == nil {
		t.Fatal("expected cross-tenant presign to fail")
	}
}

func TestLogStream_SubscribeReplay(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	ctx := context.Background()
	b, err := svc.Submit(ctx, "ws-1", "u-1", "free", validSubmit())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	svc.AppendLog(b.ID, "stdout", "first")
	svc.AppendLog(b.ID, "stdout", "second")
	broker := svc.LogHub().For(b.ID)
	snap := broker.Snapshot(0)
	if len(snap) != 2 {
		t.Fatalf("expected 2 buffered lines, got %d", len(snap))
	}
	if snap[0].Text != "first" || snap[1].Text != "second" {
		t.Fatalf("snapshot order wrong: %+v", snap)
	}
	if snap[0].Seq+1 != snap[1].Seq {
		t.Fatalf("seq not monotonic: %+v", snap)
	}
	// Subscribe and append a third.
	ch := make(chan builds.LogLine, 4)
	cancel := broker.Subscribe(ch)
	defer cancel()
	svc.AppendLog(b.ID, "stderr", "third")
	select {
	case line := <-ch:
		if line.Text != "third" || line.Stream != "stderr" {
			t.Fatalf("unexpected live line %+v", line)
		}
	case <-time.After(time.Second):
		t.Fatal("expected live line within 1s")
	}
	_ = ctx
}

func TestList_FiltersAndScopes(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	ctx := context.Background()
	// Two builds in ws-1, one in ws-2.
	for i := 0; i < 2; i++ {
		if _, err := svc.Submit(ctx, "ws-1", "u-1", "free", validSubmit()); err != nil {
			t.Fatalf("Submit: %v", err)
		}
	}
	if _, err := svc.Submit(ctx, "ws-2", "u-2", "free", validSubmit()); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	out, err := svc.List(ctx, builds.ListFilter{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 builds, got %d", len(out))
	}
	for _, b := range out {
		if b.WorkspaceID != "ws-1" {
			t.Fatalf("leak from another workspace: %s", b.WorkspaceID)
		}
	}
}
