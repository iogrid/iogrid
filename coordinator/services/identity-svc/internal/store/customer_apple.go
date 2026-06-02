// Package store — Apple sign-in helpers.
//
// The Sign-in-with-Apple flow (#582 / EPIC #581) introduces a fast
// denormalised lookup on the `users` table keyed by the 32-byte
// SHA-256 hash of (apple_sub + APPLE_SUB_SALT). This file isolates
// every method that reads / writes that column so the rest of the
// store package stays oblivious to the Apple-specific path.
//
// The companion Identifier row (kind='apple', subject=<raw sub>)
// continues to be the canonical join point for the account-management
// surface — `apple_sub_hash` on the user row exists purely to keep the
// sign-in hot path O(1) (one indexed BYTEA equality) and to enforce a
// global uniqueness invariant per environment salt.
package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// FindUserByAppleSubHash returns the user whose apple_sub_hash matches
// the supplied 32-byte digest. Returns ErrNotFound when no row exists
// (i.e. first-time sign-in for this Apple sub on this deployment).
func (s *Store) FindUserByAppleSubHash(ctx context.Context, q Querier, hash []byte) (*User, error) {
	if q == nil {
		q = s.Pool
	}
	u := &User{}
	err := q.QueryRow(ctx, `
		SELECT id, primary_email, display_name, picture_url, roles,
		       created_at, updated_at, last_login_at, deleted_at,
		       preferred_landing_role
		  FROM users
		 WHERE apple_sub_hash = $1
		   AND deleted_at IS NULL`, hash).
		Scan(&u.ID, &u.PrimaryEmail, &u.DisplayName, &u.PictureURL, &u.Roles,
			&u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt, &u.DeletedAt,
			&u.PreferredLandingRole)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

// SetUserAppleSubHash binds the hash to an existing user row. Used when
// an Apple-via-email user signs in after they'd already created a
// magic-link / Google-bound account that we'd want to auto-merge into.
// Today we don't auto-merge across Apple (email may be private-relay),
// but the helper is here for the binding-after-recovery story.
func (s *Store) SetUserAppleSubHash(ctx context.Context, q Querier, id uuid.UUID, hash []byte) error {
	if q == nil {
		q = s.Pool
	}
	_, err := q.Exec(ctx, `
		UPDATE users
		   SET apple_sub_hash = $2,
		       updated_at = now()
		 WHERE id = $1`, id, hash)
	return err
}

// CreateUserWithAppleSubHash mints a fresh user row populated with the
// apple_sub_hash column in a single INSERT. The caller is the Apple
// sign-in completer; it then inserts a companion identifiers row
// (kind='apple', subject=<raw sub>) so the user can also be looked up
// by the canonical identifier path used by the account-management UI.
//
// roles may be nil (defaults to []). The returned User has ID / created_at
// / updated_at populated.
func (s *Store) CreateUserWithAppleSubHash(ctx context.Context, q Querier, u *User, hash []byte) error {
	if q == nil {
		q = s.Pool
	}
	roles := u.Roles
	if roles == nil {
		roles = []string{}
	}
	row := q.QueryRow(ctx, `
		INSERT INTO users (primary_email, display_name, picture_url, roles, apple_sub_hash)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at`,
		u.PrimaryEmail, u.DisplayName, u.PictureURL, roles, hash)
	return row.Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt)
}
