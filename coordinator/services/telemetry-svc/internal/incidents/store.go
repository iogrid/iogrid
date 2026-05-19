package incidents

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound is returned when a lookup matches zero rows.
var ErrNotFound = errors.New("incidents: not found")

// Store is the storage contract shared by [InMemory] and [Postgres].
//
// All methods accept a context — the Postgres impl propagates it to
// pgx; the in-memory impl honours cancellation between operations but
// is otherwise non-blocking.
//
// Time semantics: every "now" is UTC. Callers may pass time.Time{} for
// timestamps and the store fills in time.Now().UTC().
type Store interface {
	// ----- Incidents -----------------------------------------------

	// CreateIncident inserts a new incident + a first Update echoing
	// the initial status. Returns the persisted row with ID +
	// CreatedAt populated.
	CreateIncident(ctx context.Context, in CreateIncidentInput) (*Incident, error)
	// GetIncident fetches one incident by ID, including Updates.
	GetIncident(ctx context.Context, id uuid.UUID) (*Incident, error)
	// AppendUpdate adds an Update to an existing incident and updates
	// the incident's Status to in.Status. If in.Status == resolved,
	// also stamps resolved_at.
	AppendUpdate(ctx context.Context, id uuid.UUID, in UpdateIncidentInput) (*Update, error)
	// ListActive returns all incidents where resolved_at IS NULL,
	// newest first. Updates are hydrated.
	ListActive(ctx context.Context) ([]Incident, error)
	// ListRecent returns incidents with started_at within `since`
	// hours from now, regardless of resolved state, newest first.
	ListRecent(ctx context.Context, since time.Duration) ([]Incident, error)

	// ----- Subscriptions -------------------------------------------

	// UpsertSubscription registers an email for incident
	// notifications. Idempotent — re-subscribing an already-verified
	// email is a no-op that returns the existing row.
	UpsertSubscription(ctx context.Context, in SubscribeInput) (*Subscription, error)

	// ----- Uptime ledger -------------------------------------------

	// RecordSample upserts one day's sample. Idempotent on (service, day).
	RecordSample(ctx context.Context, s UptimeSample) error
	// UptimeForService returns the (service, day) samples for the
	// past `days` days, oldest first, with empty days filled in as
	// State="" so the frontend can render a fixed-grid heatmap.
	UptimeForService(ctx context.Context, service string, days int) ([]UptimeSample, error)
}

// nowOrUTC returns t in UTC, or time.Now().UTC() if t is zero.
func nowOrUTC(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC()
	}
	return t.UTC()
}
