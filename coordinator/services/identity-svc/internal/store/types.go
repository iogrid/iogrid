// Package store holds the typed Postgres access layer for identity-svc.
// Each type maps 1:1 to a row in the schema declared by db/migrations/.
// Handlers never reach for pgx directly — they call store methods so the
// boundary between persistence and HTTP / Connect-Go shifts in one place.
package store

import (
	"net"
	"time"

	"github.com/google/uuid"
)

// IdentifierKind matches the Postgres identifier_kind enum.
type IdentifierKind string

const (
	KindGoogle    IdentifierKind = "google"
	KindMagicLink IdentifierKind = "magic_link"
	KindApple     IdentifierKind = "apple"
	KindGitHub    IdentifierKind = "github"
)

// MagicLinkIntent matches the Postgres magic_link_intent enum. signin
// mints a fresh session; step_up upgrades an existing session for 5min;
// merge attaches the email to a different user.
type MagicLinkIntent string

const (
	IntentSignIn MagicLinkIntent = "signin"
	IntentStepUp MagicLinkIntent = "step_up"
	IntentMerge  MagicLinkIntent = "merge"
)

// User is the canonical account record.
type User struct {
	ID           uuid.UUID
	PrimaryEmail string
	DisplayName  string
	PictureURL   string
	Roles        []string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LastLoginAt  *time.Time
	DeletedAt    *time.Time
}

// Identifier is one row in identifiers; one User may have many.
type Identifier struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	Kind         IdentifierKind
	Subject      string
	Email        string
	Verified     bool
	HostedDomain string
	CreatedAt    time.Time
	LastUsedAt   time.Time
}

// Session is one server-side refresh-token record.
type Session struct {
	ID                uuid.UUID
	UserID            uuid.UUID
	RefreshTokenHash  string
	IP                net.IP
	UserAgent         string
	CreatedAt         time.Time
	LastUsedAt        time.Time
	ExpiresAt         time.Time
	RevokedAt         *time.Time
	StepUpUntil       *time.Time
}

// MagicLinkToken is an outstanding emailed link.
type MagicLinkToken struct {
	TokenHash string
	Email     string
	Intent    MagicLinkIntent
	UserID    *uuid.UUID
	ReturnTo  string
	CreatedAt time.Time
	ExpiresAt time.Time
	UsedAt    *time.Time
}

// MergeAudit records one auto-merge event.
type MergeAudit struct {
	ID            uuid.UUID
	PrimaryUserID uuid.UUID
	MergedUserID  *uuid.UUID
	Reason        string
	MatchedEmail  string
	MatchedVia    string
	MergedAt      time.Time
}
