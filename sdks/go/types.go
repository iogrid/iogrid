package iogrid

import "time"

// WorkloadType enumerates the kinds of work the grid can route to providers.
type WorkloadType string

const (
	WorkloadTypeBandwidth WorkloadType = "BANDWIDTH"
	WorkloadTypeDocker    WorkloadType = "DOCKER"
	WorkloadTypeGPU       WorkloadType = "GPU"
	WorkloadTypeIOSBuild  WorkloadType = "IOS_BUILD"
)

// WorkloadPriority hints scheduler urgency among queued jobs.
type WorkloadPriority string

const (
	WorkloadPriorityLow    WorkloadPriority = "LOW"
	WorkloadPriorityNormal WorkloadPriority = "NORMAL"
	WorkloadPriorityHigh   WorkloadPriority = "HIGH"
)

// Money is a fixed-precision monetary amount. Micros = millionths of the
// major currency unit. 12.34 USD == {Currency: "USD", Micros: 12_340_000}.
type Money struct {
	Currency string `json:"currency"`
	Micros   int64  `json:"micros"`
}

// BandwidthRequest is the customer payload for an HTTPS-proxy workload.
type BandwidthRequest struct {
	TargetURL       string `json:"targetUrl"`
	Method          string `json:"method,omitempty"`
	SessionID       string `json:"sessionId,omitempty"`
	PreferredRegion string `json:"preferredRegion,omitempty"`
	Category        string `json:"category,omitempty"`
	MaxSpend        *Money `json:"maxSpend,omitempty"`
}

// DockerRequest is the customer payload for an ephemeral container.
type DockerRequest struct {
	Image           string            `json:"image"`
	Command         []string          `json:"command,omitempty"`
	Env             map[string]string `json:"env,omitempty"`
	TimeoutSeconds  int64             `json:"timeoutSeconds,omitempty"`
	MinCPUCores     uint32            `json:"minCpuCores,omitempty"`
	MinMemoryMiB    uint64            `json:"minMemoryMib,omitempty"`
	MinGPUMemoryMiB uint64            `json:"minGpuMemoryMib,omitempty"`
}

// GPURequest is the customer payload for a compute-heavy GPU workload.
type GPURequest struct {
	Image          string            `json:"image"`
	Command        []string          `json:"command,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	TimeoutSeconds int64             `json:"timeoutSeconds,omitempty"`
	MinVRAMMiB     uint64            `json:"minVramMib,omitempty"`
	AllowedVendors []string          `json:"allowedVendors,omitempty"`
}

// IOSBuildRequest is the customer payload for a macOS / iOS CI job.
type IOSBuildRequest struct {
	SourceTarballS3Key string   `json:"sourceTarballS3Key"`
	TartImage          string   `json:"tartImage"`
	BuildCommands      []string `json:"buildCommands,omitempty"`
	ArtifactS3Bucket   string   `json:"artifactS3Bucket,omitempty"`
	ArtifactS3Prefix   string   `json:"artifactS3Prefix,omitempty"`
}

// CreateWorkloadRequest is the body for POST /v1/workloads. Exactly one
// of the typed payloads (Bandwidth/Docker/GPU/IOSBuild) MUST be set.
type CreateWorkloadRequest struct {
	Type      WorkloadType      `json:"type"`
	Priority  WorkloadPriority  `json:"priority,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	Bandwidth *BandwidthRequest `json:"bandwidth,omitempty"`
	Docker    *DockerRequest    `json:"docker,omitempty"`
	GPU       *GPURequest       `json:"gpu,omitempty"`
	IOSBuild  *IOSBuildRequest  `json:"iosBuild,omitempty"`
}

// Workload mirrors the server-side workload record.
type Workload struct {
	ID                 string            `json:"id"`
	WorkspaceID        string            `json:"workspaceId"`
	SubmittedByUserID  string            `json:"submittedByUserId,omitempty"`
	Type               WorkloadType      `json:"type"`
	Priority           WorkloadPriority  `json:"priority,omitempty"`
	Status             string            `json:"status"`
	SubmittedAt        time.Time         `json:"submittedAt,omitempty"`
	StartedAt          *time.Time        `json:"startedAt,omitempty"`
	FinishedAt         *time.Time        `json:"finishedAt,omitempty"`
	Labels             map[string]string `json:"labels,omitempty"`
	Bandwidth          *BandwidthRequest `json:"bandwidth,omitempty"`
	Docker             *DockerRequest    `json:"docker,omitempty"`
	GPU                *GPURequest       `json:"gpu,omitempty"`
	IOSBuild           *IOSBuildRequest  `json:"iosBuild,omitempty"`
}

// WorkloadResult is the terminal-state record.
type WorkloadResult struct {
	WorkloadID      string     `json:"workloadId"`
	TerminalStatus  string     `json:"terminalStatus"`
	ExitCode        int32      `json:"exitCode,omitempty"`
	LogsS3Key       string     `json:"logsS3Key,omitempty"`
	BytesIn         uint64     `json:"bytesIn,omitempty"`
	BytesOut        uint64     `json:"bytesOut,omitempty"`
	ArtifactS3Keys  []string   `json:"artifactS3Keys,omitempty"`
	Cost            *Money     `json:"cost,omitempty"`
	CompletedAt     *time.Time `json:"completedAt,omitempty"`
}

// GetWorkloadResponse wraps the workload + terminal result.
type GetWorkloadResponse struct {
	Workload Workload        `json:"workload"`
	Result   *WorkloadResult `json:"result,omitempty"`
}

// WorkloadEvent is one streamed state transition.
type WorkloadEvent struct {
	WorkloadID string    `json:"workloadId"`
	NewStatus  string    `json:"newStatus"`
	OccurredAt time.Time `json:"occurredAt"`
	Note       string    `json:"note,omitempty"`
}

