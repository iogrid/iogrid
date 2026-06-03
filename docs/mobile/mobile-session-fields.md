# Adding a new mobile session field

This walkthrough ties together the 4 services + 4 SDKs + mobile app surfaces
that a new field on the mobile-session bring-up endpoint
(`POST /v1/vpn/sessions/mobile`) has to touch.

It uses **`inner_ip`** as the worked example, because it's a real shipped
end-to-end path:

- Wire format added to `proto/iogrid/vpn/v1/session.proto`
- Server-side allocator + storage shipped in PR **#605**
  (`feat(vpn-svc): mobile session handler wired into /v1/vpn/sessions/mobile`)
- Mobile decode wired in PR **#609** (Maestro flow 10)
- Customer SDKs surfaced in PR **#622**
  (`feat(sdks): requestMobileSession across TS/Python/Go/Java`)

If you skip a step the field will silently fall off the wire at the boundary
where it isn't projected — and you'll only find out when the mobile app
or a customer SDK consumer reports a missing value in prod. Follow all
seven steps in order.

> Mobile gotchas (Xcode 26 / Expo SDK 54 / WireGuardKitGo cgo) live in
> `mobile/ios/CONTRIBUTING.md`. This file is about the **server↔client
> contract for session fields**, not the mobile build.

## Why this order matters

**Protobuf is the single source of truth.** The `.proto` schema generates
Go stubs (`coordinator/internal/pb`), TypeScript stubs (`web/src/lib/pb`),
and is mirrored field-for-field in the customer SDKs. If you add a column
to Postgres before adding the proto field, your migration is unanchored —
the Go server can write it but nothing on the wire carries it, so the
mobile app and SDKs can't see it. Conversely if you add the field to the
TypeScript SDK first, callers compile against a wire shape the server
doesn't actually emit.

The canonical order is therefore:

1. **Proto** (wire contract)
2. **`make proto`** to regenerate Go + web stubs
3. **Coordinator service** — `store.Session` struct, `memory.go`, `postgres.go`, migration, `handlers.go`
4. **Mobile app** decode (`mobile/ios/src/lib/coordinator.ts`)
5. **Customer SDKs** (TypeScript, Python, Go, Java)
6. **Tests** alongside each layer
7. **Smoke** the deployed coordinator via the CI gate
   (`.github/workflows/sessions-mobile-smoke.yml`)

The data-plane mobile handler at
`coordinator/services/vpn-svc/internal/server/handlers.go::RequestMobileSession.Handle`
currently emits JSON directly (not protobuf-over-the-wire), so the proto
file acts as a **schema document** consumed by the SDK authors — even
though it's not yet serving as the literal wire format for this endpoint.
Keep it in sync: every JSON field on the response MUST have a proto
counterpart so the four-SDK migration in step 5 has a single reference.

---

## Step 1 — proto schema

Add the field to the `RequestMobileSession` request and/or response message
in `proto/iogrid/vpn/v1/session.proto`. If a suitable message doesn't
exist yet (the mobile-session response is currently surfaced as inline
JSON in the handler), define one — `inner_ip` lives implicitly on the
response shape today, so you'd add:

```proto
// proto/iogrid/vpn/v1/session.proto
message RequestMobileSessionResponse {
  string session_id = 1;
  string peer_public_key = 2;
  string peer_endpoint = 3;
  // Per-session tunnel-inner IPv4 address allocated atomically by vpn-svc
  // from the peer's /24 inside 10.66.0.0/16 (X = providerID[0] clamped
  // [2..253], Y = monotonic per-provider counter). Empty when the session
  // is in a non-mobile flow. Refs #605.
  string inner_ip = 4;
  string allowed_ips = 5;
  repeated string dns_servers = 6;
  string region = 7;
  string expires_at = 8;          // RFC3339
  QuotaState quota_state = 9;
}
```

Then regenerate:

```bash
make proto         # buf generate → coordinator/internal/pb + web/src/lib/pb
make proto-check   # parity check that CI will run on the PR
```

## Step 2 — `store.Session` struct

Add the in-memory representation in
`coordinator/services/vpn-svc/internal/store/store.go`. Group all
mobile-only fields under the `Mobile session fields` block so the next
contributor sees the convention:

