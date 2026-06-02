package store

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a lookup matches zero rows. Use
// errors.Is(err, store.ErrNotFound) to detect.
var ErrNotFound = errors.New("identity-svc: not found")

// ErrRetryTx is a sentinel that callers inside WithTx can return when
// they detect a transient, idempotent-on-retry conflict (e.g. a unique
// violation racing two concurrent first-launch sign-ins for the same
// Apple sub — see #620). WithTx rolls back + restarts the transaction
// when fn returns an error that wraps ErrRetryTx, up to withTxMaxRetries
// times. On retry, fn typically takes the fast path (the winning
// transaction has already committed the row that triggered the
// conflict) and returns success.
var ErrRetryTx = errors.New("identity-svc: retry transaction")

// Store wraps the pgx pool with typed methods. Methods accept a Querier
// so callers can either pass *pgxpool.Pool (auto-pooled queries) or
// pgx.Tx (atomic merge / sign-in flows).
type Store struct {
	Pool *pgxpool.Pool
}

// Querier is the minimal subset of pgx.Tx + *pgxpool.Pool used by store
// methods. Lets handlers pass either a transaction or the pool without
// changing call sites.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// New constructs a Store. Callers wire the pool from coordinator/shared/db.
func New(pool *pgxpool.Pool) *Store {
	return &Store{Pool: pool}
}

// withTxMaxRetries caps the number of times WithTx will retry on a
// Postgres SQLSTATE 40001 (serialization_failure). Eight retries with
// the exponential-jitter backoff below gives ~250ms of total slack,
// which empirically absorbs a 16-way concurrent first-launch storm
// against the same Apple sub on commodity hardware (#620 regression
// test). Beyond that the surfaced error is genuine contention, not
// transient race-loss, and the caller should surface it.
const withTxMaxRetries = 8

// withTxBaseBackoff is the seed value for the exponential-with-jitter
// delay between WithTx retries. The first retry waits ~1ms, doubling
// per attempt — small enough to stay invisible to the caller (a single
// sign-in already amortises >>50ms over JWKS + JWT validation + bcrypt)
// but large enough that two retrying transactions don't collide on the
// same clock tick.
const withTxBaseBackoff = 1 * time.Millisecond

// WithTx runs fn inside a serializable Postgres transaction. Used by
// sign-in flows that must atomically (a) find-or-create the user, (b)
// upsert the identifier, (c) insert a session row.
//
// Retry semantics: Postgres at Serializable isolation can reject a
// committing transaction with SQLSTATE 40001 (read/write dependency
// cycle) when multiple sessions race for the same row — most notably
// the Apple sign-in find-or-create flow when multiple devices launch
// the app concurrently with the same Apple sub (#620). On 40001 the
// entire transaction is aborted by Postgres; the loser must roll back
// + retry. WithTx absorbs that retry loop internally so callers can
// stay oblivious to the isolation-level wrinkle. fn MUST be idempotent
// under retry — every WithTx caller in identity-svc today already
// satisfies that (the only side effect outside the tx is the async
// merge notification fired AFTER WithTx returns).
func (s *Store) WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	var lastErr error
	for attempt := 0; attempt <= withTxMaxRetries; attempt++ {
		err := s.runTxOnce(ctx, fn)
		if err == nil {
			return nil
		}
		if !isRetriable(err) {
			return err
		}
		lastErr = err
		if attempt == withTxMaxRetries {
			break
		}
		// Exponential backoff with jitter: 1ms→2ms→4ms ±50%. Sleeping
		// here (rather than busy-looping) hands the goroutine off so
		// the winning transaction can commit before we retry.
		backoff := withTxBaseBackoff << attempt
		jitter := time.Duration(rand.Int63n(int64(backoff)))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff/2 + jitter):
		}
	}
	return fmt.Errorf("withTx: exhausted %d retries on serialization failure: %w", withTxMaxRetries, lastErr)
}

