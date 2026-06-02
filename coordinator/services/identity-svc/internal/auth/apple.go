// Package auth — Apple sign-in completer.
//
// Wires the AppleValidator + JWKS cache + store into a single
// transactional sign-in flow:
//
//   1. Validate the JWT (signature, iss, aud, nonce, exp). On failure,
//      surface ErrAppleTokenInvalid.
//   2. Compute SHA-256(apple_sub + APPLE_SUB_SALT). The salt is held in
//      the Service as a byte slice; sourced from env APPLE_SUB_SALT by
//      the bootstrap.
//   3. Find user by apple_sub_hash. On miss, mint a fresh User row
//      WITH the hash in the same INSERT (no race window where another
//      concurrent first-launch could mint a duplicate).
//   4. Ensure the companion `identifiers` row exists (kind='apple',
//      subject=<raw sub>) so the account-management surface can see
//      the binding. The INSERT is idempotent via unique (kind, subject).
//   5. Issue the standard AuthBundle (access JWT + refresh token).
//
// Email handling: Apple may return a real address, an Apple
// private-relay address, or no email at all. We accept whatever's
// present as the User's primary_email on first sign-in but NEVER use
// email as the canonical id — the salted sub hash is the only identity
// anchor.

package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/store"
)

// AppleSignInResult is what the handler returns up. Mirrors the proto
// AppleSignInResponse but keeps store.User as the canonical user
// reference (handler converts to JSON).
type AppleSignInResult struct {
	Bundle         *Bundle
	NewUser        bool
	WalletAddress  string
	NonceValidated bool
}

// hashAppleSub computes SHA-256(rawSub + salt). Returns a fixed-size
// 32-byte slice (matching the BYTEA column width on users.apple_sub_hash).
func hashAppleSub(rawSub string, salt []byte) []byte {
	h := sha256.New()
	h.Write([]byte(rawSub))
	h.Write(salt)
	return h.Sum(nil)
}

// CompleteAppleSignIn validates the supplied identity token, finds or
// creates the user, mints a bundle, and returns the result.
//
// Concurrency: the find-or-create runs inside a Serializable tx so two
// concurrent first-launch attempts from the same Apple sub return the
// same user (the loser of the unique-violation race re-reads).
func (s *Service) CompleteAppleSignIn(ctx context.Context, idToken, clientNonce, fullName string, req *http.Request) (*AppleSignInResult, error) {
	if s.Apple == nil || s.AppleSubSalt == nil {
		return nil, errors.New("auth: apple sign-in not configured")
	}
	claims, err := s.Apple.Validate(ctx, idToken, clientNonce)
	if err != nil {
		return nil, err
	}
	hash := hashAppleSub(claims.Subject, s.AppleSubSalt)
	nonceValidated := clientNonce != ""

	var result AppleSignInResult
	result.NonceValidated = nonceValidated

	err = s.Store.WithTx(ctx, func(tx pgx.Tx) error {
		// 1. Fast path: existing apple_sub_hash row.
		user, err := s.Store.FindUserByAppleSubHash(ctx, tx, hash)
		if err == nil {
			// Returning user — touch / backfill the companion identifier
			// row, then mint a bundle.
			if err := s.ensureAppleIdentifierTx(ctx, tx, user.ID, claims.Subject, claims.Email); err != nil {
				return err
			}
			bundle, err := s.issueBundleTx(ctx, tx, user, req, false, false)
			if err != nil {
				return err
			}
			result.Bundle = bundle
			result.NewUser = false
			result.WalletAddress = s.firstSolanaAddressTx(ctx, tx, user.ID)
			return nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("find user by apple_sub_hash: %w", err)
		}
		// 2. Fresh user — INSERT with the hash.
		u := &store.User{
			PrimaryEmail: claims.Email,
			DisplayName:  fullName,
		}
		if err := s.Store.CreateUserWithAppleSubHash(ctx, tx, u, hash); err != nil {
			if isUniqueViolation(err) {
				// Lost the race — re-read.
				user, rerr := s.Store.FindUserByAppleSubHash(ctx, tx, hash)
				if rerr != nil {
					return fmt.Errorf("apple race re-read: %w", rerr)
				}
				bundle, err := s.issueBundleTx(ctx, tx, user, req, false, false)
				if err != nil {
					return err
				}
				result.Bundle = bundle
				result.NewUser = false
				result.WalletAddress = s.firstSolanaAddressTx(ctx, tx, user.ID)
				return nil
			}
			return fmt.Errorf("create apple user: %w", err)
		}
		// Companion identifier row (idempotent via unique kind+subject).
		ident := &store.Identifier{
			UserID:   u.ID,
			Kind:     store.KindApple,
			Subject:  claims.Subject,
			Email:    claims.Email,
			Verified: true,
		}
		if err := s.Store.CreateIdentifier(ctx, tx, ident); err != nil {
			if !isUniqueViolation(err) {
				return fmt.Errorf("create apple identifier: %w", err)
			}
		}
		// Personal workspace + invite consumption mirror the Google path
		// so a fresh Apple user lands with a workspace ready to use.
		if _, err := s.Store.EnsurePersonalWorkspace(ctx, tx, u); err != nil {
			return fmt.Errorf("ensure personal workspace: %w", err)
		}
		if claims.Email != "" {
			if _, err := s.Store.ConsumeInvitesForEmail(ctx, tx, claims.Email, u.ID); err != nil {
				// Non-fatal — only affects workspace list visibility.
				s.Logger.Warn("apple: consume invites failed", "error", err.Error())
			}
		}
		bundle, err := s.issueBundleTx(ctx, tx, u, req, true, false)
		if err != nil {
			return err
		}
		result.Bundle = bundle
		result.NewUser = true
		result.WalletAddress = "" // Brand new — no wallet bound yet.
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// ensureAppleIdentifierTx makes sure the (kind=apple, subject=sub) row
// exists for the user. Idempotent — a unique violation is a no-op.
func (s *Service) ensureAppleIdentifierTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, sub, email string) error {
	existing, err := s.Store.FindIdentifierBySubject(ctx, tx, store.KindApple, sub)
	if err == nil && existing != nil {
		_ = s.Store.TouchIdentifier(ctx, tx, existing.ID)
		return nil
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("find apple identifier: %w", err)
	}
	ident := &store.Identifier{
		UserID:   userID,
		Kind:     store.KindApple,
		Subject:  sub,
		Email:    email,
		Verified: true,
	}
	if err := s.Store.CreateIdentifier(ctx, tx, ident); err != nil {
		if isUniqueViolation(err) {
			return nil
		}
		return fmt.Errorf("create apple identifier: %w", err)
	}
	return nil
}

// firstSolanaAddressTx fetches the first base58 Solana address bound to
// the user, or "" if none. Used to populate AppleSignInResponse.wallet_address.
func (s *Service) firstSolanaAddressTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) string {
	idents, err := s.Store.ListIdentifiersForUser(ctx, tx, userID)
	if err != nil {
		return ""
	}
	for _, i := range idents {
		if i.Kind == store.KindSolana && i.Subject != "" {
			return i.Subject
		}
	}
	return ""
}
