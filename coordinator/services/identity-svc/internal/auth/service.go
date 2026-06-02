// Package auth is the brain of identity-svc: it stitches the Google OAuth
// flow + the magic-link flow + auto-merge + JWT issuance + session
// rotation into a single coherent API that the HTTP handlers call.
//
// All policy decisions (when to auto-merge, when to require step-up,
// how long tokens live) live here so handlers stay thin.
package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/mail"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/oauth/google"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/ratelimit"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/siws"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/store"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/tokens"
)

// Service orchestrates sign-in flows. Construct one per process; methods
// are safe for concurrent use.
type Service struct {
	Store   *store.Store
	Google  *google.Client
	Mail    mail.Sender
	Signer  *tokens.Signer
	Limiter ratelimit.Limiter
	Logger  *slog.Logger

	BaseURL            string
	AllowedReturnHosts map[string]struct{}

	MagicLinkTTL              time.Duration
	RefreshTokenTTL           time.Duration
	StepUpTTL                 time.Duration
	MagicLinkPerEmailPerHour  int
	MagicLinkPerIPPerHour     int

	// SiwsChallenges persists outstanding SIWS challenges with a 5-minute
	// TTL. Production wires the Redis-backed implementation; dev / tests
	// fall back to an in-memory store on first use.
	SiwsChallenges siws.ChallengeStore

	// Apple is the Sign-in-with-Apple identity-token validator. nil when
	// the deployment hasn't opted into Apple sign-in (tests / dev). The
	// CompleteAppleSignIn helper rejects with "not configured" rather
	// than panicking when nil.
	Apple *AppleValidator

	// AppleSubSalt is the per-deployment salt mixed into SHA-256 when
	// hashing the Apple `sub` claim for the `users.apple_sub_hash`
	// lookup column. Sourced from env APPLE_SUB_SALT at boot time.
	AppleSubSalt []byte
}

// Options bundles the constructor inputs.
type Options struct {
	Store              *store.Store
	Google             *google.Client
	Mail               mail.Sender
	Signer             *tokens.Signer
	Limiter            ratelimit.Limiter
	Logger             *slog.Logger
	BaseURL            string
	AllowedReturnHosts []string

	MagicLinkTTL             time.Duration
	RefreshTokenTTL          time.Duration
	StepUpTTL                time.Duration
	MagicLinkPerEmailPerHour int
	MagicLinkPerIPPerHour    int

	// Apple Sign-in collaborators. Apple may be nil in dev / unit tests
	// where the iOS path isn't exercised; CompleteAppleSignIn returns
	// "not configured" rather than panicking.
	Apple        *AppleValidator
	AppleSubSalt []byte
}

