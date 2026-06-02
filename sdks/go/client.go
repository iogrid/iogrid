package iogrid

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Version is the SDK version. Sent in the User-Agent header and exposed
// for callers that include it in their own telemetry.
const Version = "0.1.0"

// DefaultBaseURL is api.iogrid.org over HTTPS.
const DefaultBaseURL = "https://api.iogrid.org"

// HTTPClient is the minimal interface required by Client; *http.Client
// satisfies it. Tests can inject an in-process round-tripper.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Options configure a Client. APIKey is the only required field.
type Options struct {
	APIKey     string        // required
	BaseURL    string        // default DefaultBaseURL
	HTTPClient HTTPClient    // default http.DefaultClient + 30s timeout
	UserAgent  string        // appended to the SDK UA
	Timeout    time.Duration // applied to the default *http.Client only
}

// Client is the iogrid customer SDK client.
type Client struct {
	apiKey    string
	baseURL   string
	http      HTTPClient
	userAgent string
}

// NewClient validates options and constructs a Client.
func NewClient(opts Options) (*Client, error) {
	if opts.APIKey == "" {
		return nil, errors.New("iogrid: APIKey is required")
	}
	base := opts.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	base = strings.TrimRight(base, "/")
	httpClient := opts.HTTPClient
	if httpClient == nil {
		timeout := opts.Timeout
		if timeout == 0 {
			timeout = 30 * time.Second
		}
		httpClient = &http.Client{Timeout: timeout}
	}
	ua := "iogrid-sdk-go/" + Version
	if opts.UserAgent != "" {
		ua = ua + " (" + opts.UserAgent + ")"
	}
	return &Client{
		apiKey:    opts.APIKey,
		baseURL:   base,
		http:      httpClient,
		userAgent: ua,
	}, nil
}

// --- low-level transport ----------------------------------------------------

func (c *Client) doJSON(ctx context.Context, method, path string, body, out any, query url.Values) error {
	u := c.baseURL + path
	if len(query) > 0 {
		u = u + "?" + query.Encode()
	}

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("iogrid: marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
	if err != nil {
		return fmt.Errorf("iogrid: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("iogrid: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("iogrid: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var env ErrorEnvelope
		if jerr := json.Unmarshal(respBody, &env); jerr != nil || env.Code == "" {
			env = ErrorEnvelope{Code: ErrCodeInternal, Message: "HTTP " + strconv.Itoa(resp.StatusCode)}
		}
		return &Error{
			Status:    resp.StatusCode,
			Code:      env.Code,
			Message:   env.Message,
			FieldPath: env.FieldPath,
			Metadata:  env.Metadata,
			RequestID: env.RequestID,
		}
	}

	if out == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("iogrid: decode response: %w", err)
	}
	return nil
}

// --- Workloads --------------------------------------------------------------

// CreateWorkload submits a new workload to the grid.
func (c *Client) CreateWorkload(ctx context.Context, body CreateWorkloadRequest) (*Workload, error) {
	var out Workload
	if err := c.doJSON(ctx, http.MethodPost, "/v1/workloads", body, &out, nil); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetWorkload retrieves a workload by id (includes terminal result if finished).
func (c *Client) GetWorkload(ctx context.Context, id string) (*GetWorkloadResponse, error) {
	var out GetWorkloadResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/workloads/"+url.PathEscape(id), nil, &out, nil); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListWorkloads lists workloads in the caller's workspace.
func (c *Client) ListWorkloads(ctx context.Context, opts ListWorkloadsOptions) (*ListWorkloadsResponse, error) {
	q := url.Values{}
	if opts.PageSize > 0 {
		q.Set("pageSize", strconv.FormatInt(int64(opts.PageSize), 10))
	}
	if opts.PageToken != "" {
		q.Set("pageToken", opts.PageToken)
	}
	if opts.Type != "" {
		q.Set("type", string(opts.Type))
	}
	if opts.Status != "" {
		q.Set("status", opts.Status)
	}
	if !opts.SubmittedAfter.IsZero() {
		q.Set("submittedAfter", opts.SubmittedAfter.UTC().Format(time.RFC3339))
	}
	if !opts.SubmittedBefore.IsZero() {
		q.Set("submittedBefore", opts.SubmittedBefore.UTC().Format(time.RFC3339))
	}
	var out ListWorkloadsResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/workloads", nil, &out, q); err != nil {
		return nil, err
	}
	return &out, nil
}

// CancelWorkload cancels a queued or running workload.
func (c *Client) CancelWorkload(ctx context.Context, id, reason string) (*Workload, error) {
	q := url.Values{}
	if reason != "" {
		q.Set("reason", reason)
	}
	var out Workload
	if err := c.doJSON(ctx, http.MethodDelete, "/v1/workloads/"+url.PathEscape(id), nil, &out, q); err != nil {
		return nil, err
	}
	return &out, nil
}

// StreamWorkloadEvents opens an SSE stream of workload state transitions.
// Cancel ctx (or close `done`) to terminate the stream early; events
// channel is always closed when the underlying request completes.
//
// Errors during stream setup are returned from the function directly;
// errors during streaming are delivered on the returned errs channel.
func (c *Client) StreamWorkloadEvents(ctx context.Context, id string) (<-chan WorkloadEvent, <-chan error, error) {
	u := c.baseURL + "/v1/workloads/" + url.PathEscape(id) + "/events"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("iogrid: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("iogrid: do request: %w", err)
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var env ErrorEnvelope
		if jerr := json.Unmarshal(body, &env); jerr != nil || env.Code == "" {
			env = ErrorEnvelope{Code: ErrCodeInternal, Message: "HTTP " + strconv.Itoa(resp.StatusCode)}
		}
		return nil, nil, &Error{
			Status:    resp.StatusCode,
			Code:      env.Code,
			Message:   env.Message,
			FieldPath: env.FieldPath,
			Metadata:  env.Metadata,
			RequestID: env.RequestID,
		}
	}

	events := make(chan WorkloadEvent, 16)
	errs := make(chan error, 1)
	go func() {
		defer resp.Body.Close()
		defer close(events)
		defer close(errs)
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		var dataLines []string
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				if len(dataLines) > 0 {
					var ev WorkloadEvent
					if jerr := json.Unmarshal([]byte(strings.Join(dataLines, "\n")), &ev); jerr == nil {
						select {
						case events <- ev:
						case <-ctx.Done():
							errs <- ctx.Err()
							return
						}
					}
					dataLines = dataLines[:0]
				}
				continue
			}
			if strings.HasPrefix(line, ":") {
				continue // SSE comment
			}
			if strings.HasPrefix(line, "data:") {
				dataLines = append(dataLines, strings.TrimLeft(line[5:], " "))
			}
		}
		if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
			errs <- err
		}
	}()
	return events, errs, nil
}

