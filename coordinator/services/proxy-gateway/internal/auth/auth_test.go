package auth

import (
	"context"
	"errors"
	"testing"
)

func TestStatic_Validate(t *testing.T) {
	v := NewStatic(map[string]Customer{
		"sk_live_abc": {WorkspaceID: "ws-1", CustomerID: "cust-1", Tier: "starter"},
	})
	c, err := v.Validate(context.Background(), "sk_live_abc")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if c.WorkspaceID != "ws-1" {
		t.Fatalf("ws = %q", c.WorkspaceID)
	}
	if c.ResolvedAt.IsZero() {
		t.Fatalf("ResolvedAt should be set")
	}
}

func TestStatic_EmptyKeyRejected(t *testing.T) {
	v := NewStatic(nil)
	if _, err := v.Validate(context.Background(), ""); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey, got %v", err)
	}
}

func TestStatic_UnknownKeyRejected(t *testing.T) {
	v := NewStatic(map[string]Customer{"ok": {WorkspaceID: "ws"}})
	if _, err := v.Validate(context.Background(), "nope"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey, got %v", err)
	}
}

func TestStatic_SetError(t *testing.T) {
	v := NewStatic(map[string]Customer{"ok": {WorkspaceID: "ws"}})
	v.SetError(ErrSuspended)
	if _, err := v.Validate(context.Background(), "ok"); !errors.Is(err, ErrSuspended) {
		t.Fatalf("expected ErrSuspended, got %v", err)
	}
}

func TestSplitUserPass(t *testing.T) {
	cases := []struct {
		name           string
		user, pass     string
		wantHandle     string
		wantKey        string
		wantOK         bool
	}{
		{"both", "ws-1", "sk_live_abc", "ws-1", "sk_live_abc", true},
		{"only user", "sk_live_abc", "", "", "sk_live_abc", true},
		{"empty", "", "", "", "", false},
		{"only pass", "", "sk_live_abc", "", "sk_live_abc", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, k, ok := SplitUserPass(c.user, c.pass)
			if ok != c.wantOK || h != c.wantHandle || k != c.wantKey {
				t.Fatalf("SplitUserPass(%q,%q) = (%q,%q,%v); want (%q,%q,%v)",
					c.user, c.pass, h, k, ok, c.wantHandle, c.wantKey, c.wantOK)
			}
		})
	}
}
