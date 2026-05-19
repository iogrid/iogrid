package photodna

import (
	"fmt"
	"testing"
)

func TestBloom_AddAndMayContain(t *testing.T) {
	bf := NewBloom(10_000, 0.001)
	added := []string{"aaaa", "bbbb", "deadbeef", "0123456789abcdef"}
	for _, s := range added {
		bf.Add(s)
	}
	for _, s := range added {
		if !bf.MayContain(s) {
			t.Errorf("MayContain(%q) = false, want true (Add was called)", s)
		}
	}
}

func TestBloom_NeverFalseNegative(t *testing.T) {
	bf := NewBloom(1_000, 0.001)
	for i := 0; i < 1_000; i++ {
		s := fmt.Sprintf("hash-%d", i)
		bf.Add(s)
		if !bf.MayContain(s) {
			t.Fatalf("false negative on %q (iter %d)", s, i)
		}
	}
}

func TestBloom_FPRWithinBudget(t *testing.T) {
	const n = 5_000
	bf := NewBloom(n, 0.01)
	for i := 0; i < n; i++ {
		bf.Add(fmt.Sprintf("in-%d", i))
	}
	var fp int
	const probes = 5_000
	for i := 0; i < probes; i++ {
		if bf.MayContain(fmt.Sprintf("out-%d", i)) {
			fp++
		}
	}
	// We sized for ~1% FPR. Give 3x headroom for hash-quality variance.
	if rate := float64(fp) / float64(probes); rate > 0.03 {
		t.Errorf("false-positive rate %.3f exceeds budget", rate)
	}
}

func TestBloom_NilSafe(t *testing.T) {
	var bf *Bloom
	if !bf.MayContain("x") {
		t.Errorf("nil Bloom must MayContain → true (safe-by-default)")
	}
	bf.Add("x") // must not panic
	if got := bf.Size(); got != 0 {
		t.Errorf("nil Bloom Size = %d, want 0", got)
	}
	if got := bf.Hashes(); got != 0 {
		t.Errorf("nil Bloom Hashes = %d, want 0", got)
	}
}

func TestBloom_DegenerateInputs(t *testing.T) {
	// n=0, fp out of range — should coerce to safe defaults.
	bf := NewBloom(0, 0)
	if bf.Size() == 0 {
		t.Errorf("Size must be > 0 even for degenerate input")
	}
	bf.Add("x")
	if !bf.MayContain("x") {
		t.Errorf("MayContain after Add must be true")
	}
}