// --- API keys ---------------------------------------------------------------

// CreateAPIKey mints a new API key. The secret is returned ONLY here —
// store it securely; subsequent list calls return only metadata.
func (c *Client) CreateAPIKey(ctx context.Context, body CreateAPIKeyRequest) (*CreatedAPIKey, error) {
	var out CreatedAPIKey
	if err := c.doJSON(ctx, http.MethodPost, "/v1/keys", body, &out, nil); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListAPIKeys lists API keys for the caller's workspace (metadata only).
func (c *Client) ListAPIKeys(ctx context.Context) ([]APIKeyMetadata, error) {
	var out ListAPIKeysResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/keys", nil, &out, nil); err != nil {
		return nil, err
	}
	return out.Keys, nil
}

// DeleteAPIKey revokes an API key.
func (c *Client) DeleteAPIKey(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/v1/keys/"+url.PathEscape(id), nil, nil, nil)
}

// --- Billing ----------------------------------------------------------------

// GetUsage returns one page of metered usage records.
func (c *Client) GetUsage(ctx context.Context, opts GetUsageOptions) ([]UsageRecord, error) {
	q := url.Values{}
	if opts.PageSize > 0 {
		q.Set("pageSize", strconv.FormatInt(int64(opts.PageSize), 10))
	}
	if opts.PageToken != "" {
		q.Set("pageToken", opts.PageToken)
	}
	if opts.Type != "" {
		q.Set("type", string(opts.Type))
	}
	if !opts.WindowStart.IsZero() {
		q.Set("windowStart", opts.WindowStart.UTC().Format(time.RFC3339))
	}
	if !opts.WindowEnd.IsZero() {
		q.Set("windowEnd", opts.WindowEnd.UTC().Format(time.RFC3339))
	}
	var out ListUsageResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/usage", nil, &out, q); err != nil {
		return nil, err
	}
	return out.Usage, nil
}

// --- Mobile VPN session bring-up --------------------------------------------

// RequestMobileSession opens a one-shot mobile-app VPN session via
// POST /v1/vpn/sessions/mobile. The response carries the full
// WireGuard peer config so the iOS/Android PacketTunnelProvider can
// call WireGuardAdapter.start without a second round-trip.
//
// Distinct from the legacy daemon-driven flow at POST
// /v1/vpn/sessions. On 503 the SDK returns an *Error with Status=503;
// the server's Retry-After hint defaults to 15s.
func (c *Client) RequestMobileSession(ctx context.Context, body RequestMobileSessionRequest) (*RequestMobileSessionResponse, error) {
	if body.CustomerID == "" {
		return nil, errors.New("iogrid: RequestMobileSession: CustomerID is required")
	}
	if body.ClientPublicKey == "" {
		return nil, errors.New("iogrid: RequestMobileSession: ClientPublicKey is required")
	}
	var out RequestMobileSessionResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/vpn/sessions/mobile", body, &out, nil); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetInvoices returns one page of invoices.
func (c *Client) GetInvoices(ctx context.Context, opts GetInvoicesOptions) ([]Invoice, error) {
	q := url.Values{}
	if opts.PageSize > 0 {
		q.Set("pageSize", strconv.FormatInt(int64(opts.PageSize), 10))
	}
	if opts.PageToken != "" {
		q.Set("pageToken", opts.PageToken)
	}
	var out ListInvoicesResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/invoices", nil, &out, q); err != nil {
		return nil, err
	}
	return out.Invoices, nil
}
