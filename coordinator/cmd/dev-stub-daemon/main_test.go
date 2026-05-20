package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
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
