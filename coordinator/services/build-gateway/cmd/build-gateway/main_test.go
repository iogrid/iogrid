package main

import (
	"context"
	"log/slog"
	"testing"

	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/auth"
)

// TestBuildValidator_SingleKey keeps the original single-key form working: a
// deployment that only sets BUILD_GATEWAY_STATIC_API_KEY (+ _WORKSPACE/_USER/
// _PLAN) must validate that one key unchanged.
func TestBuildValidator_SingleKey(t *testing.T) {
	t.Setenv("BUILD_GATEWAY_STATIC_API_KEY", "single-key")
	t.Setenv("BUILD_GATEWAY_STATIC_WORKSPACE", "ws-1")
	t.Setenv("BUILD_GATEWAY_STATIC_USER", "user-1")
	t.Setenv("BUILD_GATEWAY_STATIC_PLAN", "enterprise")
	t.Setenv("BUILD_GATEWAY_STATIC_API_KEYS", "")

	v := buildValidator(slog.Default())

	id, err := v.Validate(context.Background(), "single-key")
	if err != nil {
		t.Fatalf("single key must validate: %v", err)
	}
	if id.WorkspaceID != "ws-1" || id.UserID != "user-1" || id.Plan != "enterprise" {
		t.Fatalf("identity mismatch: %+v", id)
	}
	if _, err := v.Validate(context.Background(), "nope"); err != auth.ErrInvalidKey {
		t.Fatalf("unknown key must 401, got %v", err)
	}
}

// TestBuildValidator_MultiKeyCoexists is the #806 dog-food seam: a dedicated
// internal key provisioned via BUILD_GATEWAY_STATIC_API_KEYS must validate
// alongside — not evict — the original single key.
func TestBuildValidator_MultiKeyCoexists(t *testing.T) {
	t.Setenv("BUILD_GATEWAY_STATIC_API_KEY", "ping-key")
	t.Setenv("BUILD_GATEWAY_STATIC_WORKSPACE", "ping-ws")
	t.Setenv("BUILD_GATEWAY_STATIC_USER", "ping-user")
	t.Setenv("BUILD_GATEWAY_STATIC_PLAN", "enterprise")
	// Two extra entries: one fully specified (newline-separated), one with
	// only a workspace (user/plan default).
	t.Setenv("BUILD_GATEWAY_STATIC_API_KEYS",
		"dogfood-key=iogrid-ws:iogrid-user:enterprise\nbare-key=bare-ws")

	v := buildValidator(slog.Default())

	// Original key still valid.
	if id, err := v.Validate(context.Background(), "ping-key"); err != nil || id.WorkspaceID != "ping-ws" {
		t.Fatalf("original key must survive: id=%+v err=%v", id, err)
	}
	// Dedicated dog-food key valid with its own workspace/user/plan.
	id, err := v.Validate(context.Background(), "dogfood-key")
	if err != nil {
		t.Fatalf("dogfood key must validate: %v", err)
	}
	if id.WorkspaceID != "iogrid-ws" || id.UserID != "iogrid-user" || id.Plan != "enterprise" {
		t.Fatalf("dogfood identity mismatch: %+v", id)
	}
	// Bare entry: workspace only, defaults applied.
	bare, err := v.Validate(context.Background(), "bare-key")
	if err != nil {
		t.Fatalf("bare key must validate: %v", err)
	}
	if bare.WorkspaceID != "bare-ws" || bare.UserID != "" || bare.Plan != "free" {
		t.Fatalf("bare identity mismatch: %+v", bare)
	}
}

// TestBuildValidator_MultiKeyOnly proves the extra-keys env works even when the
// singular BUILD_GATEWAY_STATIC_API_KEY is unset (semicolon-separated form).
func TestBuildValidator_MultiKeyOnly(t *testing.T) {
	t.Setenv("BUILD_GATEWAY_STATIC_API_KEY", "")
	t.Setenv("BUILD_GATEWAY_STATIC_API_KEYS", "k1=ws1:u1:pro ; k2=ws2")

	v := buildValidator(slog.Default())

	if id, err := v.Validate(context.Background(), "k1"); err != nil || id.WorkspaceID != "ws1" || id.Plan != "pro" {
		t.Fatalf("k1 mismatch: id=%+v err=%v", id, err)
	}
	if id, err := v.Validate(context.Background(), "k2"); err != nil || id.WorkspaceID != "ws2" || id.Plan != "free" {
		t.Fatalf("k2 mismatch: id=%+v err=%v", id, err)
	}
}
