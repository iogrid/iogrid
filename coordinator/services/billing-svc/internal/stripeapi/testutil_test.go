package stripeapi

import (
	"testing"

	"github.com/google/uuid"
)

func uuidNew(t *testing.T) uuid.UUID {
	t.Helper()
	return uuid.New()
}
