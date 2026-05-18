package google

import (
	"crypto/sha256"
	"hash"
)

// newSHA256 indirects through this helper so a future test could swap in
// a fault-injecting hash. Compile-time only — zero runtime overhead.
func newSHA256() hash.Hash { return sha256.New() }
