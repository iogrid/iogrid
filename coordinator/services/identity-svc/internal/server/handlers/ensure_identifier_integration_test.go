//go:build integration
// +build integration

// Integration tests for EnsureIdentifier (#685) — the idempotent
// registration RPC the web's NextAuth signIn event calls via gateway-bff.
// Exercises the real Postgres-backed branches the unit tests can't reach:
//
//   - First call creates the row (created=true) — the "healing" path for
//     accounts that signed in via NextAuth before the fix existed.
//   - Repeat call is a no-op (created=false, same identifier id).
//   - Email matching is case-insensitive (magic-link).
//   - OAuth kinds match on subject, not email.
//
// Run via:  go test -tags=integration ./internal/server/handlers/...
// (Reuses pgFixture from workspace_integration_test.go.)

package handlers

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	identityv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/identity/v1"
	authmw "github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/server/middleware"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/store"
)

func TestEnsureIdentifier_Integration(t *testing.T) {
	pool, cleanup := pgFixture(t)
	defer cleanup()
	ctx := context.Background()
	st := store.New(pool)
	h := NewIdentityHandler(st)

	// A user that exists with ZERO identifier rows — exactly the #685
	// shape (created lazily by workspace bootstrap, never registered).
	u := &store.User{PrimaryEmail: "bare@openova.io", DisplayName: "bare"}
	if err := st.CreateUser(ctx, nil, u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	callerCtx := authmw.WithAuthedUser(ctx, u.ID)

	// 1. First ensure creates the row — the healing path.
	resp, err := h.EnsureIdentifier(callerCtx, connect.NewRequest(&identityv1.EnsureIdentifierRequest{
		UserId:        &commonv1.UUID{Value: u.ID.String()},
		Kind:          identityv1.IdentifierKind_IDENTIFIER_KIND_MAGIC_LINK,
		VerifiedEmail: "Bare@OpenOva.io", // mixed case on purpose
	}))
	if err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if !resp.Msg.GetCreated() {
		t.Fatalf("first ensure: expected created=true")
	}
	firstID := resp.Msg.GetIdentifier().GetId().GetValue()
	if firstID == "" {
		t.Fatalf("first ensure: empty identifier id")
	}
	if got := resp.Msg.GetIdentifier().GetVerifiedEmail(); got != "bare@openova.io" {
		t.Fatalf("first ensure: email not normalised lowercase, got %q", got)
	}

	// 2. Repeat with different casing — idempotent, same row.
	resp, err = h.EnsureIdentifier(callerCtx, connect.NewRequest(&identityv1.EnsureIdentifierRequest{
		UserId:        &commonv1.UUID{Value: u.ID.String()},
		Kind:          identityv1.IdentifierKind_IDENTIFIER_KIND_MAGIC_LINK,
		VerifiedEmail: "BARE@openova.IO",
	}))
	if err != nil {
		t.Fatalf("repeat ensure: %v", err)
	}
	if resp.Msg.GetCreated() {
		t.Fatalf("repeat ensure: expected created=false (idempotent)")
	}
	if got := resp.Msg.GetIdentifier().GetId().GetValue(); got != firstID {
		t.Fatalf("repeat ensure: id changed %q -> %q (duplicate row?)", firstID, got)
	}

	// 3. Exactly one row in the store.
	rows, err := st.ListIdentifiersForUserByKind(ctx, nil, u.ID, store.KindMagicLink)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected exactly 1 magic-link row, got %d", len(rows))
	}
	if !rows[0].Verified {
		t.Fatalf("ensured identifier must be verified (inbox control proven)")
	}

	// 4. OAuth kind matches on subject: same email, different subject
	//    creates; same subject is idempotent.
	g1, err := h.EnsureIdentifier(callerCtx, connect.NewRequest(&identityv1.EnsureIdentifierRequest{
		UserId:        &commonv1.UUID{Value: u.ID.String()},
		Kind:          identityv1.IdentifierKind_IDENTIFIER_KIND_GOOGLE,
		VerifiedEmail: "bare@openova.io",
		Subject:       "google-sub-123",
	}))
	if err != nil {
		t.Fatalf("google ensure: %v", err)
	}
	if !g1.Msg.GetCreated() {
		t.Fatalf("google ensure: expected created=true (different kind)")
	}
	g2, err := h.EnsureIdentifier(callerCtx, connect.NewRequest(&identityv1.EnsureIdentifierRequest{
		UserId:        &commonv1.UUID{Value: u.ID.String()},
		Kind:          identityv1.IdentifierKind_IDENTIFIER_KIND_GOOGLE,
		VerifiedEmail: "changed-email@openova.io", // email differs; subject is the key
		Subject:       "google-sub-123",
	}))
	if err != nil {
		t.Fatalf("google repeat: %v", err)
	}
	if g2.Msg.GetCreated() {
		t.Fatalf("google repeat: expected created=false (subject match)")
	}
	if g2.Msg.GetIdentifier().GetId().GetValue() != g1.Msg.GetIdentifier().GetId().GetValue() {
		t.Fatalf("google repeat: id changed (subject match broken)")
	}

	// 5. Cross-user remains denied with a real store too.
	other := &store.User{PrimaryEmail: "other@openova.io", DisplayName: "other"}
	if err := st.CreateUser(ctx, nil, other); err != nil {
		t.Fatalf("seed other: %v", err)
	}
	_, err = h.EnsureIdentifier(callerCtx, connect.NewRequest(&identityv1.EnsureIdentifierRequest{
		UserId:        &commonv1.UUID{Value: other.ID.String()},
		Kind:          identityv1.IdentifierKind_IDENTIFIER_KIND_MAGIC_LINK,
		VerifiedEmail: "other@openova.io",
	}))
	if got := connect.CodeOf(err); got != connect.CodePermissionDenied {
		t.Fatalf("cross-user: expected CodePermissionDenied, got %v", got)
	}
	if err != nil && !strings.Contains(err.Error(), "does not match caller") {
		t.Fatalf("cross-user: unexpected message %v", err)
	}
}