```go
// coordinator/services/vpn-svc/internal/store/store.go (lines ~229-236)
// InnerIP is the per-session tunnel-inner IPv4 address the
// mobile client uses inside the VPN namespace. Allocated by
// vpn-svc atomically (AllocateInnerIP) at session-create time
// in the 10.66.X.Y/16 range where X derives from the provider
// UUID's first byte and Y comes from an atomic counter
// (vpn_provider_inner_ip_alloc table). Empty for non-mobile
// sessions.
InnerIP string
```

If the field needs an allocator (like `inner_ip`'s
provider-scoped counter), also extend the `Store` interface:

```go
// store.go (lines ~157-164)
// AllocateInnerIP atomically allocates a per-session tunnel-inner
// IPv4 address from the peer's /24. Idempotent on (providerID, sessionID).
AllocateInnerIP(ctx context.Context, providerID, sessionID uuid.UUID) (string, error)
```

## Step 3 — memory + postgres implementations

Both store backends must implement the new field/method so unit tests
(memory) and Postgres integration tests pass.

**Memory** (`coordinator/services/vpn-svc/internal/store/memory.go`):

```go
// memory.go — extend the Memory struct so the allocator has state
type Memory struct {
    // ...
    innerIPAlloc map[string]string         // "providerID:sessionID" → allocated IP
    innerIPNext  map[uuid.UUID]uint8       // provider_id → next Y counter (2..253)
}

func (m *Memory) AllocateInnerIP(ctx context.Context, providerID, sessionID uuid.UUID) (string, error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    key := providerID.String() + ":" + sessionID.String()
    if existing, ok := m.innerIPAlloc[key]; ok {
        return existing, nil    // idempotent retry
    }
    x := providerID[0]
    if x < 2  { x = 2   }
    if x > 253 { x = 253 }
    y := m.innerIPNext[providerID]
    if y < 2 { y = 2 }
    if y >= 254 {
        return "", fmt.Errorf("inner-IP space exhausted for provider %s", providerID)
    }
    ip := fmt.Sprintf("10.66.%d.%d", x, y)
    m.innerIPNext[providerID] = y + 1
    m.innerIPAlloc[key] = ip
    return ip, nil
}
```

**Postgres** (`coordinator/services/vpn-svc/internal/store/postgres.go`):
use the same atomicity guarantees the migration's UNIQUE constraint
relies on. Inner-IP allocation is `INSERT … ON CONFLICT DO UPDATE …
RETURNING` against the per-provider counter table:

```go
// postgres.go — atomic provider-suffix bump (lines ~733-772)
func (p *Postgres) AllocateInnerIP(ctx context.Context, providerID, sessionID uuid.UUID) (string, error) {
    // 1. Idempotency — return existing if already stamped on vpn_sessions row.
    var existing sql.NullString
    err := p.pool.QueryRow(ctx,
        `SELECT host(inner_ip) FROM vpn_sessions WHERE id = $1 AND inner_ip IS NOT NULL`,
        sessionID).Scan(&existing)
    if err == nil && existing.Valid && existing.String != "" {
        return existing.String, nil
    }
    // 2. Atomic suffix bump on vpn_provider_inner_ip_alloc.
    var nextY int
    if err := p.pool.QueryRow(ctx, `
        INSERT INTO vpn_provider_inner_ip_alloc (provider_id, next_suffix, updated_at)
        VALUES ($1, 2, NOW())
        ON CONFLICT (provider_id) DO UPDATE
        SET next_suffix = vpn_provider_inner_ip_alloc.next_suffix + 1,
            updated_at = NOW()
        RETURNING next_suffix
    `, providerID).Scan(&nextY); err != nil {
        return "", fmt.Errorf("inner-ip alloc: %w", err)
    }
    x := providerID[0]
    if x < 2  { x = 2   }
    if x > 253 { x = 253 }
    return fmt.Sprintf("10.66.%d.%d", x, nextY), nil
}
```

## Step 4 — goose migration

Add a numbered migration in
`coordinator/services/vpn-svc/internal/db/`. Use the next available
number (the inner-IP work landed as `00008_session_peer_config.sql`):

