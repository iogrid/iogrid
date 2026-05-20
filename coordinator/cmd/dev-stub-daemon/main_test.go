package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	workloadsv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/workloads/v1"
)

func TestParseEligibleTypes(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	for _, tc := range []struct {
		name string
		in   string
		want []commonv1.WorkloadType
	}{
		{
			name: "bare bandwidth",
			in:   "BANDWIDTH",
			want: []commonv1.WorkloadType{commonv1.WorkloadType_WORKLOAD_TYPE_BANDWIDTH},
		},
		{
			name: "fully-qualified",
			in:   "WORKLOAD_TYPE_DOCKER",
			want: []commonv1.WorkloadType{commonv1.WorkloadType_WORKLOAD_TYPE_DOCKER},
		},
		{
			name: "mixed with spaces and unknown",
			in:   " BANDWIDTH , social_intel , DOCKER ",
			want: []commonv1.WorkloadType{
				commonv1.WorkloadType_WORKLOAD_TYPE_BANDWIDTH,
				commonv1.WorkloadType_WORKLOAD_TYPE_DOCKER,
			},
		},
		{
			name: "empty defaults to bandwidth",
			in:   "",
			want: []commonv1.WorkloadType{commonv1.WorkloadType_WORKLOAD_TYPE_BANDWIDTH},
		},
		{
			name: "only unknowns defaults to bandwidth",
			in:   "FOO,BAR",
			want: []commonv1.WorkloadType{commonv1.WorkloadType_WORKLOAD_TYPE_BANDWIDTH},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := parseEligibleTypes(tc.in, log)
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch: got %v want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("index %d: got %v want %v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestReadProviderIDFromConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")

	// Missing file → error path.
	if _, err := readProviderIDFromConfig(cfg); err == nil {
		t.Fatal("expected error for missing config.toml")
	}

	// Quoted value.
	if err := os.WriteFile(cfg, []byte(`# header
coordinator_url = "https://api.iogrid.org:443"
provider_id = "11111111-2222-3333-4444-555555555555"
ui_listen = "127.0.0.1:7777"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readProviderIDFromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "11111111-2222-3333-4444-555555555555"; got != want {
		t.Errorf("got %q want %q", got, want)
	}

	// Missing key → empty string, no error.
	if err := os.WriteFile(cfg, []byte("coordinator_url = \"x\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = readProviderIDFromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestEnvHelpers(t *testing.T) {
	t.Setenv("UNIT_TEST_KEY", "")
	if got := envOrDefault("UNIT_TEST_KEY", "fallback"); got != "fallback" {
		t.Errorf("envOrDefault empty: got %q want fallback", got)
	}
	t.Setenv("UNIT_TEST_KEY", "actual")
	if got := envOrDefault("UNIT_TEST_KEY", "fallback"); got != "actual" {
		t.Errorf("envOrDefault set: got %q want actual", got)
	}

	for _, v := range []string{"1", "true", "yes", "on", "TRUE", "Yes"} {
		t.Setenv("UNIT_TEST_BOOL", v)
		if !boolFromEnv("UNIT_TEST_BOOL") {
			t.Errorf("boolFromEnv(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"", "0", "false", "no", "off", "anything"} {
		t.Setenv("UNIT_TEST_BOOL", v)
		if boolFromEnv("UNIT_TEST_BOOL") {
			t.Errorf("boolFromEnv(%q) = true, want false", v)
		}
	}
}

// TestTunnels_EndToEnd_RoundTrip verifies the dev-stub's real
// TCP-over-DispatchFrame tunneling (iogrid#279):
//   - on TunnelOpen the stub dials target_host_port,
//   - TunnelData frames flow to the dialed socket,
//   - destination-side bytes come back as TunnelData frames,
//   - destination EOF triggers a TunnelClose with empty error.
func TestTunnels_EndToEnd_RoundTrip(t *testing.T) {
	// Spin up a destination echo server. Echoes "ECHO:" + input then
	// closes so the stub sees EOF and emits a clean TunnelClose.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 64)
		n, rerr := c.Read(buf)
		if rerr != nil && rerr != io.EOF {
			return
		}
		_, _ = c.Write([]byte("ECHO:"))
		_, _ = c.Write(buf[:n])
	}()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	var mu sync.Mutex
	var frames []*workloadsv1.DispatchFrame
	send := func(f *workloadsv1.DispatchFrame) error {
		mu.Lock()
		defer mu.Unlock()
		frames = append(frames, f)
		return nil
	}

	tun := newTunnels(log, send)
	defer tun.closeAll()

	const attemptID = "00000000-0000-0000-0000-000000000001"
	tun.open(context.Background(), attemptID, ln.Addr().String())
	tun.feed(attemptID, []byte("hello"))

	// Wait up to 2s for the echo + EOF cycle to materialise as frames.
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		count := len(frames)
		mu.Unlock()
		if count >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for frames; got %d", count)
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	// First emitted frame must be TunnelData carrying "ECHO:hello".
	var sawData, sawClose bool
	var combined []byte
	for _, f := range frames {
		if td := f.GetTunnelData(); td != nil {
			combined = append(combined, td.GetPayload()...)
			sawData = true
			continue
		}
		if tc := f.GetTunnelClose(); tc != nil {
			sawClose = true
			if tc.GetError() != "" {
				t.Errorf("clean EOF should produce empty reason; got %q", tc.GetError())
			}
		}
	}
	if !sawData {
		t.Fatalf("no TunnelData frames emitted; frames=%v", frames)
	}
	if !sawClose {
		t.Fatalf("no TunnelClose frame emitted; frames=%v", frames)
	}
	if string(combined) != "ECHO:hello" {
		t.Errorf("payload mismatch: got %q want %q", string(combined), "ECHO:hello")
	}
}

// TestTunnels_DialFailureSendsClose verifies that when the
// target_host_port is unreachable, the stub sends a TunnelClose with
// the dial error in `error` rather than leaving the attempt half-open.
func TestTunnels_DialFailureSendsClose(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	var mu sync.Mutex
	var frames []*workloadsv1.DispatchFrame
	send := func(f *workloadsv1.DispatchFrame) error {
		mu.Lock()
		defer mu.Unlock()
		frames = append(frames, f)
		return nil
	}

	tun := newTunnels(log, send)
	defer tun.closeAll()

	// 127.0.0.1:1 — almost certainly unbound.
	tun.open(context.Background(), "att-fail", "127.0.0.1:1")

	mu.Lock()
	defer mu.Unlock()
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame; got %d", len(frames))
	}
	tc := frames[0].GetTunnelClose()
	if tc == nil {
		t.Fatalf("expected TunnelClose; got %v", frames[0])
	}
	if tc.GetError() == "" {
		t.Fatalf("expected non-empty error on dial failure")
	}
	if tc.GetAttemptId().GetValue() != "att-fail" {
		t.Errorf("attempt id mismatch: %q", tc.GetAttemptId().GetValue())
	}
}

// TestTunnels_EmptyTarget rejects a TunnelOpen with no target.
func TestTunnels_EmptyTarget(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	var mu sync.Mutex
	var frames []*workloadsv1.DispatchFrame
	send := func(f *workloadsv1.DispatchFrame) error {
		mu.Lock()
		defer mu.Unlock()
		frames = append(frames, f)
		return nil
	}
	tun := newTunnels(log, send)
	defer tun.closeAll()

	tun.open(context.Background(), "att-empty", "")

	mu.Lock()
	defer mu.Unlock()
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame; got %d", len(frames))
	}
	if frames[0].GetTunnelClose() == nil {
		t.Fatalf("expected TunnelClose for empty target")
	}
}
