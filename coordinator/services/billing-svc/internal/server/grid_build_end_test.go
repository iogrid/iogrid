package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/grid"
)

// memBuildStore is an in-memory grid.BuildStore for the handler test.
type memBuildStore struct {
	rows map[string]*grid.BuildSettlement
}

func (m *memBuildStore) InsertBuildSettlement(_ context.Context, s *grid.BuildSettlement) error {
	m.rows[s.BuildID.String()+":"+s.AttemptID.String()] = s
	return nil
}
func (m *memBuildStore) GetBuildSettlement(_ context.Context, b, a uuid.UUID) (*grid.BuildSettlement, error) {
	return m.rows[b.String()+":"+a.String()], nil
}

func TestHandleBuildEnd(t *testing.T) {
	t.Run("BuildMeter disabled → 503", func(t *testing.T) {
		r := chi.NewRouter()
		mountGrid(r, &GridDeps{}) // BuildMeter nil
		srv := httptest.NewServer(r)
		defer srv.Close()
		resp, err := http.Post(srv.URL+"/v1/grid/build-end", "application/json", bytes.NewReader([]byte(`{}`)))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("want 503, got %d", resp.StatusCode)
		}
	})

	t.Run("valid build-end → 200 + 85%% provider split", func(t *testing.T) {
		r := chi.NewRouter()
		meter := &grid.BuildMeter{St: &memBuildStore{rows: map[string]*grid.BuildSettlement{}}}
		mountGrid(r, &GridDeps{BuildMeter: meter})
		srv := httptest.NewServer(r)
		defer srv.Close()

		body, _ := json.Marshal(grid.BuildInput{
			BuildID:        uuid.New(),
			AttemptID:      uuid.New(),
			CustomerWallet: "Cust1111111111111111111111111111111111111111",
			ConsumedAtomic: 40_000_000_000, // 40 GRID
			EscrowedAtomic: 100_000_000_000,
		})
		resp, err := http.Post(srv.URL+"/v1/grid/build-end", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("want 200, got %d", resp.StatusCode)
		}
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		if got := out["provider_share"]; got != float64(34_000_000_000) {
			t.Errorf("provider_share = %v, want 34e9 (85%% of 40 GRID)", got)
		}
	})

	t.Run("zero consumption → 200 settled=false", func(t *testing.T) {
		r := chi.NewRouter()
		meter := &grid.BuildMeter{St: &memBuildStore{rows: map[string]*grid.BuildSettlement{}}}
		mountGrid(r, &GridDeps{BuildMeter: meter})
		srv := httptest.NewServer(r)
		defer srv.Close()
		body, _ := json.Marshal(grid.BuildInput{
			BuildID: uuid.New(), AttemptID: uuid.New(),
			CustomerWallet: "w", ConsumedAtomic: 0,
		})
		resp, err := http.Post(srv.URL+"/v1/grid/build-end", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("want 200, got %d", resp.StatusCode)
		}
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		if out["settled"] != false {
			t.Errorf("settled = %v, want false", out["settled"])
		}
	})
}
