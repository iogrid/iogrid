// SIWS (Sign-In-With-Solana) wallet binding. Wires the verifier in
// internal/siws/ onto the identifiers table so providers can bind one or
// more Solana wallets to their User. Token / merge / step-up policy
// remains in service.go; this file only handles the wallet primitive.
package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/siws"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/store"
)

// SiwsDomain is the scope domain rendered into the SIWS signing prompt.
// Hard-coded rather than read from config because it is the user-visible
// identifier the operator approves in their wallet — any drift between
// what we sign and what we verify must be a code-level commit, not a
// runtime knob.
const SiwsDomain = "iogrid.org"

// SiwsChallengeTTL is how long a challenge stays live. Re-exported from
// the siws package so HTTP responses can surface the same value the
// store enforces.
const SiwsChallengeTTL = siws.DefaultChallengeTTL

// WithSiwsChallenges installs a challenge store on the service. Wired
// from cmd/identity-svc once the Redis client is up. When unset the
// service falls back to an in-memory store on first use (dev / tests).
func (s *Service) WithSiwsChallenges(store siws.ChallengeStore) *Service {
	s.SiwsChallenges = store
	return s
}

// challenges returns a live ChallengeStore. Falls back to an in-memory
// store the first time it is called with no production wiring — that's
// safe in single-pod dev / unit-tests, NOT in multi-replica prod (Redis
// is the only correct choice there). Wired in cmd/identity-svc.
func (s *Service) challenges() siws.ChallengeStore {
	if s.SiwsChallenges == nil {
		s.SiwsChallenges = siws.NewMemoryChallengeStore()
		s.Logger.Warn("siws: no challenge store configured; using in-memory fallback (NOT safe across pods)")
	}
	return s.SiwsChallenges
}

// SiwsStartResult is the value returned to the caller of StartSiwsBinding.
// Carries the exact bytes the wallet must sign + the TTL on the nonce.
type SiwsStartResult struct {
	Challenge string
	ExpiresAt time.Time
}

// StartSiwsBinding allocates a fresh challenge and stores it under
// (walletAddress) keyed in Redis. userID may be uuid.Nil — Complete will
// create a fresh User when create_if_missing is true.
func (s *Service) StartSiwsBinding(ctx context.Context, userID uuid.UUID, walletAddress string) (*SiwsStartResult, error) {
	walletAddress = strings.TrimSpace(walletAddress)
	if _, err := siws.DecodeAddress(walletAddress); err != nil {
		return nil, err
	}
	nonce, err := siws.NewNonce()
	if err != nil {
		return nil, fmt.Errorf("siws: nonce: %w", err)
	}
	msg := siws.BuildMessage(SiwsDomain, walletAddress, nonce)
	expiresAt := time.Now().Add(SiwsChallengeTTL)

	userIDStr := ""
	if userID != uuid.Nil {
		userIDStr = userID.String()
	}
	if err := s.challenges().Put(ctx, siws.ChallengeRecord{
		WalletAddress: walletAddress,
		Nonce:         nonce,
		Message:       msg,
		UserID:        userIDStr,
		ExpiresAt:     expiresAt,
	}, SiwsChallengeTTL); err != nil {
		return nil, fmt.Errorf("siws: store challenge: %w", err)
	}
	s.Logger.Info("siws: challenge issued",
		slog.String("wallet", walletAddress),
		slog.String("user_id", userIDStr),
	)
	return &SiwsStartResult{Challenge: msg, ExpiresAt: expiresAt}, nil
}

// SiwsCompleteResult bundles the resolved binding + an optional sign-in
// AuthBundle. The bundle is non-nil only when this completion created a
// brand-new User (create_if_missing path) — otherwise the caller is
// already authenticated and continues using their existing access token.
type SiwsCompleteResult struct {
	UserID      uuid.UUID
	Address     string
	NewUser     bool
	Bundle      *Bundle
	BoundAt     time.Time
	IdentifierID uuid.UUID
}

