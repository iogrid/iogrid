// Tests for the QuotaState signal surfaced on POST /v1/vpn/sessions,
// GET /v1/vpn/sessions/{id}, and the refresh heartbeat (#573).
//
// Covers four states (OK, THROTTLED, EXHAUSTED, plus paid-tier
// short-circuit) at both the pure-function level (computeQuotaState)
// and the HTTP wire level.

package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	pb "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/vpn/v1"
	"github.com/iogrid/iogrid/coordinator/services/vpn-svc/internal/store"
)

func TestComputeQuotaState(t *testing.T) {
	cases := []struct {
		name string
		tier string
		used uint64
		want pb.QuotaState
	}{
		{
			name: "paid_starter_high_usage_is_ok",
			tier: "SUBSCRIPTION_TIER_STARTER",
			used: FreeTierQuotaBytes * 50,
			want: pb.QuotaState_QUOTA_STATE_OK,
		},
		{
			name: "paid_growth_zero_usage_is_ok",
			tier: "SUBSCRIPTION_TIER_GROWTH",
			used: 0,
			want: pb.QuotaState_QUOTA_STATE_OK,
		},
		{
			name: "paid_enterprise_overflow_is_ok",
			tier: "SUBSCRIPTION_TIER_ENTERPRISE",
			used: FreeTierQuotaBytes + 1,
			want: pb.QuotaState_QUOTA_STATE_OK,
		},
		{
			name: "free_zero_usage_is_ok",
			tier: "SUBSCRIPTION_TIER_FREE",
			used: 0,
			want: pb.QuotaState_QUOTA_STATE_OK,
		},
		{
			name: "free_under_80pct_is_ok",
			tier: "SUBSCRIPTION_TIER_FREE",
			used: freeTierThrottleBytes - 1,
			want: pb.QuotaState_QUOTA_STATE_OK,
		},
		{
			name: "free_at_80pct_is_throttled",
			tier: "SUBSCRIPTION_TIER_FREE",
			used: freeTierThrottleBytes,
			want: pb.QuotaState_QUOTA_STATE_THROTTLED,
		},
		{
			name: "free_at_99pct_is_throttled",
			tier: "SUBSCRIPTION_TIER_FREE",
			used: FreeTierQuotaBytes - 1,
			want: pb.QuotaState_QUOTA_STATE_THROTTLED,
		},
		{
			name: "free_at_100pct_is_exhausted",
			tier: "SUBSCRIPTION_TIER_FREE",
			used: FreeTierQuotaBytes,
			want: pb.QuotaState_QUOTA_STATE_EXHAUSTED,
		},
		{
			name: "free_overflow_is_exhausted",
			tier: "SUBSCRIPTION_TIER_FREE",
			used: FreeTierQuotaBytes * 3,
			want: pb.QuotaState_QUOTA_STATE_EXHAUSTED,
		},
		{
			name: "payg_treated_as_free_under_80pct_is_ok",
			tier: "SUBSCRIPTION_TIER_PAYG",
			used: freeTierThrottleBytes - 1,
			want: pb.QuotaState_QUOTA_STATE_OK,
		},
		{
			name: "payg_treated_as_free_at_80pct_is_throttled",
			tier: "SUBSCRIPTION_TIER_PAYG",
			used: freeTierThrottleBytes,
			want: pb.QuotaState_QUOTA_STATE_THROTTLED,
		},
		{
			name: "unspecified_tier_treated_as_free_at_exhausted",
			tier: "",
			used: FreeTierQuotaBytes,
			want: pb.QuotaState_QUOTA_STATE_EXHAUSTED,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeQuotaState(tc.tier, tc.used)
			if got != tc.want {
				t.Errorf("computeQuotaState(%q, %d) = %s, want %s",
					tc.tier, tc.used, got, tc.want)
			}
		})
	}
}

// TestIntegration_QuotaState_POSTOK verifies that a free-tier customer
// with no prior usage gets QUOTA_STATE_OK on session create.
func TestIntegration_QuotaState_POSTOK(t *testing.T) {
	custID := uuid.New()
	fv := &fakeValidator{
		valid: map[string]struct{ ws, cust, tier string }{
			"iog_freeOK": {uuid.New().String(), custID.String(), "SUBSCRIPTION_TIER_FREE"},
		},
	}
	srv, st := bootWithValidator(t, fv)
	st.(*store.Memory).SeedProvider(uuid.New(), "us-east-1", "healthy")

	resp, body := postJSON(t, srv.URL+"/v1/vpn/sessions", map[string]string{
		"customer_id": custID.String(), "region": "us-east-1", "api_key": "iog_freeOK",
	})
	if resp.StatusCode != 201 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	var got map[string]interface{}
	_ = json.Unmarshal(body, &got)
	if got["quota_state"] != pb.QuotaState_QUOTA_STATE_OK.String() {
		t.Errorf("quota_state = %v, want %s", got["quota_state"], pb.QuotaState_QUOTA_STATE_OK)
	}
}

