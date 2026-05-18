package auth

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/oauth/google"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/store"
)

// completeGoogleForTest is the same transactional core CompleteGoogle uses
// after a successful OIDC exchange — exposed so integration tests can
// drive the auto-merge logic without standing up a fake OIDC provider.
// NOT exported via any HTTP route.
func (s *Service) completeGoogleForTest(ctx context.Context, id *google.Identity, req *http.Request) (*Bundle, error) {
	if !id.EmailVerified {
		return nil, errors.New("auth: Google email not verified")
	}
	var bundle *Bundle
	err := s.Store.WithTx(ctx, func(tx pgx.Tx) error {
		existing, err := s.Store.FindIdentifierBySubject(ctx, tx, store.KindGoogle, id.Subject)
		if err == nil {
			_ = s.Store.TouchIdentifier(ctx, tx, existing.ID)
			user, err := s.Store.GetUser(ctx, tx, existing.UserID)
			if err != nil {
				return err
			}
			bundle, err = s.issueBundleTx(ctx, tx, user, req, false, false)
			return err
		}
		if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		mergeTarget, matchedEmail, err := s.findMergeCandidate(ctx, tx, id)
		if err != nil {
			return err
		}
		if mergeTarget != nil {
			ident := &store.Identifier{
				UserID:       mergeTarget.ID,
				Kind:         store.KindGoogle,
				Subject:      id.Subject,
				Email:        id.Email,
				Verified:     true,
				HostedDomain: id.HostedDomain,
			}
			if err := s.Store.CreateIdentifier(ctx, tx, ident); err != nil {
				return err
			}
			if err := s.Store.InsertMergeAudit(ctx, tx, &store.MergeAudit{
				PrimaryUserID: mergeTarget.ID,
				Reason:        "google_verified_secondary_match",
				MatchedEmail:  matchedEmail,
				MatchedVia:    "magic_link_identifier_email",
			}); err != nil {
				return err
			}
			bundle, err = s.issueBundleTx(ctx, tx, mergeTarget, req, false, true)
			return err
		}
		u := &store.User{
			PrimaryEmail: id.Email,
			DisplayName:  id.Name,
			PictureURL:   id.Picture,
		}
		if err := s.Store.CreateUser(ctx, tx, u); err != nil {
			return err
		}
		ident := &store.Identifier{
			UserID:       u.ID,
			Kind:         store.KindGoogle,
			Subject:      id.Subject,
			Email:        id.Email,
			Verified:     true,
			HostedDomain: id.HostedDomain,
		}
		if err := s.Store.CreateIdentifier(ctx, tx, ident); err != nil {
			return err
		}
		bundle, err = s.issueBundleTx(ctx, tx, u, req, true, false)
		return err
	})
	if err != nil {
		return nil, err
	}
	return bundle, nil
}