```sql
-- coordinator/services/vpn-svc/internal/db/00008_session_peer_config.sql
-- +goose Up
-- +goose StatementBegin
ALTER TABLE vpn_sessions
    ADD COLUMN client_public_key       VARCHAR(64),
    ADD COLUMN inner_ip                INET,
    ADD COLUMN expires_at              TIMESTAMPTZ,
    ADD COLUMN payment_authorization   JSONB;

CREATE UNIQUE INDEX idx_vpn_sessions_provider_inner_ip
    ON vpn_sessions(current_provider_id, inner_ip)
    WHERE inner_ip IS NOT NULL AND terminated_at IS NULL;

CREATE TABLE IF NOT EXISTS vpn_provider_inner_ip_alloc (
    provider_id  UUID PRIMARY KEY,
    next_suffix  INT  NOT NULL DEFAULT 1,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS vpn_provider_inner_ip_alloc;
DROP INDEX IF EXISTS idx_vpn_sessions_provider_inner_ip;
ALTER TABLE vpn_sessions
    DROP COLUMN IF EXISTS payment_authorization,
    DROP COLUMN IF EXISTS expires_at,
    DROP COLUMN IF EXISTS inner_ip,
    DROP COLUMN IF EXISTS client_public_key;
-- +goose StatementEnd
```

Migration hygiene checklist:

- Numbered prefix (`00008_`) must be unique and strictly monotonic
- Include a `Down` block that fully reverses the `Up` (drop indexes
  BEFORE the column they reference)
- For atomic-counter columns add the partial UNIQUE index that catches
  double-allocations as a DB-layer safety net even though the picker
  takes a row lock to serialise

## Step 5 — handlers.go

Wire the new field into the `RequestMobileSession.Handle` flow at
`coordinator/services/vpn-svc/internal/server/handlers.go`. Both the
allocator call AND the response JSON projection are required:

```go
// handlers.go (lines ~1343-1397)
// Allocate inner IPv4 from the peer's /24.
//
// We must pass the session UUID too — AllocateInnerIP is idempotent
// on (providerID, sessionID) so a transient retry reuses the same
// IP instead of burning a new Y. Generate sessionID up front and
// pass it through to CreateSession below.
sessionID := uuid.New()
innerIP, allocErr := h.st.AllocateInnerIP(r.Context(), providerID, sessionID)
if allocErr != nil {
    respondError(w, http.StatusInternalServerError, "inner ip allocation failed")
    return
}
// ...
session := &store.Session{
    ID:              sessionID,
    CustomerID:      customerUUID,
    Region:          chosenRegion,
    PrimaryProvider: providerID,
    CurrentProvider: providerID,
    State:           pb.VpnSessionState_CREATING,
    CreatedAt:       time.Now(),
    LastActivityAt:  time.Now(),
    ClientPublicKey: req.ClientPublicKey,
    InnerIP:         innerIP,           // ← the new field flows through here
    ExpiresAt:       &expiresAt,
    PaymentAuthorization: paymentAuth,
}
// ...
// Response shape — note the JSON key is customer_inner_cidr (CIDR /32
// form), NOT raw inner_ip. SDK authors mirror this casing.
_ = json.NewEncoder(w).Encode(map[string]interface{}{
    "session_id":          sessionID.String(),
    "peer_public_key":     providerInfo.WgPublicKey,
    "peer_endpoint":       providerInfo.Endpoint,
    "customer_inner_cidr": innerIP + "/32",
    "allowed_ips":         "0.0.0.0/0",
    "dns_servers":         h.defaults.DNSServers,
    "expires_at":          expiresAt.UTC().Format(time.RFC3339),
    "region":              chosenRegion,
    "quota_state":         computeQuotaState(resolvedTier, usedBytes).String(),
})
```

**Wire-shape gotcha:** the handler emits `customer_inner_cidr` (the
allocated IP suffixed with `/32`), not raw `inner_ip`. The mobile
app's `coordinator.ts` decodes the raw `inner_ip` key for forward
compatibility, and SDK consumers see `customer_inner_cidr` because
they go through the customer-SDK path. Keep the JSON key spelling
consistent with whatever the SDK consumers already expect — renaming
breaks every downstream parser.

