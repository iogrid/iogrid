// Package auth — Apple ID token (JWT) validator.
//
// Apple's "Sign in with Apple" flow returns a JWT identity token signed
// by Apple's OIDC keys. The token's payload carries:
//
//   - `iss`: must equal "https://appleid.apple.com"
//   - `aud`: must equal the bundle id we sign in for (io.iogrid.app)
//   - `sub`: the canonical, stable user id (an opaque Apple-internal
//     string; we hash it with a per-deployment salt before storing)
//   - `nonce`: present iff the client supplied a nonce on the
//     authentication request — we compare to a server-known nonce
//   - `exp`: standard JWT expiry
//   - `email`: may be a real address, an Apple private-relay address,
//     or absent entirely — NEVER use it as a stable identifier
//
// Apple signs tokens with RS256 against the keys published at
// https://appleid.apple.com/auth/keys. We resolve the signing key by
// `kid` header against a 24h-cached JWKS document (jwks_cache.go).
package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AppleIssuer is the constant `iss` claim Apple sets on every
// real-world Sign-in-with-Apple identity token.
const AppleIssuer = "https://appleid.apple.com"

// AppleAudience is the iOS bundle id Apple signs the audience claim
// with. Tokens minted for any other app MUST fail validation here so a
// stolen token from a sibling app cannot impersonate an iogrid user.
const AppleAudience = "io.iogrid.app"

// AppleClaims is the subset of the Apple ID token payload we care about.
// The repo's JWT library exposes claims via a struct that implements
// jwt.Claims; we declare our own so we can read both standard and
// Apple-specific fields uniformly.
type AppleClaims struct {
	jwt.RegisteredClaims
	Nonce         string `json:"nonce,omitempty"`
	NonceHash     string `json:"nonce_supported,omitempty"`
	Email         string `json:"email,omitempty"`
	EmailVerified any    `json:"email_verified,omitempty"` // string or bool depending on Apple's mood
	IsPrivateMail any    `json:"is_private_email,omitempty"`
}

// AppleValidator is configured per process. The JWKSCache + clock are
// the only collaborators; the issuer + audience are constants pinned at
// construction time so tests can swap them for the fake JWKS server.
type AppleValidator struct {
	Issuer   string
	Audience string
	JWKS     *JWKSCache
	Now      func() time.Time // injected clock; tests use a fixed time
}

// NewAppleValidator builds a validator with sensible defaults. Pass nil
// for jwks to default to the canonical Apple URL with a 24h TTL.
func NewAppleValidator(jwks *JWKSCache) *AppleValidator {
	if jwks == nil {
		jwks = NewJWKSCache(AppleJWKSURL, DefaultJWKSCacheTTL, nil)
	}
	return &AppleValidator{
		Issuer:   AppleIssuer,
		Audience: AppleAudience,
		JWKS:     jwks,
		Now:      time.Now,
	}
}

// ErrAppleTokenInvalid is the sentinel returned by Validate on every
// failure mode; callers use errors.Is to detect.
var ErrAppleTokenInvalid = errors.New("apple identity token invalid")

// Validate parses + verifies the supplied identity token and returns
// the structured claims when it passes. The nonce parameter is the
// nonce the CLIENT claims it generated; when non-empty we require the
// token's nonce claim to match it. When empty we skip the nonce check
// (older expo-apple-authentication versions don't surface the nonce).
//
// Validation order (fails fast on the first mismatch):
//  1. Parseable header + alg=RS256 + kid present
//  2. JWKS lookup for the kid (cache miss → eager refresh)
//  3. Signature verification with the resolved RSA public key
//  4. iss == "https://appleid.apple.com"
//  5. aud contains AppleAudience (Apple ships aud as a string OR array)
//  6. exp > now (clock injected; tests can advance)
//  7. nonce matches (when request-side nonce is non-empty)
func (v *AppleValidator) Validate(ctx context.Context, idToken, clientNonce string) (*AppleClaims, error) {
	parsed, err := jwt.ParseWithClaims(idToken, &AppleClaims{}, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodRS256.Alg() {
			return nil, fmt.Errorf("%w: unexpected alg=%v", ErrAppleTokenInvalid, t.Method.Alg())
		}
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, fmt.Errorf("%w: missing kid header", ErrAppleTokenInvalid)
		}
		key, err := v.JWKS.GetKey(ctx, kid)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrAppleTokenInvalid, err)
		}
		return key, nil
	}, jwt.WithLeeway(30*time.Second))
	if err != nil {
		// Surface expired / signature errors uniformly. The wrapped
		// jwt.Error variants (ErrTokenExpired, ErrTokenSignatureInvalid)
		// are useful in tests; the public API only exposes the sentinel.
		return nil, fmt.Errorf("%w: %v", ErrAppleTokenInvalid, err)
	}
	if !parsed.Valid {
		return nil, fmt.Errorf("%w: token marked invalid by jwt lib", ErrAppleTokenInvalid)
	}
	claims, ok := parsed.Claims.(*AppleClaims)
	if !ok {
		return nil, fmt.Errorf("%w: claims type mismatch", ErrAppleTokenInvalid)
	}
	if claims.Issuer != v.Issuer {
		return nil, fmt.Errorf("%w: iss=%q want=%q", ErrAppleTokenInvalid, claims.Issuer, v.Issuer)
	}
	if !audienceContains(claims.Audience, v.Audience) {
		return nil, fmt.Errorf("%w: aud=%v want=%q", ErrAppleTokenInvalid, claims.Audience, v.Audience)
	}
	// Manual clock check — jwt.ParseWithClaims already enforces exp, but
	// we also reject tokens whose exp claim is entirely missing.
	if claims.ExpiresAt == nil {
		return nil, fmt.Errorf("%w: exp claim missing", ErrAppleTokenInvalid)
	}
	now := v.now()
	if now.After(claims.ExpiresAt.Time) {
		return nil, fmt.Errorf("%w: token expired at %s", ErrAppleTokenInvalid, claims.ExpiresAt.Time)
	}
	if clientNonce != "" {
		if claims.Nonce == "" {
			return nil, fmt.Errorf("%w: client supplied nonce but token has none", ErrAppleTokenInvalid)
		}
		if claims.Nonce != clientNonce {
			return nil, fmt.Errorf("%w: nonce mismatch", ErrAppleTokenInvalid)
		}
	}
	if claims.Subject == "" {
		return nil, fmt.Errorf("%w: sub claim missing", ErrAppleTokenInvalid)
	}
	return claims, nil
}

func (v *AppleValidator) now() time.Time {
	if v.Now == nil {
		return time.Now()
	}
	return v.Now()
}

func audienceContains(aud jwt.ClaimStrings, want string) bool {
	for _, a := range aud {
		if a == want {
			return true
		}
	}
	return false
}