// runTxOnce runs fn inside a single serializable transaction and
// returns the first error encountered (either from fn or from commit).
func (s *Store) runTxOnce(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// isSerializationFailure reports whether err is a Postgres SQLSTATE
// 40001 (serialization_failure). Used by WithTx to identify retriable
// concurrency conflicts under Serializable isolation.
func isSerializationFailure(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40001"
}

// isRetriable reports whether err signals that WithTx should roll back
// the current transaction + restart fn. Two triggers:
//
//   - SQLSTATE 40001 (serialization_failure): Postgres' canonical
//     Serializable retry signal, raised when a read/write dependency
//     cycle is detected — most commonly when two concurrent
//     CompleteAppleSignIn calls race the find-or-create flow (#620).
//   - ErrRetryTx sentinel: callers inside fn can wrap this when they
//     detect a transient conflict that isn't a 40001 — most commonly a
//     SQLSTATE 23505 unique-violation that the next attempt's fast
//     path will resolve cleanly (the winning transaction's row has
//     committed by retry time, so the lookup hits).
func isRetriable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrRetryTx) {
		return true
	}
	return isSerializationFailure(err)
}

// --- users ----------------------------------------------------------------

// CreateUser inserts a new user row. roles may be nil.
func (s *Store) CreateUser(ctx context.Context, q Querier, u *User) error {
	if q == nil {
		q = s.Pool
	}
	roles := u.Roles
	if roles == nil {
		roles = []string{}
	}
	row := q.QueryRow(ctx, `
		INSERT INTO users (primary_email, display_name, picture_url, roles)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at, updated_at`,
		u.PrimaryEmail, u.DisplayName, u.PictureURL, roles)
	return row.Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt)
}