## Step 6 — mobile app decode

Add the field to the mobile coordinator client at
`mobile/ios/src/lib/coordinator.ts`. Mobile uses **camelCase** in the
interface and snake_case for the wire payload:

```ts
// mobile/ios/src/lib/coordinator.ts (lines ~168-181)
export interface MobileSession {
  sessionId: string;
  peerPublicKey: string;
  peerEndpoint: string;
  innerIP: string;            // ← new field surfaced to the PacketTunnelProvider
  region: string;
  retryAfterSec?: number;     // populated on 503
  status: number;
}

// ...inside requestMobileSession (lines ~259-273):
const body = (await res.json()) as {
  session_id: string;
  peer_public_key: string;
  peer_endpoint: string;
  inner_ip?: string;          // ← optional so older servers still parse
  region?: string;
};
return {
  sessionId: body.session_id,
  peerPublicKey: body.peer_public_key,
  peerEndpoint: body.peer_endpoint,
  innerIP: body.inner_ip ?? '',
  region: body.region ?? args.region,
  status: res.status,
};
```

Make the wire field **optional** in the response type. The mobile app
must keep working against a coordinator pod that hasn't rolled the new
field yet — empty-string fallback is fine for a string field; for
numeric/enum fields pick a sentinel that maps to "unspecified".

## Step 7 — customer SDKs (TypeScript / Python / Go / Java)

All four SDKs must add the new request/response type **and** any
client-side validation. Snake_case wire format on the type, language-
idiomatic naming on the method.

**TypeScript** (`sdks/typescript/src/types.ts`,
`sdks/typescript/src/client.ts`):

```ts
// sdks/typescript/src/types.ts (lines ~233-250)
export interface RequestMobileSessionResponse {
  session_id: string;
  peer_public_key: string;
  peer_endpoint: string;
  customer_inner_cidr: string;        // wire shape is /32 form
  allowed_ips: string;
  dns_servers: string[];
  region: string;
  expires_at: string;
  quota_state: QuotaState | string;
}

// sdks/typescript/src/client.ts (lines ~254-271)
async requestMobileSession(
  body: RequestMobileSessionRequest,
  signal?: AbortSignal,
): Promise<RequestMobileSessionResponse> {
  if (!body.customer_id) throw new Error('requestMobileSession: customer_id is required');
  if (!body.client_public_key) throw new Error('requestMobileSession: client_public_key is required');
  return this.transport.request<RequestMobileSessionResponse>(
    'POST', '/v1/vpn/sessions/mobile', body, undefined, signal,
  );
}
```

**Python** (`sdks/python/src/iogrid/types.py`, `client.py`):

```python
# types.py (lines ~226-232)
class RequestMobileSessionResponse(TypedDict, total=False):
    session_id: str
    peer_public_key: str
    peer_endpoint: str
    customer_inner_cidr: str
    allowed_ips: str
    dns_servers: List[str]
    region: str
    expires_at: str
    quota_state: str

# client.py (lines ~294-323)
async def request_mobile_session(
    self,
    body: RequestMobileSessionRequest,
) -> RequestMobileSessionResponse:
    if not body.get("customer_id"):
        raise ValueError("request_mobile_session: customer_id is required")
    if not body.get("client_public_key"):
        raise ValueError("request_mobile_session: client_public_key is required")
    return await self._transport.request(
        RequestMobileSessionResponse,
        "POST", "/v1/vpn/sessions/mobile", body,
    )
```

**Go** (`sdks/go/types.go`, `sdks/go/client.go`):

```go
// types.go (lines ~257-265)
type RequestMobileSessionResponse struct {
    SessionID         string     `json:"session_id"`
    PeerPublicKey     string     `json:"peer_public_key"`
    PeerEndpoint      string     `json:"peer_endpoint"`
    CustomerInnerCIDR string     `json:"customer_inner_cidr"`
    AllowedIPs        string     `json:"allowed_ips"`
    DNSServers        []string   `json:"dns_servers"`
    Region            string     `json:"region"`
    ExpiresAt         string     `json:"expires_at"`
    QuotaState        string     `json:"quota_state"`
}

// client.go (lines ~338-355)
func (c *Client) RequestMobileSession(ctx context.Context, body RequestMobileSessionRequest) (*RequestMobileSessionResponse, error) {
    if body.CustomerID == "" {
        return nil, errors.New("iogrid: RequestMobileSession: CustomerID is required")
    }
    if body.ClientPublicKey == "" {
        return nil, errors.New("iogrid: RequestMobileSession: ClientPublicKey is required")
    }
    var out RequestMobileSessionResponse
    return &out, c.do(ctx, "POST", "/v1/vpn/sessions/mobile", body, &out)
}
```

