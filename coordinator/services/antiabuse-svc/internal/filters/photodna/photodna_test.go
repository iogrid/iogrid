package photodna

import (
	"context"
	"encoding/hex"
	"testing"
)

func TestStubMode_AlwaysAllow(t *testing.T) {
	b := New(Options{}) // no APIKey → stub
	if b.Enabled() {
		t.Error("backend should not be Enabled() without APIKey")
	}
	r := b.CheckURL(context.Background(), "https://example.com/img.jpg")
	if r.Match {
		t.Errorf("stub mode must never match: %+v", r)
	}
}

func TestEnabled_WithKey(t *testing.T) {
	b := New(Options{APIKey: "test-key"})
	if !b.Enabled() {
		t.Error("backend should be Enabled() with APIKey")
	}
}

func TestInjectMatch_ProducesBlock(t *testing.T) {
	b := New(Options{APIKey: "test-key"})
	url := "https://example.com/csam.jpg"
	hash := hex.EncodeToString([]byte(url))
	b.InjectMatch(hash)
	r := b.CheckURL(context.Background(), url)
	if !r.Match {
		t.Fatalf("expected match after InjectMatch: %+v", r)
	}
	if r.Reason != "csam_hash_match" {
		t.Errorf("Reason = %q, want csam_hash_match", r.Reason)
	}
}

func TestCheckDomain_AlwaysAllow(t *testing.T) {
	b := New(Options{APIKey: "test-key"})
	if r := b.CheckDomain(context.Background(), "anything"); r.Match {
		t.Errorf("CheckDomain must not match (PhotoDNA is per-image)")
	}
}