// CompleteSiwsBinding verifies the signature, consumes the challenge,
// and either:
//   - attaches a new Identifier(kind=solana, subject=address) to userID, or
//   - when userID is empty AND createIfMissing is true, creates a fresh
//     User whose only identifier is this wallet (and mints a sign-in
//     bundle so the caller is logged in).
//
// Replay defence: the challenge is GETDEL-ed before signature verification
// so a leaked signature can never be re-played against the server.
func (s *Service) CompleteSiwsBinding(ctx context.Context, userID uuid.UUID, walletAddress, signature string, createIfMissing bool, req *http.Request) (*SiwsCompleteResult, error) {
	walletAddress = strings.TrimSpace(walletAddress)
	if _, err := siws.DecodeAddress(walletAddress); err != nil {
		return nil, err
	}
	// 1) Consume the challenge atomically. If the wallet retries with no
	//    prior Start, this fails fast.
	chal, err := s.challenges().Consume(ctx, walletAddress)
	if err != nil {
		return nil, err
	}
	// 2) Verify the signature against the EXACT bytes we issued.
	if err := siws.VerifySignature(walletAddress, chal.Message, signature); err != nil {
		return nil, err
	}
	// 3) Cross-check the user binding: if Start was called with a userID,
	//    Complete must match — defends against an authenticated caller
	//    bouncing a different user's challenge through their wallet.
	expectedUser := ""
	if userID != uuid.Nil {
		expectedUser = userID.String()
	}
	if chal.UserID != "" && chal.UserID != expectedUser {
		return nil, errors.New("siws: user_id does not match challenge")
	}

	result := &SiwsCompleteResult{Address: walletAddress, BoundAt: time.Now().UTC()}

	err = s.Store.WithTx(ctx, func(tx pgx.Tx) error {
		// If the wallet is already bound globally we reject; one wallet
		// belongs to one user only (the unique index on identifiers
		// (kind, subject) enforces this at the DB layer too).
		existing, err := s.Store.FindIdentifierBySubject(ctx, tx, store.KindSolana, walletAddress)
		switch {
		case err == nil:
			// Already bound. Allow idempotent re-bind to the same user;
			// reject if some other user holds it.
			if existing.UserID != userID {
				return errors.New("siws: wallet already bound to another user")
			}
			// Touch + return existing binding without minting a new one.
			_ = s.Store.TouchIdentifier(ctx, tx, existing.ID)
			result.IdentifierID = existing.ID
			result.UserID = existing.UserID
			return nil
		case errors.Is(err, store.ErrNotFound):
			// Expected — fall through to create.
		default:
			return err
		}

		// 4) Either attach to an existing user OR mint a new one.
		var bindUser *store.User
		if userID != uuid.Nil {
			u, err := s.Store.GetUser(ctx, tx, userID)
			if err != nil {
				return fmt.Errorf("siws: load user: %w", err)
			}
			bindUser = u
		} else {
			if !createIfMissing {
				return errors.New("siws: anonymous bind requires create_if_missing=true")
			}
			u := &store.User{
				PrimaryEmail: "",
				DisplayName:  "",
				Roles:        []string{"USER_ROLE_PROVIDER"},
			}
			if err := s.Store.CreateUser(ctx, tx, u); err != nil {
				return fmt.Errorf("siws: create user: %w", err)
			}
			bindUser = u
			result.NewUser = true
		}

		ident := &store.Identifier{
			UserID:   bindUser.ID,
			Kind:     store.KindSolana,
			Subject:  walletAddress,
			Verified: true,
		}
		if err := s.Store.CreateIdentifier(ctx, tx, ident); err != nil {
			return fmt.Errorf("siws: create identifier: %w", err)
		}
		result.IdentifierID = ident.ID
		result.UserID = bindUser.ID

		// 5) If we just created a User, also mint a sign-in bundle so the
		//    onboarding flow doesn't need a second round-trip.
		if result.NewUser {
			bundle, err := s.issueBundleTx(ctx, tx, bindUser, req, true, false)
			if err != nil {
				return fmt.Errorf("siws: issue bundle: %w", err)
			}
			result.Bundle = bundle
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.Logger.Info("siws: wallet bound",
		slog.String("wallet", walletAddress),
		slog.String("user_id", result.UserID.String()),
		slog.Bool("new_user", result.NewUser),
	)
	return result, nil
}

// ListBoundWallets returns every Solana wallet currently bound to the
// user. Order is creation-time ascending so wallets shown in the UI keep
// a stable position as new ones are added.
func (s *Service) ListBoundWallets(ctx context.Context, userID uuid.UUID) ([]store.Identifier, error) {
	if userID == uuid.Nil {
		return nil, errors.New("siws: user_id is required")
	}
	return s.Store.ListIdentifiersForUserByKind(ctx, nil, userID, store.KindSolana)
}

// UnbindWallet removes a Solana identifier from a user. Returns
// ErrNotFound when the wallet is not bound to the supplied user — the
// caller maps that to a 404 so an attacker cannot probe for which
// wallets some other user owns.
func (s *Service) UnbindWallet(ctx context.Context, userID uuid.UUID, walletAddress string) error {
	walletAddress = strings.TrimSpace(walletAddress)
	if _, err := siws.DecodeAddress(walletAddress); err != nil {
		return err
	}
	ident, err := s.Store.FindIdentifierBySubjectAndUser(ctx, nil, store.KindSolana, walletAddress, userID)
	if err != nil {
		return err
	}
	if err := s.Store.DeleteIdentifier(ctx, nil, ident.ID); err != nil {
		return err
	}
	s.Logger.Info("siws: wallet unbound",
		slog.String("wallet", walletAddress),
		slog.String("user_id", userID.String()),
	)
	return nil
}