**Java** (`sdks/java/src/main/java/com/iogrid/sdk/Types.java`,
`IogridClient.java`):

```java
// Types.java (lines ~226-232)
public record RequestMobileSessionResponse(
    @JsonProperty("session_id")          String sessionId,
    @JsonProperty("peer_public_key")     String peerPublicKey,
    @JsonProperty("peer_endpoint")       String peerEndpoint,
    @JsonProperty("customer_inner_cidr") String customerInnerCidr,
    @JsonProperty("allowed_ips")         String allowedIps,
    @JsonProperty("dns_servers")         List<String> dnsServers,
    @JsonProperty("region")              String region,
    @JsonProperty("expires_at")          String expiresAt,
    @JsonProperty("quota_state")         String quotaState
) {}

// IogridClient.java (lines ~218-229)
public Types.RequestMobileSessionResponse requestMobileSession(
    Types.RequestMobileSessionRequest body) throws IOException {
  if (body.customerId() == null || body.customerId().isEmpty()) {
    throw new IllegalArgumentException("requestMobileSession: customerId is required");
  }
  if (body.clientPublicKey() == null || body.clientPublicKey().isEmpty()) {
    throw new IllegalArgumentException("requestMobileSession: clientPublicKey is required");
  }
  return doPost("/v1/vpn/sessions/mobile", body,
      Types.RequestMobileSessionResponse.class);
}
```

## Testing checklist

Each layer needs its own test so a regression at one boundary doesn't
hide under green coverage somewhere else.

| Layer | Test file | What to assert |
|---|---|---|
| `store.Memory` | `coordinator/services/vpn-svc/internal/store/inner_ip_test.go` | `AllocateInnerIP` is idempotent for the same `(provider, session)`; different providers use distinct X octets; Y is monotonic per provider |
| `store.Postgres` | `coordinator/services/vpn-svc/internal/store/inner_ip_postgres_integration_test.go` | Same three properties, plus a manual `UPDATE vpn_sessions SET inner_ip = $1::inet` to verify the idempotency-by-prior-stamp path |
| Handler | `coordinator/services/vpn-svc/internal/server/*_test.go` | 201 path returns the field; 503 / 429 paths omit it (or surface zero) |
| TypeScript SDK | `sdks/typescript/test/client.test.ts` | `customer_inner_cidr` round-trips through `requestMobileSession()` |
| Mobile decode | `mobile/ios/__tests__/coordinator.test.ts` (or Maestro flow 10) | `body.inner_ip` decoded to `MobileSession.innerIP`; missing key → empty-string fallback |
| Deployed coordinator | `.github/workflows/sessions-mobile-smoke.yml` | Post a real request against the live coordinator and assert the field appears in the response — gates the CD pipeline |

## Checklist before opening the PR

- [ ] Proto field added with comment + `// Refs #<issue>`
- [ ] `make proto-check` is clean
- [ ] `store.Session` struct extended under the `Mobile session fields` block
- [ ] Both `memory.go` and `postgres.go` implement the field/method
- [ ] Goose migration added with `Up` AND `Down`, partial UNIQUE index
      where appropriate
- [ ] `handlers.go` reads/writes the field; JSON key spelling matches
      existing wire convention (`customer_inner_cidr`-style, not raw)
- [ ] `mobile/ios/src/lib/coordinator.ts` decodes the new wire key with
      an optional fallback
- [ ] All four customer SDKs add the request/response type + client
      validation
- [ ] Unit tests at each layer + the deployed-coordinator smoke gate
- [ ] PR title `feat(<scope>): <field> on mobile session (Refs #<issue>)`,
      body links the proto commit + the SDK commit
