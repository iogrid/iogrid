package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/store"
)

// InternalGetUserWallet resolves a user's bound Solana/$GRID wallet for
// trusted service-to-service callers — specifically the build-gateway, which
// needs it to settle a finished iOS build's provider earnings in devnet
// $GRID (iogrid/iogrid#718). This is NOT bearer-authenticated; it is guarded
// by a shared internal token at the route layer (InternalAuth middleware),
// so it must only ever be mounted on the cluster-internal path.
//
//	GET /internal/v1/users/{userID}/wallet
//	200 { user_id, wallet_address, wallet_provider }
//	404 when the user has no wallet binding (caller treats as "skip settle")
func (h *AuthHandler) InternalGetUserWallet(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		http.Error(w, "store not configured", http.StatusServiceUnavailable)
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	b, err := h.Store.GetCustomerWalletBinding(r.Context(), nil, userID)
	if errors.Is(err, store.ErrNotFound) || b == nil {
		http.Error(w, "no wallet binding", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "wallet lookup failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"user_id":         userID.String(),
		"wallet_address":  b.WalletAddress,
		"wallet_provider": string(b.WalletProvider),
	})
}

// InternalAuth wraps a handler with a constant-time shared-token check. An
// empty configured token disables the route (503) rather than leaving it
// open — fail closed, since this exposes wallet bindings.
func InternalAuth(token string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if token == "" {
			http.Error(w, "internal API disabled", http.StatusServiceUnavailable)
			return
		}
		got := r.Header.Get("X-Internal-Token")
		// length-independent comparison; subtle-constant-time not critical
		// for a cluster-internal token but cheap to do right.
		if len(got) != len(token) || subtleConstEq(got, token) == false {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func subtleConstEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}
