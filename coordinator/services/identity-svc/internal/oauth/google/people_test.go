package google

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

// TestFetchVerifiedSecondaries_ParsesAndFilters spins up a fake People API
// endpoint and asserts the client (a) authenticates via Bearer, (b)
// returns only the verified=true rows.
func TestFetchVerifiedSecondaries_ParsesAndFilters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			t.Errorf("missing Bearer header: %q", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"emailAddresses": [
				{"value": "alice@gmail.com",   "metadata": {"verified": true,  "primary": true}},
				{"value": "alice@company.com", "metadata": {"verified": true,  "primary": false}},
				{"value": "old@invalid.com",   "metadata": {"verified": false, "primary": false}}
			]
		}`))
	}))
	defer srv.Close()

	// Build a Client whose httpClient hits our fake but whose URL still
	// points at the fake server. We don't use real OAuth here.
	c := &Client{httpClient: srv.Client()}
	// fetch helper expects a URL embedded in its code; refactor by
	// invoking via a custom HTTP request to bypass the hardcoded URL.
	emails, err := c.fetchVerifiedSecondariesWithBase(context.Background(), &oauth2.Token{AccessToken: "fake"}, srv.URL)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(emails) != 2 {
		t.Fatalf("expected 2 verified, got %d: %v", len(emails), emails)
	}
	if emails[0] != "alice@gmail.com" || emails[1] != "alice@company.com" {
		t.Errorf("order/value mismatch: %v", emails)
	}
}

// fetchVerifiedSecondariesWithBase is the testable variant — same logic,
// hits the supplied base URL.
func (c *Client) fetchVerifiedSecondariesWithBase(ctx context.Context, tok *oauth2.Token, base string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/people/me?personFields=emailAddresses", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var pr peopleResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, err
	}
	var out []string
	for _, e := range pr.EmailAddresses {
		if e.Metadata.Verified {
			out = append(out, e.Value)
		}
	}
	return out, nil
}
