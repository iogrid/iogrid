package store

import (
	"encoding/json"
	"testing"

	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/builds"
)

// Regression for the #709/#721 in-memory-green/Postgres-broken class: the
// Postgres store serializes builds via the explicit persistedBuild projection,
// so any Build field not copied there is silently dropped on persist. Pin that
// CustomerWallet (added in #719 for $GRID settlement) survives the round-trip.
func TestPersistedBuild_CustomerWalletRoundTrips(t *testing.T) {
	in := &builds.Build{
		ID: "b1", WorkspaceID: "ws1", SubmittedByUserID: "u1",
		CustomerWallet: "Wallet111111111111111111111111111111111111",
		Status:         builds.StatusQueued,
	}
	raw, err := json.Marshal(toPersisted(in))
	if err != nil {
		t.Fatal(err)
	}
	var p persistedBuild
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	out := p.toBuild()
	if out.CustomerWallet != in.CustomerWallet {
		t.Fatalf("CustomerWallet dropped on persist: got %q want %q (Postgres would drop the $GRID settle wallet)",
			out.CustomerWallet, in.CustomerWallet)
	}
}
