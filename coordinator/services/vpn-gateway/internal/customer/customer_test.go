package customer

import (
	"encoding/hex"
	"testing"

	"github.com/iogrid/iogrid/coordinator/services/vpn-gateway/internal/tier"
)

func mkPubKey(b byte) [32]byte {
	var pk [32]byte
	for i := range pk {
		pk[i] = b
	}
	return pk
}

func TestUpsertAndLookup(t *testing.T) {
	r := New()
	pk := mkPubKey(0x11)
	if err := r.Upsert(Customer{
		ID:         "user-1",
		PubKey:     pk,
		AssignedIP: "10.99.0.2",
		Tier:       tier.TierPlus,
		Country:    "US",
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, ok := r.ByPubKey(pk)
	if !ok {
		t.Fatal("ByPubKey miss after Upsert")
	}
	if got.ID != "user-1" || got.Tier != tier.TierPlus || got.Country != "US" {
		t.Errorf("ByPubKey: %+v", got)
	}
	got2, ok := r.ByID("user-1")
	if !ok || got2.ID != "user-1" {
		t.Error("ByID miss")
	}
	if r.Len() != 1 {
		t.Errorf("Len = %d, want 1", r.Len())
	}
}

func TestUpsertRejectsEmpty(t *testing.T) {
	r := New()
	if err := r.Upsert(Customer{PubKey: mkPubKey(1)}); err == nil {
		t.Error("Upsert with empty ID should fail")
	}
	if err := r.Upsert(Customer{ID: "u"}); err == nil {
		t.Error("Upsert with zero pubkey should fail")
	}
}

func TestPubKeyCollision(t *testing.T) {
	r := New()
	pk := mkPubKey(0x22)
	_ = r.Upsert(Customer{ID: "u1", PubKey: pk})
	if err := r.Upsert(Customer{ID: "u2", PubKey: pk}); err == nil {
		t.Error("collision (same pubkey, different user) should error")
	}
}

func TestPubKeyRotationDropsStaleIndex(t *testing.T) {
	r := New()
	pk1 := mkPubKey(0x33)
	pk2 := mkPubKey(0x44)
	_ = r.Upsert(Customer{ID: "u", PubKey: pk1, AssignedIP: "10.99.0.3"})
	_ = r.Upsert(Customer{ID: "u", PubKey: pk2, AssignedIP: "10.99.0.3"})
	if _, ok := r.ByPubKey(pk1); ok {
		t.Error("stale pubkey should be deindexed after rotation")
	}
	if _, ok := r.ByPubKey(pk2); !ok {
		t.Error("new pubkey should resolve")
	}
}

func TestRemove(t *testing.T) {
	r := New()
	pk := mkPubKey(0x55)
	_ = r.Upsert(Customer{ID: "u", PubKey: pk})
	r.Remove("u")
	if r.Len() != 0 {
		t.Error("remove should drop entry")
	}
	if _, ok := r.ByPubKey(pk); ok {
		t.Error("ByPubKey should miss after remove")
	}
}

func TestReplaceAllAtomic(t *testing.T) {
	r := New()
	_ = r.Upsert(Customer{ID: "old", PubKey: mkPubKey(0x99)})
	err := r.ReplaceAll([]Customer{
		{ID: "u1", PubKey: mkPubKey(0xa1)},
		{ID: "u2", PubKey: mkPubKey(0xa2)},
	})
	if err != nil {
		t.Fatalf("ReplaceAll: %v", err)
	}
	if r.Len() != 2 {
		t.Errorf("Len = %d, want 2", r.Len())
	}
	if _, ok := r.ByID("old"); ok {
		t.Error("old entry should be gone after ReplaceAll")
	}
	if _, ok := r.ByID("u1"); !ok {
		t.Error("u1 should resolve")
	}
}

func TestReplaceAllDuplicate(t *testing.T) {
	r := New()
	pk := mkPubKey(0xbb)
	err := r.ReplaceAll([]Customer{
		{ID: "u1", PubKey: pk},
		{ID: "u2", PubKey: pk},
	})
	if err == nil {
		t.Error("ReplaceAll with duplicate pubkey should error")
	}
}

func TestDecodePubKey(t *testing.T) {
	// Base64 form (standard wg pubkey output).
	pk := mkPubKey(0x07)
	b64 := stdB64.EncodeToString(pk[:])
	got, err := DecodePubKey(b64)
	if err != nil {
		t.Fatalf("DecodePubKey b64: %v", err)
	}
	if got != pk {
		t.Errorf("DecodePubKey b64 round-trip mismatch")
	}
	// Hex form.
	got2, err := DecodePubKey(hex.EncodeToString(pk[:]))
	if err != nil {
		t.Fatalf("DecodePubKey hex: %v", err)
	}
	if got2 != pk {
		t.Errorf("DecodePubKey hex round-trip mismatch")
	}
	// Bad input.
	if _, err := DecodePubKey("not-a-key"); err == nil {
		t.Error("DecodePubKey should error on garbage")
	}
}
