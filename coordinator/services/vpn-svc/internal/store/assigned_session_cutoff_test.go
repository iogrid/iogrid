package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// #730: a session the daemon couldn't bind within AssignedSessionMaxAge is
// an abandoned connect attempt — ListAssignedSessions must stop returning
// it, or the binder polls it forever (observed live: 9 day-old zombies
// polled every 5s). Fresh unbound sessions must still be returned.
func TestMemory_ListAssignedSessions_ExcludesStaleBringUps(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	provider := uuid.New()

	fresh := &Session{
		ID:              uuid.New(),
		CustomerID:      uuid.New(),
		Region:          "us-east-1",
		PrimaryProvider: provider,
		CurrentProvider: provider,
		CreatedAt:       time.Now().Add(-1 * time.Minute),
	}
	stale := &Session{
		ID:              uuid.New(),
		CustomerID:      uuid.New(),
		Region:          "us-east-1",
		PrimaryProvider: provider,
		CurrentProvider: provider,
		CreatedAt:       time.Now().Add(-AssignedSessionMaxAge - time.Minute),
	}
	for _, s := range []*Session{fresh, stale} {
		if err := m.CreateSession(ctx, s); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
	}

	assigned, err := m.ListAssignedSessions(ctx, provider)
	if err != nil {
		t.Fatalf("ListAssignedSessions: %v", err)
	}
	if len(assigned) != 1 {
		t.Fatalf("want exactly the fresh session, got %d sessions", len(assigned))
	}
	if assigned[0].ID != fresh.ID {
		t.Fatalf("want fresh session %s, got %s", fresh.ID, assigned[0].ID)
	}
}