// TestIntegration_QuotaState_POSTThrottled verifies that once a free-tier
// customer crosses 80% of FreeTierQuotaBytes, POST /v1/vpn/sessions
// succeeds (still under the hard cap) but reports QUOTA_STATE_THROTTLED.
func TestIntegration_QuotaState_POSTThrottled(t *testing.T) {
	custID := uuid.New()
	fv := &fakeValidator{
		valid: map[string]struct{ ws, cust, tier string }{
			"iog_freeT": {uuid.New().String(), custID.String(), "SUBSCRIPTION_TIER_FREE"},
		},
	}
	srv, st := bootWithValidator(t, fv)
	provID := uuid.New()
	st.(*store.Memory).SeedProvider(provID, "us-east-1", "healthy")

	// Seed prior usage at exactly the 80% threshold.
	_ = st.CreateSession(context.Background(), &store.Session{
		ID: uuid.New(), CustomerID: custID, Region: "us-east-1",
		PrimaryProvider: provID, CurrentProvider: provID,
		BytesIn: freeTierThrottleBytes, BytesOut: 0,
		CreatedAt: time.Now(), LastActivityAt: time.Now(),
	})

	resp, body := postJSON(t, srv.URL+"/v1/vpn/sessions", map[string]string{
		"customer_id": custID.String(), "region": "us-east-1", "api_key": "iog_freeT",
	})
	if resp.StatusCode != 201 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	var got map[string]interface{}
	_ = json.Unmarshal(body, &got)
	if got["quota_state"] != pb.QuotaState_QUOTA_STATE_THROTTLED.String() {
		t.Errorf("quota_state = %v, want %s", got["quota_state"], pb.QuotaState_QUOTA_STATE_THROTTLED)
	}
}

// TestIntegration_QuotaState_POSTExhausted429 verifies that a free-tier
// customer over the hard quota gets 429 AND quota_state=EXHAUSTED in the
// error body so the mobile app can drive the paywall.
func TestIntegration_QuotaState_POSTExhausted429(t *testing.T) {
	custID := uuid.New()
	fv := &fakeValidator{
		valid: map[string]struct{ ws, cust, tier string }{
			"iog_freeE": {uuid.New().String(), custID.String(), "SUBSCRIPTION_TIER_FREE"},
		},
	}
	srv, st := bootWithValidator(t, fv)
	provID := uuid.New()
	st.(*store.Memory).SeedProvider(provID, "us-east-1", "healthy")

	_ = st.CreateSession(context.Background(), &store.Session{
		ID: uuid.New(), CustomerID: custID, Region: "us-east-1",
		PrimaryProvider: provID, CurrentProvider: provID,
		BytesIn: FreeTierQuotaBytes + 1, BytesOut: 0,
		CreatedAt: time.Now(), LastActivityAt: time.Now(),
	})

	resp, body := postJSON(t, srv.URL+"/v1/vpn/sessions", map[string]string{
		"customer_id": custID.String(), "region": "us-east-1", "api_key": "iog_freeE",
	})
	if resp.StatusCode != 429 {
		t.Fatalf("status=%d body=%s, want 429", resp.StatusCode, body)
	}
	var got map[string]interface{}
	_ = json.Unmarshal(body, &got)
	if got["quota_state"] != pb.QuotaState_QUOTA_STATE_EXHAUSTED.String() {
		t.Errorf("quota_state in 429 body = %v, want %s",
			got["quota_state"], pb.QuotaState_QUOTA_STATE_EXHAUSTED)
	}
}

