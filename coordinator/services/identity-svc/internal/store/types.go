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
	// KindSolana represents a Sign-In-With-Solana wallet binding; the
	// identifier's subject holds the base58-encoded ed25519 pubkey.
	KindSolana IdentifierKind = "solana"
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
	// PreferredLandingRole is the consumer-app persona the user picked
	// on /welcome (EPIC #422 / PR #445). nil = never picked; the web
	// auth middleware redirects to /welcome on the next sign-in. Set
	// values are validated against the Postgres enum
	// preferred_landing_role: 'provider' / 'customer' / 'vpn'. The
	// shared /account surface is not a valid value — every persona's
	// rail can reach it.
	PreferredLandingRole *string
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

// --- Workspace bounded context -------------------------------------------

// WorkspacePlan is the string form of the proto WorkspacePlan enum. We
// store as TEXT so the proto enum is the source of truth (adding a tier
// doesn't require a migration). Handlers validate user input against
// these constants.
type WorkspacePlan string

const (
	PlanFree       WorkspacePlan = "FREE"
	PlanStarter    WorkspacePlan = "STARTER"
	PlanGrowth     WorkspacePlan = "GROWTH"
	PlanEnterprise WorkspacePlan = "ENTERPRISE"
)

// Role is the per-membership role. Ordered by rank — higher rank can
// always do what a lower rank can (read implies nothing, OWNER implies
// ADMIN implies the rest).
type Role string

const (
	RoleOwner       Role = "OWNER"
	RoleAdmin       Role = "ADMIN"
	RoleBillingOnly Role = "BILLING_ONLY"
	RoleReadOnly    Role = "READ_ONLY"
)

// Workspace mirrors one row in workspaces.
type Workspace struct {
	ID                      uuid.UUID
	OwnerUserID             uuid.UUID
	Name                    string
	Plan                    WorkspacePlan
	BillingCustomerIDStripe string
	CreatedAt               time.Time
	UpdatedAt               time.Time
	DeletedAt               *time.Time
}

// WorkspaceMember mirrors one row in workspace_members.
type WorkspaceMember struct {
	WorkspaceID uuid.UUID
	UserID      uuid.UUID
	Role        Role
	JoinedAt    time.Time
}

// WorkspaceMemberDetail is a join of WorkspaceMember with the underlying
// User for the "Members" list in the management plane.
type WorkspaceMemberDetail struct {
	Member       WorkspaceMember
	PrimaryEmail string
	DisplayName  string
}

// WorkspaceInvite represents a pending invitation for an email that
// hasn't signed up yet. Consumed when the invitee redeems a magic-link.
type WorkspaceInvite struct {
	ID           uuid.UUID
	WorkspaceID  uuid.UUID
	InviteeEmail string
	Role         Role
	InvitedBy    uuid.UUID
	CreatedAt    time.Time
	ConsumedAt   *time.Time
}