// New builds a Service from Options. Defaults are filled in for any zero
// duration so tests can omit them.
func New(o Options) *Service {
	if o.MagicLinkTTL == 0 {
		o.MagicLinkTTL = 10 * time.Minute
	}
	if o.RefreshTokenTTL == 0 {
		o.RefreshTokenTTL = 30 * 24 * time.Hour
	}
	if o.StepUpTTL == 0 {
		o.StepUpTTL = 5 * time.Minute
	}
	if o.MagicLinkPerEmailPerHour == 0 {
		o.MagicLinkPerEmailPerHour = 3
	}
	if o.MagicLinkPerIPPerHour == 0 {
		o.MagicLinkPerIPPerHour = 10
	}
	hosts := make(map[string]struct{}, len(o.AllowedReturnHosts))
	for _, h := range o.AllowedReturnHosts {
		hosts[strings.ToLower(strings.TrimSpace(h))] = struct{}{}
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return &Service{
		Store:                    o.Store,
		Google:                   o.Google,
		Mail:                     o.Mail,
		Signer:                   o.Signer,
		Limiter:                  o.Limiter,
		Logger:                   o.Logger,
		BaseURL:                  o.BaseURL,
		AllowedReturnHosts:       hosts,
		MagicLinkTTL:             o.MagicLinkTTL,
		RefreshTokenTTL:          o.RefreshTokenTTL,
		StepUpTTL:                o.StepUpTTL,
		MagicLinkPerEmailPerHour: o.MagicLinkPerEmailPerHour,
		MagicLinkPerIPPerHour:    o.MagicLinkPerIPPerHour,
		Apple:                    o.Apple,
		AppleSubSalt:             o.AppleSubSalt,
	}
}

// --- shared types --------------------------------------------------------

// Bundle is the post-sign-in payload returned to the client. Mirrors the
// AuthBundle proto.
type Bundle struct {
	AccessToken           string
	AccessTokenExpiresAt  time.Time
	RefreshToken          string
	RefreshTokenExpiresAt time.Time
	User                  *store.User
	NewUser               bool
	Merged                bool
}

// --- Google flow --------------------------------------------------------

// StartGoogle returns the authorize URL + state token.
func (s *Service) StartGoogle(ctx context.Context, returnTo string) (string, string, error) {
	if err := s.checkReturnTo(returnTo); err != nil {
		return "", "", err
	}
	r, err := s.Google.Start(ctx, returnTo)
	if err != nil {
		return "", "", err
	}
	return r.AuthorizeURL, r.State, nil
}

// CompleteGoogle exchanges the code, fetches verified-secondaries, and
// mints a session (creating a User or auto-merging as needed).
func (s *Service) CompleteGoogle(ctx context.Context, code, state string, req *http.Request) (*Bundle, error) {
	id, err := s.Google.Complete(ctx, code, state)
	if err != nil {
		return nil, err
	}
	if !id.EmailVerified {
		return nil, errors.New("auth: Google email not verified")
	}
	var bundle *Bundle
	err = s.Store.WithTx(ctx, func(tx pgx.Tx) error {
		// 1) Existing Google identifier?
		existing, err := s.Store.FindIdentifierBySubject(ctx, tx, store.KindGoogle, id.Subject)
		if err == nil {
			// Known user — touch + mint bundle.
			//
			// Propagate the TouchIdentifier error: under Serializable
			// isolation a SQLSTATE 40001 here would otherwise silently
			// abort the rest of the transaction (every later statement
			// would then surface as 25P02 "current transaction is
			// aborted"). Bubbling lets WithTx see the 40001 and retry.
			// See #620 for the same fix applied to the Apple flow.
			if err := s.Store.TouchIdentifier(ctx, tx, existing.ID); err != nil {
				return fmt.Errorf("touch google identifier: %w", err)
			}
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
		// 2) Auto-merge: do any of Google's verified secondaries already
		//    have a magic-link identifier on file?
		mergeTarget, matchedEmail, err := s.findMergeCandidate(ctx, tx, id)
		if err != nil {
			return err
		}
		if mergeTarget != nil {
			// Attach the Google identifier to the existing user. Audit
			// + notify after commit (best-effort).
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
			s.notifyMergeAsync(req.Context(), mergeTarget.PrimaryEmail, id.Email, matchedEmail)
			bundle, err = s.issueBundleTx(ctx, tx, mergeTarget, req, false, true)
			return err
		}
		// 3) Fresh user.
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
		// Auto-mint a personal workspace + owner membership. We do it
		// inside the same tx so a partial sign-in never leaves a user
		// without a workspace (which would later 500 every workload
		// submission). Failure here aborts the sign-in.
		if _, err := s.Store.EnsurePersonalWorkspace(ctx, tx, u); err != nil {
			return fmt.Errorf("auth: ensure personal workspace: %w", err)
		}
		// Promote any pending invites for this verified email into
		// real memberships. Non-fatal: a failure here only affects
		// the workspace list for this user, not their sign-in.
		if _, err := s.Store.ConsumeInvitesForEmail(ctx, tx, id.Email, u.ID); err != nil {
			s.Logger.Warn("consume invites failed", slog.String("error", err.Error()))
		}
		bundle, err = s.issueBundleTx(ctx, tx, u, req, true, false)
		return err
	})
	if err != nil {
		return nil, err
	}
	return bundle, nil
}

// findMergeCandidate returns the user we should merge INTO based on
// Google's verified-secondaries list. Empty match → (nil, "", nil).
func (s *Service) findMergeCandidate(ctx context.Context, tx pgx.Tx, id *google.Identity) (*store.User, string, error) {
	for _, secondary := range id.VerifiedSecondaries {
		matches, err := s.Store.FindIdentifiersByEmail(ctx, tx, secondary)
		if err != nil {
			return nil, "", err
		}
		for _, m := range matches {
			if m.Kind != store.KindMagicLink {
				continue
			}
			if !m.Verified {
				continue
			}
			user, err := s.Store.GetUser(ctx, tx, m.UserID)
			if err != nil {
				return nil, "", err
			}
			if user.DeletedAt != nil {
				continue
			}
			return user, secondary, nil
		}
	}
	return nil, "", nil
}

// --- Magic-link flow ----------------------------------------------------

// MagicLinkResponse is returned to the requester. accepted is always true
// when we accepted the request (anti-enumeration); errors only fire on
// rate-limit / malformed input.
type MagicLinkResponse struct {
	Accepted  bool
	ExpiresIn time.Duration
}

// RequestMagicLink generates a fresh token, stores its hash, and sends
// the raw token via SMTP.
func (s *Service) RequestMagicLink(ctx context.Context, email, returnTo, sourceIP string, intent store.MagicLinkIntent) (MagicLinkResponse, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || !strings.Contains(email, "@") {
		return MagicLinkResponse{}, errors.New("auth: invalid email")
	}
	if err := s.checkReturnTo(returnTo); err != nil {
		return MagicLinkResponse{}, err
	}
	// Rate limit: per-email and per-IP, 1h fixed windows.
	if s.Limiter != nil {
		if ok, _, err := s.Limiter.Allow(ctx, "ml:email:"+email, s.MagicLinkPerEmailPerHour, time.Hour); err == nil && !ok {
			return MagicLinkResponse{}, fmt.Errorf("auth: too many requests for this email")
		}
		if sourceIP != "" {
			if ok, _, err := s.Limiter.Allow(ctx, "ml:ip:"+sourceIP, s.MagicLinkPerIPPerHour, time.Hour); err == nil && !ok {
				return MagicLinkResponse{}, fmt.Errorf("auth: too many requests from this IP")
			}
		}
	}
	rawToken, err := tokens.Random32()
	if err != nil {
		return MagicLinkResponse{}, err
	}
	hash := tokens.SHA256Hex(rawToken)
	expiresAt := time.Now().Add(s.MagicLinkTTL)
	if err := s.Store.CreateMagicLinkToken(ctx, nil, &store.MagicLinkToken{
		TokenHash: hash,
		Email:     email,
		Intent:    intent,
		ReturnTo:  returnTo,
		ExpiresAt: expiresAt,
	}); err != nil {
		return MagicLinkResponse{}, err
	}
	link := s.buildMagicLinkURL(rawToken, returnTo)
	subject, htmlBody, textBody, err := mail.RenderMagicLink(mail.LinkData{
		URL:       link,
		ExpiresIn: humanDuration(s.MagicLinkTTL),
		Intent:    string(intent),
	})
	if err != nil {
		return MagicLinkResponse{}, err
	}
	if err := s.Mail.Send(ctx, mail.Message{To: email, Subject: subject, HTMLBody: htmlBody, TextBody: textBody}); err != nil {
		s.Logger.Error("magic-link send failed", slog.String("email", email), slog.String("error", err.Error()))
		// We still return accepted=true to avoid leaking enumeration
		// signal; the user can retry after rate-limit window.
	}
	return MagicLinkResponse{Accepted: true, ExpiresIn: s.MagicLinkTTL}, nil
}

// CompleteMagicLink redeems the raw token, mints a session, and applies
// auto-merge in the magic-link → Google direction when applicable.
func (s *Service) CompleteMagicLink(ctx context.Context, rawToken string, req *http.Request) (*Bundle, error) {
	if rawToken == "" {
		return nil, errors.New("auth: missing token")
	}
	hash := tokens.SHA256Hex(rawToken)
	var bundle *Bundle
	err := s.Store.WithTx(ctx, func(tx pgx.Tx) error {
		m, err := s.Store.ConsumeMagicLinkToken(ctx, tx, hash)
		if err != nil {
			return fmt.Errorf("auth: token invalid or expired")
		}
		if m.Intent == store.IntentStepUp {
			return s.completeStepUpTx(ctx, tx, m)
		}
		// Magic-link sign-in: find existing magic-link identifier OR
		// the user whose Google identifier has this email as a
		// verified secondary (auto-merge direction 2).
		ident, mergeTarget, err := s.findMagicLinkUserOrMergeTarget(ctx, tx, m.Email)
		if err != nil {
			return err
		}
		switch {
		case ident != nil:
			user, err := s.Store.GetUser(ctx, tx, ident.UserID)
			if err != nil {
				return err
			}
			// Propagate the TouchIdentifier error: under Serializable
			// isolation a SQLSTATE 40001 here would otherwise silently
			// abort the rest of the transaction (every later statement
			// would then surface as 25P02 "current transaction is
			// aborted"). Bubbling lets WithTx see the 40001 and retry.
			// See #620 for the same fix applied to the Apple flow.
			if err := s.Store.TouchIdentifier(ctx, tx, ident.ID); err != nil {
				return fmt.Errorf("touch magic-link identifier: %w", err)
			}
			bundle, err = s.issueBundleTx(ctx, tx, user, req, false, false)
			return err
		case mergeTarget != nil:
			// Auto-merge: attach a magic-link identifier to the Google
			// user (rather than create a separate stub User).
			newIdent := &store.Identifier{
				UserID:   mergeTarget.ID,
				Kind:     store.KindMagicLink,
				Subject:  "",
				Email:    m.Email,
				Verified: true,
			}
			if err := s.Store.CreateIdentifier(ctx, tx, newIdent); err != nil {
				if isUniqueViolation(err) {
					// Could race with another magic-link redemption
					// that already attached this email. The 23505 has
					// already aborted this transaction at the Postgres
					// level (any further statement on `tx` would
					// return SQLSTATE 25P02 "current transaction is
					// aborted"), so we cannot do an in-tx re-read
					// here — return store.ErrRetryTx and let
					// store.WithTx restart the whole flow. On retry
					// the fast path (findMagicLinkUserOrMergeTarget)
					// sees the winner's committed row. See #620.
					return fmt.Errorf("magic-link race lost on identifier insert: %w", store.ErrRetryTx)
				}
				return err
			}
			if err := s.Store.InsertMergeAudit(ctx, tx, &store.MergeAudit{
				PrimaryUserID: mergeTarget.ID,
				Reason:        "magic_link_email_matches_google_secondary",
				MatchedEmail:  m.Email,
				MatchedVia:    "google_verified_secondary",
			}); err != nil {
				return err
			}
			s.notifyMergeAsync(req.Context(), mergeTarget.PrimaryEmail, m.Email, m.Email)
			bundle, err = s.issueBundleTx(ctx, tx, mergeTarget, req, false, true)
			return err
		default:
			// Fresh magic-link user.
			u := &store.User{PrimaryEmail: m.Email}
			if err := s.Store.CreateUser(ctx, tx, u); err != nil {
				return err
			}
			newIdent := &store.Identifier{
				UserID:   u.ID,
				Kind:     store.KindMagicLink,
				Subject:  "",
				Email:    m.Email,
				Verified: true,
			}
			if err := s.Store.CreateIdentifier(ctx, tx, newIdent); err != nil {
				return err
			}
			// Auto-mint a personal workspace + owner membership. See
			// the Google flow for rationale.
			if _, err := s.Store.EnsurePersonalWorkspace(ctx, tx, u); err != nil {
				return fmt.Errorf("auth: ensure personal workspace: %w", err)
			}
			if _, err := s.Store.ConsumeInvitesForEmail(ctx, tx, m.Email, u.ID); err != nil {
				s.Logger.Warn("consume invites failed", slog.String("error", err.Error()))
			}
			bundle, err = s.issueBundleTx(ctx, tx, u, req, true, false)
			return err
		}
	})
	if err != nil {
		return nil, err
	}
	return bundle, nil
}

// findMagicLinkUserOrMergeTarget returns (existingMagicLinkIdent, nil) when
// a magic-link row for this email exists; (nil, googleUser) when a Google
// user lists this email as a verified secondary; (nil, nil) when neither.
func (s *Service) findMagicLinkUserOrMergeTarget(ctx context.Context, tx pgx.Tx, email string) (*store.Identifier, *store.User, error) {
	identifiers, err := s.Store.FindIdentifiersByEmail(ctx, tx, email)
	if err != nil {
		return nil, nil, err
	}
	// Prefer an exact magic-link match — that's the canonical re-auth path.
	for i := range identifiers {
		if identifiers[i].Kind == store.KindMagicLink && identifiers[i].Verified {
			return &identifiers[i], nil, nil
		}
	}
	// Magic-link → Google direction of auto-merge requires us to have
	// previously recorded the email as a Google verified-secondary. We
	// store secondaries by inserting *another* Google identifier row
	// with email=secondary, subject=<google sub>. That row's existence
	// IS the proof.
	for i := range identifiers {
		if identifiers[i].Kind == store.KindGoogle && identifiers[i].Verified {
			user, err := s.Store.GetUser(ctx, tx, identifiers[i].UserID)
			if err != nil {
				return nil, nil, err
			}
			if user.DeletedAt == nil {
				return nil, user, nil
			}
		}
	}
	return nil, nil, nil
}

// --- Session lifecycle --------------------------------------------------

// Refresh rotates the refresh token. Each call mints a fresh access +
// refresh pair; the old refresh token is invalidated on commit.
func (s *Service) Refresh(ctx context.Context, refreshToken string, req *http.Request) (*Bundle, error) {
	if refreshToken == "" {
		return nil, errors.New("auth: missing refresh token")
	}
	hash := tokens.SHA256Hex(refreshToken)
	var bundle *Bundle
	err := s.Store.WithTx(ctx, func(tx pgx.Tx) error {
		sess, err := s.Store.FindSessionByRefreshHash(ctx, tx, hash)
		if err != nil {
			return fmt.Errorf("auth: invalid refresh token")
		}
		user, err := s.Store.GetUser(ctx, tx, sess.UserID)
		if err != nil {
			return err
		}
		newToken, err := tokens.Random32()
		if err != nil {
			return err
		}
		newHash := tokens.SHA256Hex(newToken)
		newExpiry := time.Now().Add(s.RefreshTokenTTL)
		if err := s.Store.RotateSession(ctx, tx, sess.ID, newHash, newExpiry); err != nil {
			return err
		}
		identifiers, err := s.Store.ListIdentifiersForUser(ctx, tx, user.ID)
		if err != nil {
			return err
		}
		access, accessExp, err := s.Signer.IssueAccessToken(user.ID, sess.ID, user.PrimaryEmail,
			user.Roles, identifierKindsToStrings(identifiers),
			sess.StepUpUntil != nil && sess.StepUpUntil.After(time.Now()),
			solanaAddressesFrom(identifiers))
		if err != nil {
			return err
		}
		bundle = &Bundle{
			AccessToken:           access,
			AccessTokenExpiresAt:  accessExp,
			RefreshToken:          newToken,
			RefreshTokenExpiresAt: newExpiry,
			User:                  user,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return bundle, nil
}

// SignOut revokes the session attached to the refresh token.
func (s *Service) SignOut(ctx context.Context, refreshToken string) error {
	hash := tokens.SHA256Hex(refreshToken)
	sess, err := s.Store.FindSessionByRefreshHash(ctx, nil, hash)
	if err != nil {
		// Idempotent: unknown tokens just return OK.
		return nil
	}
	return s.Store.RevokeSession(ctx, nil, sess.ID)
}

// ListSessions returns every live session for the user.
func (s *Service) ListSessions(ctx context.Context, userID uuid.UUID) ([]store.Session, error) {
	return s.Store.ListSessionsForUser(ctx, nil, userID)
}

// RevokeSession revokes a single session id (must belong to the user).
func (s *Service) RevokeSession(ctx context.Context, userID, sessionID uuid.UUID) error {
	sessions, err := s.Store.ListSessionsForUser(ctx, nil, userID)
	if err != nil {
		return err
	}
	for _, sess := range sessions {
		if sess.ID == sessionID {
			return s.Store.RevokeSession(ctx, nil, sessionID)
		}
	}
	return errors.New("auth: session not found")
}

// --- Step-up ------------------------------------------------------------

// RequestStepUp sends step-up magic links to every primary email bound to
// the user.
func (s *Service) RequestStepUp(ctx context.Context, userID uuid.UUID, sourceIP string) error {
	user, err := s.Store.GetUser(ctx, nil, userID)
	if err != nil {
		return err
	}
	_, err = s.RequestMagicLink(ctx, user.PrimaryEmail, "", sourceIP, store.IntentStepUp)
	return err
}

// completeStepUpTx executes inside the WithTx of CompleteMagicLink when
// the token had intent=step_up. Returns an error to roll back if the
// session-bind cannot be persisted; we don't issue a fresh access token
// (the existing access token is still valid, only step_up_until changes).
func (s *Service) completeStepUpTx(ctx context.Context, tx pgx.Tx, m *store.MagicLinkToken) error {
	if m.UserID == nil {
		return errors.New("auth: step-up token missing user binding")
	}
	// Step-up applies to the user's most-recent live session. Real-world
	// UX should pass the session id explicitly, but the proto contract
	// only carries the token, so we fall back to "freshest session".
	sessions, err := s.Store.ListSessionsForUser(ctx, tx, *m.UserID)
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		return errors.New("auth: no live session to step-up")
	}
	until := time.Now().Add(s.StepUpTTL)
	return s.Store.MarkSessionStepUp(ctx, tx, sessions[0].ID, until)
}

// --- helpers ------------------------------------------------------------

// issueBundleTx creates a session row, signs an access token, and returns
// the bundle. Must be called inside the same tx as the user/identifier
// inserts so partial sign-ins never leak.
func (s *Service) issueBundleTx(ctx context.Context, tx pgx.Tx, user *store.User, req *http.Request, newUser, merged bool) (*Bundle, error) {
	if err := s.Store.UpdateLastLogin(ctx, tx, user.ID); err != nil {
		return nil, err
	}
	rawRefresh, err := tokens.Random32()
	if err != nil {
		return nil, err
	}
	refreshHash := tokens.SHA256Hex(rawRefresh)
	refreshExpiry := time.Now().Add(s.RefreshTokenTTL)
	ip, ua := requestMetadata(req)
	sess := &store.Session{
		UserID:           user.ID,
		RefreshTokenHash: refreshHash,
		IP:               ip,
		UserAgent:        ua,
		ExpiresAt:        refreshExpiry,
	}
	if err := s.Store.CreateSession(ctx, tx, sess); err != nil {
		return nil, err
	}
	identifiers, err := s.Store.ListIdentifiersForUser(ctx, tx, user.ID)
	if err != nil {
		return nil, err
	}
	access, accessExp, err := s.Signer.IssueAccessToken(user.ID, sess.ID, user.PrimaryEmail,
		user.Roles, identifierKindsToStrings(identifiers), false,
		solanaAddressesFrom(identifiers))
	if err != nil {
		return nil, err
	}
	return &Bundle{
		AccessToken:           access,
		AccessTokenExpiresAt:  accessExp,
		RefreshToken:          rawRefresh,
		RefreshTokenExpiresAt: refreshExpiry,
		User:                  user,
		NewUser:               newUser,
		Merged:                merged,
	}, nil
}

// notifyMergeAsync best-effort sends the "your accounts were merged"
// email to both the surviving primary and the just-merged secondary.
// Fire-and-forget: SMTP failure must never block the sign-in.
func (s *Service) notifyMergeAsync(parentCtx context.Context, primaryEmail, mergedEmail, matchedEmail string) {
	if s.Mail == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = parentCtx // retained for future tracing baggage extraction
		for _, addr := range dedup([]string{primaryEmail, mergedEmail}) {
			subject, htmlBody, textBody, err := mail.RenderMergeNotice(mail.MergeNoticeData{
				MatchedEmail: matchedEmail,
				PrimaryEmail: primaryEmail,
			})
			if err != nil {
				continue
			}
			if err := s.Mail.Send(ctx, mail.Message{To: addr, Subject: subject, HTMLBody: htmlBody, TextBody: textBody}); err != nil {
				s.Logger.Warn("merge notice send failed", slog.String("to", addr), slog.String("error", err.Error()))
			}
		}
	}()
}

// buildMagicLinkURL builds the public URL the user will click. We put the
// raw token in the path so server-side referrer-leakage risks are limited
// (browsers don't send query strings as Referer to off-origin links, but
// some user-agents log them in history).
func (s *Service) buildMagicLinkURL(token, returnTo string) string {
	u, _ := url.Parse(s.BaseURL)
	q := url.Values{"token": {token}}
	if returnTo != "" {
		q.Set("return_to", returnTo)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/v1/auth/magic-link/complete"
	u.RawQuery = q.Encode()
	return u.String()
}

// checkReturnTo defends against open-redirect by requiring return_to's
// host to be in the allowlist (or empty, which means "use default").
func (s *Service) checkReturnTo(returnTo string) error {
	if returnTo == "" {
		return nil
	}
	u, err := url.Parse(returnTo)
	if err != nil {
		return fmt.Errorf("auth: invalid return_to: %w", err)
	}
	if u.Host == "" {
		// Relative URL — safe.
		return nil
	}
	host := strings.ToLower(u.Hostname())
	if _, ok := s.AllowedReturnHosts[host]; !ok {
		return fmt.Errorf("auth: return_to host %q not allowed", host)
	}
	return nil
}

// requestMetadata extracts source IP + user-agent. Defensive against nil
// requests (CLI tests).
func requestMetadata(req *http.Request) (net.IP, string) {
	if req == nil {
		return nil, ""
	}
	ua := req.UserAgent()
	ipStr := req.Header.Get("X-Real-IP")
	if ipStr == "" {
		ipStr = strings.SplitN(req.RemoteAddr, ":", 2)[0]
	}
	ip := net.ParseIP(ipStr)
	return ip, ua
}

func identifierKindsToStrings(idents []store.Identifier) []string {
	out := make([]string, 0, len(idents))
	for _, i := range idents {
		out = append(out, string(i.Kind))
	}
	return out
}

// solanaAddressesFrom extracts the base58 addresses of every KindSolana
// identifier in the slice. Returns nil (not an empty slice) when none
// exist so the JWT claim is omitted by `omitempty` rather than encoded
// as an empty array — downstream JS / Go deserializers handle nil
// uniformly but `[]` triggers a "no wallet" branch in some clients.
func solanaAddressesFrom(idents []store.Identifier) []string {
	var out []string
	for _, i := range idents {
		if i.Kind == store.KindSolana && i.Subject != "" {
			out = append(out, i.Subject)
		}
	}
	return out
}

func dedup(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func humanDuration(d time.Duration) string {
	if d >= time.Hour {
		return fmt.Sprintf("%d hours", int(d/time.Hour))
	}
	if d >= time.Minute {
		return fmt.Sprintf("%d minutes", int(d/time.Minute))
	}
	return d.String()
}

// isUniqueViolation reports whether err is a Postgres unique constraint
// violation. Used to convert race losses into idempotent successes.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