// TestIntegration_QuotaState_POSTPaidOK verifies paid-tier customers
// always get QUOTA_STATE_OK regardless of MTD bytes.
func TestIntegration_QuotaState_POSTPaidOK(t *testing.T) {
	custID := uuid.New()
	fv := &fakeValidator{
		valid: map[string]struct{ ws, cust, tier string }{
			"iog_paidOK": {uuid.New().String(), custID.String(), "SUBSCRIPTION_TIER_GROWTH"},
		},
	}
	srv, st := bootWithValidator(t, fv)
	provID := uuid.New()
	st.(*store.Memory).SeedProvider(provID, "us-east-1", "healthy")

	// Seed massive prior usage — paid tier should still be OK.
	_ = st.CreateSession(context.Background(), &store.Session{
		ID: uuid.New(), CustomerID: custID, Region: "us-east-1",
		PrimaryProvider: provID, CurrentProvider: provID,
		BytesIn: FreeTierQuotaBytes * 100, BytesOut: 0,
		CreatedAt: time.Now(), LastActivityAt: time.Now(),
	})

	resp, body := postJSON(t, srv.URL+"/v1/vpn/sessions", map[string]string{
		"customer_id": custID.String(), "region": "us-east-1", "api_key": "iog_paidOK",
	})
	if resp.StatusCode != 201 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	var got map[string]interface{}
	_ = json.Unmarshal(body, &got)
	if got["quota_state"] != pb.QuotaState_QUOTA_STATE_OK.String() {
		t.Errorf("quota_state = %v, want %s (paid tier always OK)",
			got["quota_state"], pb.QuotaState_QUOTA_STATE_OK)
	}
}

// TestIntegration_QuotaState_RefreshReflectsLiveUsage verifies the
// heartbeat endpoint reports quota_state computed against MTD bytes
// — the mobile app polls /refresh and uses this to flip banners.
func TestIntegration_QuotaState_RefreshReflectsLiveUsage(t *testing.T) {
	srv, st := boot(t)
	mem := st.(*store.Memory)
	provID := uuid.New()
	mem.SeedProvider(provID, "us-east-1", "healthy")

	custID := uuid.New()
	sessionID := uuid.New()
	_ = mem.CreateSession(context.Background(), &store.Session{
		ID:              sessionID,
		CustomerID:      custID,
		Region:          "us-east-1",
		PrimaryProvider: provID,
		CurrentProvider: provID,
		CreatedAt:       time.Now(),
		LastActivityAt:  time.Now(),
	})

	// Heartbeat reports MTD bytes well under the throttle threshold.
	resp, body := postJSON(t, srv.URL+"/v1/vpn/sessions/"+sessionID.String()+"/refresh",
		map[string]interface{}{
			"bytes_in":  uint64(1024),
			"bytes_out": uint64(1024),
		})
	if resp.StatusCode != 200 {
		t.Fatalf("refresh: status=%d body=%s", resp.StatusCode, body)
	}
	var got map[string]interface{}
	_ = json.Unmarshal(body, &got)
	if got["quota_state"] != pb.QuotaState_QUOTA_STATE_OK.String() {
		t.Errorf("early refresh quota_state = %v, want OK", got["quota_state"])
	}

	// Now bump MTD bytes past the throttle threshold; next heartbeat
	// should flip to THROTTLED.
	resp2, body2 := postJSON(t, srv.URL+"/v1/vpn/sessions/"+sessionID.String()+"/refresh",
		map[string]interface{}{
			"bytes_in":  uint64(freeTierThrottleBytes),
			"bytes_out": uint64(0),
		})
	if resp2.StatusCode != 200 {
		t.Fatalf("refresh #2: status=%d body=%s", resp2.StatusCode, body2)
	}
	var got2 map[string]interface{}
	_ = json.Unmarshal(body2, &got2)
	if got2["quota_state"] != pb.QuotaState_QUOTA_STATE_THROTTLED.String() {
		t.Errorf("post-bump refresh quota_state = %v, want THROTTLED", got2["quota_state"])
	}
}

// TestIntegration_QuotaState_GetSession verifies GET /v1/vpn/sessions/{id}
// includes quota_state for the mobile-app session read path.
func TestIntegration_QuotaState_GetSession(t *testing.T) {
	srv, st := boot(t)
	mem := st.(*store.Memory)
	provID := uuid.New()
	mem.SeedProvider(provID, "us-east-1", "healthy")

	custID := uuid.New()
	sessionID := uuid.New()
	_ = mem.CreateSession(context.Background(), &store.Session{
		ID:              sessionID,
		CustomerID:      custID,
		Region:          "us-east-1",
		PrimaryProvider: provID,
		CurrentProvider: provID,
		// Seed at exhausted level so this test exercises a non-OK state.
		BytesIn:        FreeTierQuotaBytes + 1,
		CreatedAt:      time.Now(),
		LastActivityAt: time.Now(),
	})

	resp, err := http.Get(srv.URL + "/v1/vpn/sessions/" + sessionID.String())
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, respBody)
	}
	var got map[string]interface{}
	_ = json.Unmarshal(respBody, &got)
	if got["quota_state"] != pb.QuotaState_QUOTA_STATE_EXHAUSTED.String() {
		t.Errorf("GET quota_state = %v, want EXHAUSTED", got["quota_state"])
	}
}