// GetUser returns a user by ID.
func (s *Store) GetUser(ctx context.Context, q Querier, id uuid.UUID) (*User, error) {
	if q == nil {
		q = s.Pool
	}
	u := &User{}
	err := q.QueryRow(ctx, `
		SELECT id, primary_email, display_name, picture_url, roles,
		       created_at, updated_at, last_login_at, deleted_at,
		       preferred_landing_role, notification_prefs::text
		  FROM users
		 WHERE id = $1`, id).
		Scan(&u.ID, &u.PrimaryEmail, &u.DisplayName, &u.PictureURL, &u.Roles,
			&u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt, &u.DeletedAt,
			&u.PreferredLandingRole, &u.NotificationPrefs)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

// SetPreferredLandingRole records the consumer-app persona the user
// picked on /welcome (EPIC #422 / PR #445). Pass one of "provider",
// "customer", "vpn" — or the empty string to clear, which re-triggers
// the /welcome picker on the next sign-in.
//
// The enum cast happens in Postgres; an invalid string surfaces as a
// regular pgx error (SQLSTATE 22P02) which handlers translate to
// CodeInvalidArgument.
func (s *Store) SetPreferredLandingRole(ctx context.Context, q Querier, id uuid.UUID, role string) error {
	if q == nil {
		q = s.Pool
	}
	if role == "" {
		_, err := q.Exec(ctx, `
			UPDATE users SET preferred_landing_role = NULL, updated_at = now()
			 WHERE id = $1`, id)
		return err
	}
	_, err := q.Exec(ctx, `
		UPDATE users
		   SET preferred_landing_role = $2::preferred_landing_role,
		       updated_at = now()
		 WHERE id = $1`, id, role)
	return err
}

// SetNotificationPrefs persists the user's notification-channel
// preferences (Refs #631). prefsJSON MUST be a JSON object string (the
// handler validates it parses before calling); pass the empty string to
// clear the column back to NULL, which re-arms the all-on-email default
// on the /account/notifications surface.
//
// The ::jsonb cast happens in Postgres; malformed JSON surfaces as a
// regular pgx error (SQLSTATE 22P02) which handlers translate to
// CodeInvalidArgument.
func (s *Store) SetNotificationPrefs(ctx context.Context, q Querier, id uuid.UUID, prefsJSON string) error {
	if q == nil {
		q = s.Pool
	}
	if prefsJSON == "" {
		_, err := q.Exec(ctx, `
			UPDATE users SET notification_prefs = NULL, updated_at = now()
			 WHERE id = $1`, id)
		return err
	}
	_, err := q.Exec(ctx, `
		UPDATE users
		   SET notification_prefs = $2::jsonb,
		       updated_at = now()
		 WHERE id = $1`, id, prefsJSON)
	return err
}

// UpdateLastLogin stamps last_login_at = now() for the given user.
func (s *Store) UpdateLastLogin(ctx context.Context, q Querier, id uuid.UUID) error {
	if q == nil {
		q = s.Pool
	}
	_, err := q.Exec(ctx, `
		UPDATE users SET last_login_at = now(), updated_at = now()
		 WHERE id = $1`, id)
	return err
}

// UpdateUserProfile mutates the user-editable fields. Empty strings are
// treated as "do not change" so partial updates are caller-friendly.
func (s *Store) UpdateUserProfile(ctx context.Context, q Querier, id uuid.UUID, displayName, primaryEmail, pictureURL string) (*User, error) {
	if q == nil {
		q = s.Pool
	}
	sets := []string{"updated_at = now()"}
	args := []any{id}
	if displayName != "" {
		args = append(args, displayName)
		sets = append(sets, fmt.Sprintf("display_name = $%d", len(args)))
	}
	if primaryEmail != "" {
		args = append(args, primaryEmail)
		sets = append(sets, fmt.Sprintf("primary_email = $%d", len(args)))
	}
	if pictureURL != "" {
		args = append(args, pictureURL)
		sets = append(sets, fmt.Sprintf("picture_url = $%d", len(args)))
	}
	sql := fmt.Sprintf("UPDATE users SET %s WHERE id = $1", strings.Join(sets, ", "))
	if _, err := q.Exec(ctx, sql, args...); err != nil {
		return nil, err
	}
	return s.GetUser(ctx, q, id)
}

// SoftDeleteUser flips deleted_at = now() so the row stays available for
// audit references but is hidden from active-user queries.
func (s *Store) SoftDeleteUser(ctx context.Context, q Querier, id uuid.UUID) error {
	if q == nil {
		q = s.Pool
	}
	_, err := q.Exec(ctx, `UPDATE users SET deleted_at = now(), updated_at = now() WHERE id = $1`, id)
	return err
}

// --- identifiers ----------------------------------------------------------

// CreateIdentifier inserts a new identifier. Caller is responsible for
// detecting unique-violation (kind, subject) when the row already exists.
func (s *Store) CreateIdentifier(ctx context.Context, q Querier, i *Identifier) error {
	if q == nil {
		q = s.Pool
	}
	var email any = nil
	if i.Email != "" {
		email = i.Email
	}
	row := q.QueryRow(ctx, `
		INSERT INTO identifiers (user_id, kind, subject, email, verified, hosted_domain)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, last_used_at`,
		i.UserID, i.Kind, i.Subject, email, i.Verified, i.HostedDomain)
	return row.Scan(&i.ID, &i.CreatedAt, &i.LastUsedAt)
}

// FindIdentifierBySubject looks up an identifier by (kind, subject). Used
// by the Google OAuth completer.
func (s *Store) FindIdentifierBySubject(ctx context.Context, q Querier, kind IdentifierKind, subject string) (*Identifier, error) {
	if q == nil {
		q = s.Pool
	}
	i := &Identifier{}
	var email *string
	err := q.QueryRow(ctx, `
		SELECT id, user_id, kind, subject, email, verified, hosted_domain, created_at, last_used_at
		  FROM identifiers
		 WHERE kind = $1 AND subject = $2`, kind, subject).
		Scan(&i.ID, &i.UserID, &i.Kind, &i.Subject, &email, &i.Verified, &i.HostedDomain, &i.CreatedAt, &i.LastUsedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if email != nil {
		i.Email = *email
	}
	return i, nil
}

// FindIdentifiersByEmail returns every identifier (any kind) whose email
// matches. Used by auto-merge to find verified magic-link rows.
func (s *Store) FindIdentifiersByEmail(ctx context.Context, q Querier, email string) ([]Identifier, error) {
	if q == nil {
		q = s.Pool
	}
	rows, err := q.Query(ctx, `
		SELECT id, user_id, kind, subject, COALESCE(email, ''), verified, hosted_domain, created_at, last_used_at
		  FROM identifiers
		 WHERE email = $1`, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Identifier
	for rows.Next() {
		var i Identifier
		if err := rows.Scan(&i.ID, &i.UserID, &i.Kind, &i.Subject, &i.Email, &i.Verified, &i.HostedDomain, &i.CreatedAt, &i.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// ListIdentifiersForUser returns every identifier bound to the user.
func (s *Store) ListIdentifiersForUser(ctx context.Context, q Querier, userID uuid.UUID) ([]Identifier, error) {
	if q == nil {
		q = s.Pool
	}
	rows, err := q.Query(ctx, `
		SELECT id, user_id, kind, subject, COALESCE(email, ''), verified, hosted_domain, created_at, last_used_at
		  FROM identifiers
		 WHERE user_id = $1
		 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Identifier
	for rows.Next() {
		var i Identifier
		if err := rows.Scan(&i.ID, &i.UserID, &i.Kind, &i.Subject, &i.Email, &i.Verified, &i.HostedDomain, &i.CreatedAt, &i.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// TouchIdentifier stamps last_used_at = now() to record a successful auth.
func (s *Store) TouchIdentifier(ctx context.Context, q Querier, id uuid.UUID) error {
	if q == nil {
		q = s.Pool
	}
	_, err := q.Exec(ctx, `UPDATE identifiers SET last_used_at = now() WHERE id = $1`, id)
	return err
}

// GetIdentifierForUser returns one identifier row by primary key and
// asserts it belongs to userID. Used by the account-identifiers Remove
// flow (RemoveIdentifier RPC) so a caller cannot scrub another user's
// identifiers by guessing UUIDs. Returns ErrNotFound when the row is
// missing OR is bound to a different user (same surface either way to
// keep the response leak-free).
func (s *Store) GetIdentifierForUser(ctx context.Context, q Querier, id, userID uuid.UUID) (*Identifier, error) {
	if q == nil {
		q = s.Pool
	}
	i := &Identifier{}
	var email *string
	err := q.QueryRow(ctx, `
		SELECT id, user_id, kind, subject, email, verified, hosted_domain, created_at, last_used_at
		  FROM identifiers
		 WHERE id = $1 AND user_id = $2`, id, userID).
		Scan(&i.ID, &i.UserID, &i.Kind, &i.Subject, &email, &i.Verified, &i.HostedDomain, &i.CreatedAt, &i.LastUsedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if email != nil {
		i.Email = *email
	}
	return i, nil
}

// FindIdentifierBySubjectAndUser is FindIdentifierBySubject scoped to a
// specific user. Used by SIWS unbind so we can confirm the wallet belongs
// to the caller before deletion. Returns ErrNotFound when no row matches.
func (s *Store) FindIdentifierBySubjectAndUser(ctx context.Context, q Querier, kind IdentifierKind, subject string, userID uuid.UUID) (*Identifier, error) {
	if q == nil {
		q = s.Pool
	}
	i := &Identifier{}
	var email *string
	err := q.QueryRow(ctx, `
		SELECT id, user_id, kind, subject, email, verified, hosted_domain, created_at, last_used_at
		  FROM identifiers
		 WHERE kind = $1 AND subject = $2 AND user_id = $3`, kind, subject, userID).
		Scan(&i.ID, &i.UserID, &i.Kind, &i.Subject, &email, &i.Verified, &i.HostedDomain, &i.CreatedAt, &i.LastUsedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if email != nil {
		i.Email = *email
	}
	return i, nil
}

// ListIdentifiersForUserByKind returns every identifier of a single kind
// bound to the user. SIWS callers narrow to KindSolana to list the
// wallets attached to a provider; the kind argument keeps a single
// SELECT path for any future per-kind enumeration.
func (s *Store) ListIdentifiersForUserByKind(ctx context.Context, q Querier, userID uuid.UUID, kind IdentifierKind) ([]Identifier, error) {
	if q == nil {
		q = s.Pool
	}
	rows, err := q.Query(ctx, `
		SELECT id, user_id, kind, subject, COALESCE(email, ''), verified, hosted_domain, created_at, last_used_at
		  FROM identifiers
		 WHERE user_id = $1 AND kind = $2
		 ORDER BY created_at`, userID, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Identifier
	for rows.Next() {
		var i Identifier
		if err := rows.Scan(&i.ID, &i.UserID, &i.Kind, &i.Subject, &i.Email, &i.Verified, &i.HostedDomain, &i.CreatedAt, &i.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// DeleteIdentifier removes a single identifier by primary key. Used by
// SIWS Unbind. Returns ErrNotFound when the row does not exist (caller
// converts to a 404 / "not bound").
func (s *Store) DeleteIdentifier(ctx context.Context, q Querier, id uuid.UUID) error {
	if q == nil {
		q = s.Pool
	}
	tag, err := q.Exec(ctx, `DELETE FROM identifiers WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ReassignIdentifiers moves every identifier from fromUser → toUser. Used
// during merge so the source user can be soft-deleted afterwards.
func (s *Store) ReassignIdentifiers(ctx context.Context, q Querier, fromUser, toUser uuid.UUID) error {
	if q == nil {
		q = s.Pool
	}
	_, err := q.Exec(ctx, `UPDATE identifiers SET user_id = $1 WHERE user_id = $2`, toUser, fromUser)
	return err
}

// --- sessions -------------------------------------------------------------

// CreateSession inserts a new session row.
func (s *Store) CreateSession(ctx context.Context, q Querier, sess *Session) error {
	if q == nil {
		q = s.Pool
	}
	var ip any
	if sess.IP != nil {
		ip = sess.IP.String()
	}
	row := q.QueryRow(ctx, `
		INSERT INTO sessions (user_id, refresh_token_hash, ip, user_agent, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, last_used_at`,
		sess.UserID, sess.RefreshTokenHash, ip, sess.UserAgent, sess.ExpiresAt)
	return row.Scan(&sess.ID, &sess.CreatedAt, &sess.LastUsedAt)
}

// FindSessionByRefreshHash returns the live (non-revoked, not-expired)
// session matching the supplied hash. Used by RefreshToken.
func (s *Store) FindSessionByRefreshHash(ctx context.Context, q Querier, hash string) (*Session, error) {
	if q == nil {
		q = s.Pool
	}
	sess := &Session{}
	var ipStr *string
	err := q.QueryRow(ctx, `
		SELECT id, user_id, refresh_token_hash, ip::text, user_agent, created_at, last_used_at,
		       expires_at, revoked_at, step_up_until
		  FROM sessions
		 WHERE refresh_token_hash = $1
		   AND revoked_at IS NULL
		   AND expires_at > now()`, hash).
		Scan(&sess.ID, &sess.UserID, &sess.RefreshTokenHash, &ipStr, &sess.UserAgent,
			&sess.CreatedAt, &sess.LastUsedAt, &sess.ExpiresAt, &sess.RevokedAt, &sess.StepUpUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if ipStr != nil {
		sess.IP = net.ParseIP(*ipStr)
	}
	return sess, nil
}

// RotateSession replaces the refresh-token hash for an existing session,
// stamps last_used_at, and extends the expiry. Atomic so a concurrent
// refresh cannot mint two valid tokens from the same row.
func (s *Store) RotateSession(ctx context.Context, q Querier, id uuid.UUID, newHash string, newExpiresAt time.Time) error {
	if q == nil {
		q = s.Pool
	}
	tag, err := q.Exec(ctx, `
		UPDATE sessions
		   SET refresh_token_hash = $2,
		       last_used_at = now(),
		       expires_at = $3
		 WHERE id = $1
		   AND revoked_at IS NULL`, id, newHash, newExpiresAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeSession soft-revokes a session (sets revoked_at = now()).
func (s *Store) RevokeSession(ctx context.Context, q Querier, id uuid.UUID) error {
	if q == nil {
		q = s.Pool
	}
	tag, err := q.Exec(ctx, `
		UPDATE sessions SET revoked_at = now()
		 WHERE id = $1 AND revoked_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListSessionsForUser returns every live session for the user.
func (s *Store) ListSessionsForUser(ctx context.Context, q Querier, userID uuid.UUID) ([]Session, error) {
	if q == nil {
		q = s.Pool
	}
	rows, err := q.Query(ctx, `
		SELECT id, user_id, refresh_token_hash, ip::text, user_agent,
		       created_at, last_used_at, expires_at, revoked_at, step_up_until
		  FROM sessions
		 WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > now()
		 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var sess Session
		var ipStr *string
		if err := rows.Scan(&sess.ID, &sess.UserID, &sess.RefreshTokenHash, &ipStr, &sess.UserAgent,
			&sess.CreatedAt, &sess.LastUsedAt, &sess.ExpiresAt, &sess.RevokedAt, &sess.StepUpUntil); err != nil {
			return nil, err
		}
		if ipStr != nil {
			sess.IP = net.ParseIP(*ipStr)
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// MarkSessionStepUp flips step_up_until = now() + ttl for the supplied
// session id.
func (s *Store) MarkSessionStepUp(ctx context.Context, q Querier, id uuid.UUID, until time.Time) error {
	if q == nil {
		q = s.Pool
	}
	tag, err := q.Exec(ctx, `
		UPDATE sessions SET step_up_until = $2, last_used_at = now()
		 WHERE id = $1 AND revoked_at IS NULL`, id, until)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// PurgeExpiredSessions deletes rows whose expires_at is older than the
// supplied cutoff. Returns the count purged.
func (s *Store) PurgeExpiredSessions(ctx context.Context, q Querier, before time.Time) (int64, error) {
	if q == nil {
		q = s.Pool
	}
	tag, err := q.Exec(ctx, `DELETE FROM sessions WHERE expires_at < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// --- magic-link tokens ----------------------------------------------------

// CreateMagicLinkToken inserts a new outstanding token.
func (s *Store) CreateMagicLinkToken(ctx context.Context, q Querier, m *MagicLinkToken) error {
	if q == nil {
		q = s.Pool
	}
	var uid any
	if m.UserID != nil {
		uid = *m.UserID
	}
	row := q.QueryRow(ctx, `
		INSERT INTO magic_link_tokens (token_hash, email, intent, user_id, return_to, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at`,
		m.TokenHash, m.Email, m.Intent, uid, m.ReturnTo, m.ExpiresAt)
	return row.Scan(&m.CreatedAt)
}

// ConsumeMagicLinkToken atomically marks the token used and returns it.
// Returns ErrNotFound when the token is missing, already used, or expired.
func (s *Store) ConsumeMagicLinkToken(ctx context.Context, q Querier, hash string) (*MagicLinkToken, error) {
	if q == nil {
		q = s.Pool
	}
	m := &MagicLinkToken{}
	var uid *uuid.UUID
	err := q.QueryRow(ctx, `
		UPDATE magic_link_tokens
		   SET used_at = now()
		 WHERE token_hash = $1
		   AND used_at IS NULL
		   AND expires_at > now()
		RETURNING token_hash, email, intent, user_id, return_to, created_at, expires_at, used_at`, hash).
		Scan(&m.TokenHash, &m.Email, &m.Intent, &uid, &m.ReturnTo, &m.CreatedAt, &m.ExpiresAt, &m.UsedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if uid != nil {
		m.UserID = uid
	}
	return m, nil
}

// CountRecentMagicLinkTokens counts tokens issued for the given email in
// the supplied window. Used by the per-email rate limiter.
func (s *Store) CountRecentMagicLinkTokens(ctx context.Context, q Querier, email string, since time.Time) (int, error) {
	if q == nil {
		q = s.Pool
	}
	var n int
	err := q.QueryRow(ctx, `
		SELECT count(*) FROM magic_link_tokens
		 WHERE email = $1 AND created_at >= $2`, email, since).Scan(&n)
	return n, err
}

// PurgeExpiredMagicLinkTokens deletes rows whose expires_at has passed.
func (s *Store) PurgeExpiredMagicLinkTokens(ctx context.Context, q Querier, before time.Time) (int64, error) {
	if q == nil {
		q = s.Pool
	}
	tag, err := q.Exec(ctx, `DELETE FROM magic_link_tokens WHERE expires_at < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// --- merge audit ----------------------------------------------------------

// InsertMergeAudit appends one audit row.
func (s *Store) InsertMergeAudit(ctx context.Context, q Querier, a *MergeAudit) error {
	if q == nil {
		q = s.Pool
	}
	var merged any
	if a.MergedUserID != nil {
		merged = *a.MergedUserID
	}
	row := q.QueryRow(ctx, `
		INSERT INTO merge_audit (primary_user_id, merged_user_id, reason, matched_email, matched_via)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, merged_at`,
		a.PrimaryUserID, merged, a.Reason, a.MatchedEmail, a.MatchedVia)
	return row.Scan(&a.ID, &a.MergedAt)
}
