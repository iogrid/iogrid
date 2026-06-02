// Package peer holds the vpn-svc peer-selection logic that drives the
// mobile PacketTunnelProvider session bring-up (#588).
//
// The picker is a thin layer over store.Store's existing
// SelectProviderForSession / SelectProviderAcrossRegions paths — it
// exists so the POST /v1/vpn/sessions handler can stay focused on
// request decoding + response shaping, with the "which provider goes
// where" decision encapsulated here for unit-testability and future
// extension (per-customer affinity hashing, paid-tier capacity
// reservation, etc.).
package peer

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/iogrid/iogrid/coordinator/services/vpn-svc/internal/store"
)

// ErrNoPeer is the canonical "503 with retry-after" sentinel returned
// when no healthy provider can be allocated for the requested region.
// Handlers should translate this into a 503 with a Retry-After header
// (per #588 DoD).
var ErrNoPeer = errors.New("no healthy peer available")

// Picker selects a peer (provider) for a new mobile VPN session.
type Picker struct {
	st store.Store
}

// NewPicker builds a Picker over the supplied store.
func NewPicker(st store.Store) *Picker {
	return &Picker{st: st}
}

// Pick chooses a peer in the requested region, OR geo-nearest if
// region == "auto" (per #588 DoD).
//
// Returns the provider UUID + the region the picker ultimately
// committed to. Returns ErrNoPeer if no healthy provider is
// available — callers should wrap that into a 503 response with a
// Retry-After header.
//
// clientIPHint is the originating client IP (from X-Forwarded-For)
// used by the cross-region path to bias toward geo-nearest. When
// empty (e.g. direct-from-tests), the picker degenerates to
// "least-loaded across all regions" — correct, just less locality-aware.
func (p *Picker) Pick(ctx context.Context, region, clientIPHint string) (uuid.UUID, string, error) {
	if region == "" || region == "auto" {
		id, chosenRegion, err := p.st.SelectProviderAcrossRegions(ctx, clientIPHint)
		if err != nil {
			return uuid.Nil, "", ErrNoPeer
		}
		return id, chosenRegion, nil
	}

	id, err := p.st.SelectProviderForSession(ctx, region)
	if err != nil {
		return uuid.Nil, "", ErrNoPeer
	}
	return id, region, nil
}

// HTTPStatusForPickError returns the HTTP status code a handler should
// surface for the given Pick error. Kept here so the handler doesn't
// duplicate the error-to-status mapping.
func HTTPStatusForPickError(err error) int {
	if errors.Is(err, ErrNoPeer) {
		return http.StatusServiceUnavailable
	}
	return http.StatusInternalServerError
}
