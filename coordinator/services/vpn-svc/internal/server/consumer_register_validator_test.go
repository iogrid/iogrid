package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"

	billingv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1/billingv1connect"
	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
)

// scriptedValidator scripts inner-Validate outcomes per call.
type scriptedValidator struct {
	calls   int
	results []error
}

func (f *scriptedValidator) Validate(ctx context.Context, apiKey string) (string, string, string, error) {
	i := f.calls
	f.calls++
	if i >= len(f.results) {
		i = len(f.results) - 1
	}
	if e := f.results[i]; e != nil {
		return "", "", "", e
	}
	return "ws-1", "cust-1", "SUBSCRIPTION_TIER_UNSPECIFIED", nil
}

// stubApiKeyService implements the real Connect handler so the decorator
// speaks the actual protocol in tests.
type stubApiKeyService struct {
	billingv1connect.UnimplementedApiKeyServiceHandler
	fail bool
}

func (s *stubApiKeyService) RegisterConsumerAccount(ctx context.Context, req *connect.Request[billingv1.RegisterConsumerAccountRequest]) (*connect.Response[billingv1.RegisterConsumerAccountResponse], error) {
	if s.fail {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("stub down"))
	}
	return connect.NewResponse(&billingv1.RegisterConsumerAccountResponse{
		CustomerId: &commonv1.UUID{Value: "00000000-0000-4000-8000-000000000001"},
		Tier:       billingv1.SubscriptionTier_SUBSCRIPTION_TIER_UNSPECIFIED,
		Created:    true,
	}), nil
}

func registerStub(t *testing.T, fail bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	path, h := billingv1connect.NewApiKeyServiceHandler(&stubApiKeyService{fail: fail})
	mux.Handle(path, h)
	return httptest.NewServer(mux)
}

// Miss → register → re-validate success: the heal-on-touch path.
func TestConsumerRegistering_HealsOnFirstUse(t *testing.T) {
	srv := registerStub(t, false)
	defer srv.Close()
	inner := &scriptedValidator{results: []error{errors.New("invalid api key"), nil}}
	v := NewConsumerRegisteringValidator(inner, srv.URL, nil)

	ws, _, _, err := v.Validate(context.Background(), "1234567890123456")
	if err != nil {
		t.Fatalf("expected heal, got %v", err)
	}
	if ws != "ws-1" || inner.calls != 2 {
		t.Fatalf("expected re-validate after register (calls=%d ws=%q)", inner.calls, ws)
	}
}

// Non-consumer-shaped keys never trigger registration.
func TestConsumerRegistering_PassesThroughWorkspaceKeys(t *testing.T) {
	srv := registerStub(t, false)
	defer srv.Close()
	inner := &scriptedValidator{results: []error{errors.New("invalid api key")}}
	v := NewConsumerRegisteringValidator(inner, srv.URL, nil)

	_, _, _, err := v.Validate(context.Background(), "iog_abc123def456")
	if err == nil {
		t.Fatalf("expected the original auth error to stand")
	}
	if inner.calls != 1 {
		t.Fatalf("registration must not fire for non-consumer keys (calls=%d)", inner.calls)
	}
}

// Registration failure → fail closed with the ORIGINAL error.
func TestConsumerRegistering_FailsClosedWhenRegisterUnavailable(t *testing.T) {
	srv := registerStub(t, true)
	defer srv.Close()
	inner := &scriptedValidator{results: []error{errors.New("invalid api key")}}
	v := NewConsumerRegisteringValidator(inner, srv.URL, nil)

	_, _, _, err := v.Validate(context.Background(), "1234567890123456")
	if err == nil || err.Error() != "invalid api key" {
		t.Fatalf("expected the original fail-closed error, got %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("no re-validate after failed registration (calls=%d)", inner.calls)
	}
}
