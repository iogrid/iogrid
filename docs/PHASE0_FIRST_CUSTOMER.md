# Phase 0 first customer — vCard LinkedIn enrichment

This document is the operator-facing walkthrough of the Phase 0 customer
integration. It pairs the runnable demo in
[`examples/phase0-vcard-customer/`](../examples/phase0-vcard-customer)
with the architectural context in [`docs/ROADMAP.md`](./ROADMAP.md) Phase 0.

> **TL;DR.** Dynolabs vCard (OpenOva's contacts app) uses iogrid's
> bandwidth proxy to fetch LinkedIn profile pages from a residential
> IP, bypassing the per-datacenter-IP rate limit LinkedIn applies to
> AWS / Hetzner / GCP egress. This is the first real customer use case
> on the iogrid mesh. It validates routing, anti-abuse, and pricing
> end-to-end at minimal operational risk.

## What we're proving

Phase 0 of the iogrid roadmap commits to three success criteria
(see [`ROADMAP.md`](./ROADMAP.md#phase-0--internal-pilot-2-weeks)):

1. **Routing works** — a real customer API key, routed through the
   bandwidth proxy, lands on a real LinkedIn IP and the destination
   responds 200.
2. **Anti-abuse is sound** — `social-intel` is an opt-in category. The
   scheduler MUST refuse to route LinkedIn traffic to a provider who
   didn't opt in.
3. **Latency is acceptable** — p95 < 600 ms end-to-end, vs. Proxycurl's
   ~1 s. See [latency benchmark](#latency-benchmark) below.

The Dynolabs vCard product (a self-hosted iOS contacts app) provides
the demand side. Its `/v1/enrich/email` handler currently calls Apollo
and returns an empty payload without a paid plan, so titles + companies
+ photos never make it into user contact cards (the "Build 170
title/company not imported" issue). This is fixed by routing the
LinkedIn-page fetch through iogrid.

## Step-by-step — signing up as a customer

The self-service onboarding flow is exposed by gateway-bff:

```bash
POST /api/v1/onboard/customer
Authorization: Bearer <user-access-token>
Content-Type: application/json

{
  "handle":                "vcard-prod",
  "display_name":          "Dynolabs vCard — Production",
  "billing_email":         "billing@dynolabs.io",
  "initial_api_key_label": "linkedin-enrich-cronjob"
}
```

A successful signup returns `201` with:

```json
{
  "workspace_id":     "11111111-2222-3333-4444-555555555555",
  "handle":           "vcard-prod",
  "display_name":     "Dynolabs vCard — Production",
  "billing_email":    "billing@dynolabs.io",
  "api_key": {
    "id":           "...",
    "workspace_id": "...",
    "label":        "linkedin-enrich-cronjob",
    "prefix":       "iog_aBcD...",
    "created_at":   "2026-05-19T08:00:00Z",
    "plaintext":    "iog_aBcD...full-32-byte-secret..."
  },
  "proxy_endpoint":    "proxy.iogrid.org:443",
  "onboarding_guide":  "https://docs.iogrid.org/getting-started/phase0-first-customer/",
  "created_at":        "2026-05-19T08:00:00Z"
}
```

Notes:

- **The plaintext API key appears only in this response.** Subsequent
  calls to `GET /api/v1/customer/api-keys` return the same key without
  `plaintext`. Persist the secret client-side immediately — there is no
  recovery flow short of revoking + re-issuing.
- **Workspace handle uniqueness** is enforced at signup. Reserved
  handles (`admin`, `api`, `billing`, `support`, etc.) are blocked
  client-side at the BFF.
- **Identity-svc workspace creation** is stubbed in Phase 0 — gateway-bff
  holds a handle → UUID mapping in-process. Phase 1 promotes this to
  the identity-svc.Workspace persistence path (issue tracked alongside
  the workspace-agent work).

## Step-by-step — making your first proxy request

### Option A — straight `curl` over SOCKS5

```bash
export IOGRID_API_KEY=iog_aBcD...   # from the onboarding response
export IOGRID_WORKSPACE=vcard-prod
curl --proxy "socks5h://$IOGRID_WORKSPACE:$IOGRID_API_KEY@proxy.iogrid.org:443" \
     -H "User-Agent: Mozilla/5.0 (compatible; vCardEnrich/0.1)" \
     https://www.linkedin.com/in/satyanadella -o profile.html
```

The `socks5h://` scheme tells curl to do DNS resolution server-side
(important — we don't want the client to leak target hostnames to its
local resolver).

### Option B — the Phase 0 Go demo

```bash
git clone https://github.com/iogrid/iogrid.git
cd iogrid/examples/phase0-vcard-customer

export IOGRID_API_KEY=iog_aBcD...
export IOGRID_WORKSPACE=vcard-prod

go run . -vanity satyanadella -timeout 10s
```

Output is a single JSON object:

```json
{
  "vanity":          "satyanadella",
  "name":            "Satya Nadella",
  "title":           "Chairman and CEO at Microsoft",
  "company":         "Microsoft",
  "latency_ms":      542,
  "proxy_used":      true,
  "provider_country":"US"
}
```

This is the canonical client shape — vcard-api consumes the same JSON
directly into its contact-import pipeline.

### Option C — running it as a CronJob

```bash
kubectl create namespace vcard-enrich
kubectl create secret generic iogrid-creds \
  -n vcard-enrich \
  --from-literal=api-key=$IOGRID_API_KEY \
  --from-literal=workspace=$IOGRID_WORKSPACE
kubectl apply -k examples/phase0-vcard-customer
```

The CronJob fires every 6 hours and POSTs results to the customer
webhook of choice (default `https://vcard-api.dynolabs.io/v1/enrich/import`).

## Latency benchmark

Target: **p95 < 600 ms** for a single LinkedIn profile fetch through
the iogrid proxy.

### Synthetic budget (per hop, from `examples/phase0-vcard-customer/README.md`)

| Hop | Budget |
|---|---|
| Client → proxy-gateway (TCP + SOCKS5 + USERPASS) | 60 ms |
| Gateway → billing-svc.ValidateApiKey | 5 ms |
| Gateway → antiabuse-svc.CheckUrl | 2 ms |
| Gateway → provider daemon (WireGuard tunnel) | 80 ms |
| Provider → LinkedIn (residential ISP US-East to LinkedIn DC) | 150 ms |
| LinkedIn render + send (profile page ~250 KB HTML) | 200 ms |
| Return path (LinkedIn → provider → gateway → client) | 80 ms |
| **Total p50** | **~430 ms** |
| **+ tail latency budget (network jitter + scheduling churn)** | **+170 ms** |
| **Total p95** | **~600 ms** |

### Measured-in-production targets

These will be populated in the soak-test run that closes the Phase 0
milestone (5-day continuous run). Until that data exists, treat the
synthetic budget as a hypothesis, not a benchmark.

| Metric | Target | Measured |
|---|---|---|
| p50 latency | ≤ 430 ms | _pending soak_ |
| p95 latency | ≤ 600 ms | _pending soak_ |
| p99 latency | ≤ 1000 ms | _pending soak_ |
| Success rate | ≥ 99% | _pending soak_ |
| Provider-IP rotation events / day | < 100 | _pending soak_ |
| LinkedIn 403 rate | < 1% | _pending soak_ |

## Pricing — comparison to Proxycurl

The pitch line of the Phase 0 customer story is:

> Same enrichment data, ~10× cheaper at expected usage volumes, half
> the latency, and the customer controls their own ToS posture.

The numeric backing:

| Provider | Per-call price | Per-GB equivalent | Notes |
|---|---|---|---|
| **Proxycurl** (LinkedIn enrichment SaaS) | $0.49 / profile | ~$1900 / GB | Cached lookups, no real fetch. 1 profile ≈ 250 KB equivalent traffic. |
| **ScrapingBee** (general residential proxy + JS render) | $0.005 / call | ~$20 / GB | Headless Chrome. 1.5 s p50. |
| **Bright Data** (residential proxy line) | n/a per-call | $4–8 / GB | Enterprise contract, opaque consent legacy. |
| **iogrid (Phase 0)** | n/a per-call | **$0.30 / GB** | Transparent, byte-by-byte audit log. |

At expected vCard volume (~10 enrichments/day during Phase 0 soak):

- Proxycurl monthly bill: 10 × 30 × $0.49 = **$147 / month**
- iogrid monthly bill: 300 × 250 KB = ~75 MB ≈ **$0.02 / month**

This is _per-customer_ economics on a workload that costs Proxycurl
roughly the same to serve (Proxycurl pays for residential bandwidth
too — they just charge a per-call premium on top). The ~10× number in
the pitch comes from comparing to typical _customer-experienced_
LinkedIn-enrichment prices at the volumes a normal SaaS pulls (B2B
sales tools fetch hundreds to thousands of profiles a day).

## Customer success story framing

For the marketing site we frame this as **"Dynolabs vCard's import
quality went from 0% to >90% on enriched contacts after switching to
iogrid bandwidth proxy."**

The framing emphasises three points:

1. **Validation that the iogrid model works for real B2B workloads.**
   The use case (LinkedIn enrichment) is one of the largest categories
   of residential-proxy demand in the industry.
2. **Cost honesty.** We undercut Proxycurl ~10× at expected volume
   without claiming hidden-magic on top. The price difference comes
   from non-cash provider payouts (free-VPN tier provides 98% margin —
   see [`docs/BUSINESS-STRATEGY.md` §3 (Unit economics & provider incentives)](BUSINESS-STRATEGY.md#3-unit-economics--provider-incentives)).
3. **Customer-controlled ToS posture.** Unlike Proxycurl's "we make
   the API call for you" model, iogrid's customers retain control of
   their own outbound requests + headers, so ToS-compliance posture
   is theirs to set. This is the same architectural shape Bright Data
   / Oxylabs / Smartproxy operate under.

See [`marketing/app/page.tsx`](../marketing/app/page.tsx) for the
landing-page integration of this story.

## LinkedIn ToS / scraping gray area

Same posture as the demo's README:

- iogrid sells **bandwidth**. Customers choose their own destinations.
  The customer is responsible for ToS compliance on the destinations
  they route through.
- iogrid enforces **anti-abuse** (CSAM hashes, PhishTank, port
  restrictions — see [`docs/BUSINESS-STRATEGY.md` §6 (Legal risk landscape & mitigation)](BUSINESS-STRATEGY.md#6-legal-risk-landscape--mitigation)). iogrid does NOT
  enforce destination ToS.
- The [hiQ Labs v. LinkedIn](https://en.wikipedia.org/wiki/HiQ_Labs_v._LinkedIn)
  case held that scraping publicly-accessible LinkedIn data does not
  violate the CFAA, but does not immunize the scraper from breach-of-
  contract claims under LinkedIn's ToS.
- For Phase 0 specifically (Dynolabs vCard as the internal customer):
  the operator is the OpenOva founder, the use case is enriching their
  own contacts on their own product, volume is low (single-digit
  profiles per minute). Same risk profile every other LinkedIn-
  enrichment tool runs under.

The customer ToS draft (lawyer-reviewed, lands in Phase 1) will make
the customer-side ToS-compliance responsibility explicit and require
indemnification against destination-side legal claims.

## Where next

- [Demo source — `examples/phase0-vcard-customer/`](../examples/phase0-vcard-customer)
- [Bandwidth proxy reference docs](../docs-site/src/content/docs/workloads/proxy.mdx)
  (will be promoted to `https://docs.iogrid.org/workloads/proxy/`)
- [Phase 0 success criteria — `ROADMAP.md`](./ROADMAP.md#phase-0--internal-pilot-2-weeks)
- [Anti-abuse policy — `docs/BUSINESS-STRATEGY.md` §6 (Legal risk landscape & mitigation)](BUSINESS-STRATEGY.md#6-legal-risk-landscape--mitigation)
- [Incentive economics — `docs/BUSINESS-STRATEGY.md` §3 (Unit economics & provider incentives)](BUSINESS-STRATEGY.md#3-unit-economics--provider-incentives)
