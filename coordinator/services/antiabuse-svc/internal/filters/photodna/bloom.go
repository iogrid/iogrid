package photodna

import (
	"encoding/binary"
	"hash/fnv"
	"math"
	"sync"
)

// Bloom is a tiny fixed-FPR bloom filter used to short-circuit
// definitely-not-a-match hash lookups before we round-trip to NCMEC.
//
// We pre-size the filter from the expected number of entries (NCMEC's
// published set is ~1M hashes today; we pick a 0.1% false-positive
// rate which costs ~14 bits per entry — ~1.8MB for 1M hashes). The
// real partnership ships an SDK that hands us the canonical hash list
// directly; this implementation is intentionally hash-list agnostic so
// it works against any future format.
//
// The filter is read-only after construction; replacement is done by
// SetBloom() on the Backend (atomic pointer swap under a single mutex).
type Bloom struct {
	m    uint64 // bit count
	k    uint64 // hash count
	bits []uint64

	mu sync.RWMutex
}

// NewBloom builds an empty filter sized for the given expected entry
// count and false-positive rate. fpRate must be in (0, 1); n must be > 0.
// Defaults: n=1, fpRate=0.001 (any silly inputs are coerced to safe
// values so the caller can't accidentally make a zero-sized filter).
func NewBloom(n int, fpRate float64) *Bloom {
	if n <= 0 {
		n = 1
	}
	if fpRate <= 0 || fpRate >= 1 {
		fpRate = 0.001
	}
	// m = -n * ln(p) / (ln 2)^2
	m := uint64(math.Ceil(-float64(n) * math.Log(fpRate) / (math.Ln2 * math.Ln2)))
	// k = (m/n) * ln 2
	k := uint64(math.Max(1, math.Round(float64(m)/float64(n)*math.Ln2)))
	// Round m up to a 64-bit boundary.
	words := (m + 63) / 64
	if words == 0 {
		words = 1
	}
	return &Bloom{
		m:    words * 64,
		k:    k,
		bits: make([]uint64, words),
	}
}

// Add inserts a hex-encoded hash string into the filter.
func (b *Bloom) Add(hashHex string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := uint64(0); i < b.k; i++ {
		bit := b.hashAt(hashHex, i) % b.m
		b.bits[bit/64] |= 1 << (bit % 64)
	}
}

// MayContain reports whether the filter possibly contains the hash.
// False positives are bounded by the construction FPR; false negatives
// are impossible (lemma the orchestrator depends on).
func (b *Bloom) MayContain(hashHex string) bool {
	if b == nil {
		return true // be safe — without a bloom, always consult the API
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for i := uint64(0); i < b.k; i++ {
		bit := b.hashAt(hashHex, i) % b.m
		if b.bits[bit/64]&(1<<(bit%64)) == 0 {
			return false
		}
	}
	return true
}

// Size returns the bit count.
func (b *Bloom) Size() uint64 {
	if b == nil {
		return 0
	}
	return b.m
}

// Hashes returns the hash-function count k.
func (b *Bloom) Hashes() uint64 {
	if b == nil {
		return 0
	}
	return b.k
}

// hashAt produces the i-th independent hash via double-hashing (Kirsch
// & Mitzenmacher 2006): h_i = h1 + i * h2. Two independent FNV-1a
// hashes (one over the raw string, one over its reverse) give us cheap
// 64-bit primitives without bringing in a crypto dependency.
func (b *Bloom) hashAt(s string, i uint64) uint64 {
	h1 := fnv.New64a()
	_, _ = h1.Write([]byte(s))
	v1 := h1.Sum64()

	h2 := fnv.New64a()
	// Reverse the string for an independent-ish second hash.
	rev := make([]byte, len(s))
	for j := range s {
		rev[j] = s[len(s)-1-j]
	}
	_, _ = h2.Write(rev)
	// Mix in i so the i-th hash differs from the 0th when i==0 (rare
	// corner-case: when v2 happens to equal 0, every hash collapses).
	var ib [8]byte
	binary.LittleEndian.PutUint64(ib[:], i)
	_, _ = h2.Write(ib[:])
	v2 := h2.Sum64()

	return v1 + i*v2
}
