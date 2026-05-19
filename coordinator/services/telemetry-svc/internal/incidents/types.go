package incidents

import (
	"time"

	"github.com/google/uuid"
)

// Impact is the operator's headline severity classification.
//
// The status page colour-codes the headline strip from the highest-impact
// active incident:
//
//	none     — grey  (informational)
//	minor    — amber (partial degradation, no SLO breach)
//	major    — orange (SLO breach for one service)
//	critical — red    (full outage / multiple services)
type Impact string

const (
	ImpactNone     Impact = "none"
	ImpactMinor    Impact = "minor"
	ImpactMajor    Impact = "major"
	ImpactCritical Impact = "critical"
)

// Severity returns a monotone integer for impact comparison. Higher =
// worse. Unknown values map to 0 so they never override a known one.
func (i Impact) Severity() int {
	switch i {
	case ImpactCritical:
		return 4
	case ImpactMajor:
		return 3
	case ImpactMinor:
		return 2
	case ImpactNone:
		return 1
	default:
		return 0
	}
}

// Valid reports whether i is one of the known enum values.
func (i Impact) Valid() bool {
	switch i {
	case ImpactNone, ImpactMinor, ImpactMajor, ImpactCritical:
		return true
	}
	return false
}

// Status follows the classic StatusPage.io lifecycle.
type Status string

const (
	StatusInvestigating Status = "investigating"
	StatusIdentified    Status = "identified"
	StatusMonitoring    Status = "monitoring"
	StatusResolved      Status = "resolved"
)

// Valid reports whether s is one of the known enum values.
func (s Status) Valid() bool {
	switch s {
	case StatusInvestigating, StatusIdentified, StatusMonitoring, StatusResolved:
		return true
	}
	return false
}

// Incident is the operator-visible incident record.
type Incident struct {
	ID                uuid.UUID `json:"id"`
	Title             string    `json:"title"`
	Body              string    `json:"body,omitempty"`
	Status            Status    `json:"status"`
	Impact            Impact    `json:"impact"`
	AffectedServices  []string  `json:"affected_services"`
	StartedAt         time.Time `json:"started_at"`
	ResolvedAt        *time.Time `json:"resolved_at,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	// Updates is the (optionally hydrated) timeline of status changes.
	// Populated by ListActive/ListRecent when the caller asks; nil on
	// bare reads.
	Updates []Update `json:"updates,omitempty"`
}

// Update is one chronological log entry on an Incident.
type Update struct {
	ID         uuid.UUID `json:"id"`
	IncidentID uuid.UUID `json:"incident_id"`
	Status     Status    `json:"status"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
}

// Subscription is one email subscriber.
type Subscription struct {
	ID             uuid.UUID `json:"id"`
	Email          string    `json:"email"`
	Verified       bool      `json:"verified"`
	VerifyToken    string    `json:"-"`
	ServicesFilter []string  `json:"services_filter,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	VerifiedAt     *time.Time `json:"verified_at,omitempty"`
	UnsubscribedAt *time.Time `json:"unsubscribed_at,omitempty"`
}

// UptimeSample is one (service, day) cell on the heatmap.
//
// State values:
//
//	"op"    — fully operational, all SLOs within budget
//	"deg"   — degraded, at least one SLO burning at >2x
//	"down"  — major outage, at least one SLO burning at >14x for >5m
//	"maint" — planned maintenance window (operator-set)
//	""      — no data (rendered as a faint placeholder)
type UptimeSample struct {
	Service string  `json:"service"`
	Day     string  `json:"day"` // YYYY-MM-DD, UTC
	State   string  `json:"state"`
	SLIPct  float64 `json:"sli_pct"`
}

// CreateIncidentInput is the request shape for POST /status/incidents.
type CreateIncidentInput struct {
	Title            string   `json:"title"`
	Body             string   `json:"body"`
	Status           Status   `json:"status"`
	Impact           Impact   `json:"impact"`
	AffectedServices []string `json:"affected_services"`
}

// UpdateIncidentInput is the request shape for POST /status/incidents/{id}/updates.
type UpdateIncidentInput struct {
	Status Status `json:"status"`
	Body   string `json:"body"`
}

// SubscribeInput is the request shape for POST /status/subscribe.
type SubscribeInput struct {
	Email          string   `json:"email"`
	ServicesFilter []string `json:"services_filter,omitempty"`
}
