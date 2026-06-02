package payment

import (
	"crypto/sha256"
	"errors"

	"filippo.io/edwards25519"
)

// isOnCurve returns true iff the 32-byte input decodes as a valid
// edwards25519 group element. PDAs must be OFF the curve so that no one
// can have the private key for them — this matches solana-program's
// is_on_curve check used inside find_program_address.
func isOnCurve(b []byte) bool {
	if len(b) != 32 {
		return false
	}
	var p edwards25519.Point
	_, err := p.SetBytes(b)
	return err == nil
}

// findProgramAddress implements Solana's PDA derivation:
//
//	for bump := 255; bump >= 0; bump-- {
//	    h := sha256(seeds || []byte{bump} || program || "ProgramDerivedAddress")
//	    if h is OFF the ed25519 curve: return h, bump
//	}
//
// We don't import ed25519 curve math here because Go's stdlib doesn't expose
// the necessary primitives — we use the Solana-canonical test: "the hash is
// off-curve iff the 32-byte point is not a valid ed25519 public key
// y-coordinate", which is what `is_on_curve` tests in solana-program. Since
// we use this only for the Associated Token Account derivation (where the
// PDA is well-known to be off-curve for any valid wallet), we can use a
// lightweight curve check based on the byte structure: in practice for ATAs
// the canonical bump (255) is used overwhelmingly, but to be correct we
// iterate from 255 downward and trust solana-program's behaviour that any
// PDA must be off-curve.
//
// Because verifying off-curve in pure-Go without a 3rd-party SDK is
// involved, we use a pragmatic approach: try bump 255 first (overwhelming
// common case), then iterate. We perform the off-curve check via
// edwards25519.SetBytes from x/crypto/internal/edwards25519 — but that's
// internal. So instead we use filippo.io/edwards25519 which is already a
// transitive dep through golang.org/x/crypto.
func findProgramAddress(seeds [][]byte, programID []byte) ([]byte, uint8, error) {
	if len(seeds) > 16 {
		return nil, 0, errors.New("findProgramAddress: too many seeds (max 16)")
	}
	for _, s := range seeds {
		if len(s) > 32 {
			return nil, 0, errors.New("findProgramAddress: seed too long (max 32 bytes)")
		}
	}
	for bumpInt := 255; bumpInt >= 0; bumpInt-- {
		bump := uint8(bumpInt)
		h := sha256.New()
		for _, s := range seeds {
			h.Write(s)
		}
		h.Write([]byte{bump})
		h.Write(programID)
		h.Write([]byte("ProgramDerivedAddress"))
		sum := h.Sum(nil)
		if !isOnCurve(sum) {
			return sum, bump, nil
		}
	}
	return nil, 0, errors.New("findProgramAddress: no off-curve bump (unreachable for valid seeds)")
}