// ListWorkloadsOptions narrows the listing.
type ListWorkloadsOptions struct {
	PageSize         int32
	PageToken        string
	Type             WorkloadType
	Status           string
	SubmittedAfter   time.Time
	SubmittedBefore  time.Time
}

// ListWorkloadsResponse is the paged listing envelope.
type ListWorkloadsResponse struct {
	Workloads     []Workload `json:"workloads"`
	NextPageToken string     `json:"nextPageToken,omitempty"`
}

// CreateAPIKeyRequest is the body for POST /v1/keys.
type CreateAPIKeyRequest struct {
	Name      string     `json:"name"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	Scopes    []string   `json:"scopes,omitempty"`
}

// APIKeyMetadata is the server-side description of an API key (sans secret).
type APIKeyMetadata struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	Scopes     []string   `json:"scopes,omitempty"`
}

// CreatedAPIKey extends APIKeyMetadata with the one-time-only secret.
type CreatedAPIKey struct {
	APIKeyMetadata
	Secret string `json:"secret"`
}

// ListAPIKeysResponse is the envelope for GET /v1/keys.
type ListAPIKeysResponse struct {
	Keys          []APIKeyMetadata `json:"keys"`
	NextPageToken string           `json:"nextPageToken,omitempty"`
}

// UsageRecord is one metered line item.
type UsageRecord struct {
	ID         string       `json:"id"`
	WorkloadID string       `json:"workloadId"`
	Type       WorkloadType `json:"type"`
	Quantity   uint64       `json:"quantity"`
	Cost       Money        `json:"cost"`
	RecordedAt time.Time    `json:"recordedAt"`
}

// GetUsageOptions narrows the usage list.
type GetUsageOptions struct {
	PageSize    int32
	PageToken   string
	Type        WorkloadType
	WindowStart time.Time
	WindowEnd   time.Time
}

// ListUsageResponse is the envelope for GET /v1/usage.
type ListUsageResponse struct {
	Usage         []UsageRecord `json:"usage"`
	NextPageToken string        `json:"nextPageToken,omitempty"`
	PageSubtotal  *Money        `json:"pageSubtotal,omitempty"`
}

// Invoice is the period-end aggregate record.
type Invoice struct {
	ID               string     `json:"id"`
	PeriodStart      *time.Time `json:"periodStart,omitempty"`
	PeriodEnd        *time.Time `json:"periodEnd,omitempty"`
	Subtotal         *Money     `json:"subtotal,omitempty"`
	Tax              *Money     `json:"tax,omitempty"`
	Total            Money      `json:"total"`
	Status           string     `json:"status"`
	IssuedAt         *time.Time `json:"issuedAt,omitempty"`
	PaidAt           *time.Time `json:"paidAt,omitempty"`
	HostedInvoiceURL string     `json:"hostedInvoiceUrl,omitempty"`
}

// GetInvoicesOptions narrows the invoice list.
type GetInvoicesOptions struct {
	PageSize  int32
	PageToken string
}

// ListInvoicesResponse is the envelope for GET /v1/invoices.
type ListInvoicesResponse struct {
	Invoices      []Invoice `json:"invoices"`
	NextPageToken string    `json:"nextPageToken,omitempty"`
}

// QuotaState mirrors iogrid.vpn.v1.QuotaState — the quota-gate signal
// echoed on every mobile VPN response so the iOS/Android app can
// render the "you're at X%" banner without a separate fetch.
type QuotaState string

const (
	QuotaStateUnspecified QuotaState = "QUOTA_STATE_UNSPECIFIED"
	QuotaStateHealthy     QuotaState = "QUOTA_STATE_HEALTHY"
	QuotaStateThrottled   QuotaState = "QUOTA_STATE_THROTTLED"
	QuotaStateExhausted   QuotaState = "QUOTA_STATE_EXHAUSTED"
)

// RequestMobileSessionRequest is the body for POST
// /v1/vpn/sessions/mobile. The mobile-app one-shot bring-up endpoint
// returns the full WireGuard peer config so PacketTunnelProvider can
// call WireGuardAdapter.start without a second round-trip.
//
// NOTE: the VPN surface uses snake_case on the wire (distinct from the
// workload / billing surfaces which use camelCase). The struct tags
// below match vpn-svc's handler verbatim.
type RequestMobileSessionRequest struct {
	CustomerID      string `json:"customer_id"`
	Region          string `json:"region,omitempty"`
	ClientPublicKey string `json:"client_public_key"`
	APIKey          string `json:"api_key,omitempty"`
	// PaymentAuthorization is opaque to the SDK — Track 5 owns
	// validation. Pass any JSON-marshalable value (struct, map,
	// json.RawMessage).
	PaymentAuthorization any `json:"payment_authorization,omitempty"`
}

// RequestMobileSessionResponse is the body returned by POST
// /v1/vpn/sessions/mobile (snake_case wire).
type RequestMobileSessionResponse struct {
	SessionID         string     `json:"session_id"`
	PeerPublicKey     string     `json:"peer_public_key"`
	PeerEndpoint      string     `json:"peer_endpoint"`
	CustomerInnerCIDR string     `json:"customer_inner_cidr"`
	AllowedIPs        string     `json:"allowed_ips"`
	DNSServers        []string   `json:"dns_servers"`
	Region            string     `json:"region"`
	ExpiresAt         time.Time  `json:"expires_at"`
	QuotaState        QuotaState `json:"quota_state"`
}

// ErrorEnvelope is the JSON body returned by the iogrid API on non-2xx
// responses. Decoded automatically by the SDK and surfaced as *Error.
type ErrorEnvelope struct {
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	FieldPath string            `json:"fieldPath,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	RequestID string            `json:"requestId,omitempty"`
}
