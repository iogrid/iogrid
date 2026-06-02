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
			wallet, err := s.firstSolanaAddressTx(ctx, tx, user.ID)
			if err != nil {
				return fmt.Errorf("apple: first solana address: %w", err)
			}
			result.Bundle = bundle
			result.NewUser = false
			result.WalletAddress = wallet
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
				// Lost the race against another concurrent first-launch
				// for the same Apple sub. The unique violation has
				// already aborted this transaction at the Postgres
				// level (any further statement on `tx` would return
				// SQLSTATE 25P02 "current transaction is aborted"), so
				// we cannot do an in-tx re-read here — instead we
				// return store.ErrRetryTx and let store.WithTx restart
				// the whole flow. On retry the fast path
				// (FindUserByAppleSubHash) sees the winner's committed
				// row and returns the existing user. See #620.
				return fmt.Errorf("apple race lost on user insert: %w", store.ErrRetryTx)
			}
			return fmt.Errorf("create apple user: %w", err)
		}
		// Companion identifier row (idempotent via unique kind+subject).
		// A 23505 here means a concurrent first-launch raced us on the
		// identifier insert too — same recovery as the user-insert
		// race above: bounce back through WithTx so the next attempt
		// hits the fast path with both rows present and committed.
		// (Any further statement on this tx would return 25P02 "current
		// transaction is aborted" — we cannot continue inline.)
		ident := &store.Identifier{
			UserID:   u.ID,
			Kind:     store.KindApple,
			Subject:  claims.Subject,
			Email:    claims.Email,
			Verified: true,
		}
		if err := s.Store.CreateIdentifier(ctx, tx, ident); err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("apple race lost on identifier insert: %w", store.ErrRetryTx)
			}
			return fmt.Errorf("create apple identifier: %w", err)
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
// exists for the user. Idempotent across SEQUENTIAL re-bindings, but
// CONCURRENT races (two devices hitting first-launch at the same time)
// need to bubble through WithTx — see the inline note for the 23505
// branch below.
func (s *Service) ensureAppleIdentifierTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, sub, email string) error {
	existing, err := s.Store.FindIdentifierBySubject(ctx, tx, store.KindApple, sub)
	if err == nil && existing != nil {
		// Propagate the TouchIdentifier error: under Serializable
		// isolation a SQLSTATE 40001 here would otherwise silently
		// abort the rest of the transaction (every later statement
		// would then surface as 25P02 "current transaction is
		// aborted"). Bubbling lets WithTx see the 40001 and retry.
		if err := s.Store.TouchIdentifier(ctx, tx, existing.ID); err != nil {
			return fmt.Errorf("touch apple identifier: %w", err)
		}
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
			// A concurrent goroutine inserted the row between our
			// FindIdentifierBySubject and CreateIdentifier calls. The
			// 23505 has aborted this transaction at the Postgres level
			// (any further statement would return 25P02), so we cannot
			// just `return nil` — bounce back through WithTx and let
			// the next attempt hit the FindIdentifierBySubject fast
			// path. See #620.
			return fmt.Errorf("apple race lost on identifier ensure: %w", store.ErrRetryTx)
		}
		return fmt.Errorf("create apple identifier: %w", err)
	}
	return nil
}

// firstSolanaAddressTx fetches the first base58 Solana address bound to
// the user, or "" if none. Used to populate AppleSignInResponse.wallet_address.
//
// Returns the error from the underlying ListIdentifiersForUser so the
// caller can propagate SQLSTATE 40001 back through WithTx for retry.
// (Swallowing it silently would let the transaction continue in an
// aborted state and surface every later statement as 25P02 "current
// transaction is aborted".)
func (s *Service) firstSolanaAddressTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (string, error) {
	idents, err := s.Store.ListIdentifiersForUser(ctx, tx, userID)
	if err != nil {
		return "", err
	}
	for _, i := range idents {
		if i.Kind == store.KindSolana && i.Subject != "" {
			return i.Subject, nil
		}
	}
	return "", nil
}
