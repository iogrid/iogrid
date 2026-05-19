package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func newTransparencyAPI() *API {
	return &API{
		Transparency: NewMemoryTransparencyStore(),
	}
}

func TestPublishAndGetTransparencyReport_RoundTrip(t *testing.T) {
	api := newTransparencyAPI()
	body := []byte(`{"year":2026,"quarter":1,"total_checks":42}`)

	// Publish
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transparency/publish", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	api.PublishTransparencyReport(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("publish status = %d, want %d. Body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}

	// Get
	r := chi.NewRouter()
	r.Get("/status/transparency/{year}/{quarter}", api.GetTransparencyReport)
	req2 := httptest.NewRequest(http.MethodGet, "/status/transparency/2026/1", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("get status = %d", w2.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(w2.Body.Bytes(), &got); err != nil {
		t.Fatalf("get body parse: %v", err)
	}
	if got["total_checks"].(float64) != 42 {
		t.Errorf("got total_checks = %v, want 42", got["total_checks"])
	}
}

func TestGetTransparencyReport_404OnMissing(t *testing.T) {
	api := newTransparencyAPI()
	r := chi.NewRouter()
	r.Get("/x/{year}/{quarter}", api.GetTransparencyReport)
	req := httptest.NewRequest(http.MethodGet, "/x/2025/4", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestListTransparencyReports_OrderedNewestFirst(t *testing.T) {
	api := newTransparencyAPI()
	for _, c := range []struct {
		year, quarter int
	}{
		{2025, 1}, {2026, 1}, {2025, 4}, {2026, 2},
	} {
		body, _ := json.Marshal(map[string]int{"year": c.year, "quarter": c.quarter})
		req := httptest.NewRequest(http.MethodPost, "/p", strings.NewReader(string(body)))
		w := httptest.NewRecorder()
		api.PublishTransparencyReport(w, req)
	}
	req := httptest.NewRequest(http.MethodGet, "/list", nil)
	w := httptest.NewRecorder()
	api.ListTransparencyReports(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var got struct {
		Reports []TransparencyIndex `json:"reports"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Reports) != 4 {
		t.Fatalf("reports len = %d, want 4", len(got.Reports))
	}
	// Newest first: 2026 Q2, 2026 Q1, 2025 Q4, 2025 Q1
	want := []TransparencyIndex{
		{Year: 2026, Quarter: 2},
		{Year: 2026, Quarter: 1},
		{Year: 2025, Quarter: 4},
		{Year: 2025, Quarter: 1},
	}
	for i, exp := range want {
		if got.Reports[i] != exp {
			t.Errorf("reports[%d] = %+v, want %+v", i, got.Reports[i], exp)
		}
	}
}

func TestPublish_BadJSON_400(t *testing.T) {
	api := newTransparencyAPI()
	req := httptest.NewRequest(http.MethodPost, "/p", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	api.PublishTransparencyReport(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestPublish_BadQuarter_400(t *testing.T) {
	api := newTransparencyAPI()
	req := httptest.NewRequest(http.MethodPost, "/p",
		strings.NewReader(`{"year":2026,"quarter":7}`))
	w := httptest.NewRecorder()
	api.PublishTransparencyReport(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestMissingStore_503(t *testing.T) {
	api := &API{}
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()
	api.GetTransparencyReport(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}
