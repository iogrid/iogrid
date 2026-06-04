package server

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	billingv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1"
)

// RegisterConsumerAccount contract tests (#690 D1). The Postgres-backed
// idempotency + race path is integration territory; these pin the
// format gate.

func TestRegisterConsumerAccount_RejectsBadFormats(t *testing.T) {
	h := &ApiKeyHandler{} // store untouched on the format path
	for _, bad := range []string{"", "123", "abcdefghijklmnop", "1234 5678 9012 3456", "12345678901234567", "123456789012345"} {
		_, err := h.RegisterConsumerAccount(context.Background(), connect.NewRequest(&billingv1.RegisterConsumerAccountRequest{
			AccountNumber: bad,
		}))
		if got := connect.CodeOf(err); got != connect.CodeInvalidArgument {
			t.Fatalf("account_number=%q: code = %v, want InvalidArgument", bad, got)
		}
	}
}

func TestRegisterConsumerAccount_DeterministicWorkspace(t *testing.T) {
	// The synthetic workspace id must be stable across calls/processes:
	// uuid.NewSHA1(namespace, number) — pin the derivation against an
	// accidental namespace change (which would orphan every account).
	a := deriveConsumerWorkspaceID("1234567890123456")
	b := deriveConsumerWorkspaceID("1234567890123456")
	c := deriveConsumerWorkspaceID("6543210987654321")
	if a != b {
		t.Fatalf("derivation not deterministic: %s vs %s", a, b)
	}
	if a == c {
		t.Fatalf("distinct numbers collided")
	}
	if a.String() != "0da7d6fc-a82e-588b-98a0-fcc73b46fcfb" {
		t.Fatalf("namespace drifted — derived %s for the pinned vector (update ONLY with a data migration)", a)
	}
}
