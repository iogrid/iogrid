# Phase 0 customer demo — vCard LinkedIn enrichment

This is the runnable companion to [`docs/PHASE0_FIRST_CUSTOMER.md`](../../docs/archive/2026-05-21-phase0-first-customer.md)
and the canonical example of iogrid's first internal customer per
[`docs/ROADMAP.md`](../../docs/ROADMAP.md) Phase 0.

## The scenario

[Dynolabs vCard](https://dynolabs.io/vcard) is OpenOva's contacts app.
When a user imports a contact by email, vCard's `/v1/enrich/email`
handler currently calls Apollo — which returns an empty payload
without a paid plan, so titles + companies + photos never land in the
user's contact card.

**Phase 0 makes vCard route a LinkedIn-profile-page fetch through
iogrid's bandwidth proxy.** A residential IP from the iogrid mesh
fronts the request, evades the per-datacenter-IP rate limit LinkedIn
applies to AWS / Hetzner / GCP egress, and returns the rendered HTML
to vCard. vCard then parses name / title / company out of the public
profile page with a permissive HTML walker — no headless browser, no
JavaScript execution, no logged-in scraping.

This validates three things end-to-end:

1. **Routing works** — customer API key → proxy-gateway → provider
   daemon → destination, with the byte stream tunnelled.
2. **Anti-abuse filter is sound** — LinkedIn is an opt-in category
   (`social-intel`) and only providers who consented to it receive
   the request.
3. **Latency is acceptable** — p95 under 600 ms (versus Proxycurl's
   ~1 s) measured against a representative sample.

## What's in this directory

| File | Purpose |
|---|---|
| `client.go` | Minimal Go client. Authenticates via API key, opens SOCKS5 connection to `proxy.iogrid.org:443`, fetches a LinkedIn profile through it, parses name / title / company out of the HTML. |
| `client_test.go` | Unit tests for the HTML extraction (uses fixture HTML, no network). |
| `Dockerfile` | Containerises the demo. Single static binary, ~12 MB image. |
| `kustomization.yaml` | Kustomize overlay for running the demo as a `CronJob` on the cluster. |
| `cronjob.yaml` | Reference `CronJob` manifest — fires every 6 hours, posts results to a webhook. |

## Quick start (local)

```bash
# 1. Set credentials (issued by the iogrid customer-onboarding flow — see
#    docs/PHASE0_FIRST_CUSTOMER.md for the self-service walkthrough)
export IOGRID_API_KEY=ig_live_xxxx
export IOGRID_WORKSPACE=vcard-prod
export PROXY_URL=proxy.iogrid.org:443

# 2. Enrich a single profile
go run ./examples/phase0-vcard-customer \
  -vanity satyanadella \
  -timeout 10s

# Example output (JSON to stdout):
# {
#   "vanity":   "satyanadella",
#   "name":     "Satya Nadella",
#   "title":    "Chairman and CEO at Microsoft",
#   "company":  "Microsoft",
#   "latency_ms": 542,
#   "proxy_used": true,
#   "provider_country": "US"
# }
```

## How the proxy is wired

```
   vCard worker          Traefik edge          iogrid proxy-gateway      provider daemon
   (this Go binary)     (TLS terminator,      (in-cluster Service,      (home Mac, WG tunnel)
                        proxy.iogrid.org:443)  speaks SOCKS5 on TCP)
       │                        │                       │                         │
       │  TCP SYN/SYN-ACK       │                       │                         │
       ├───────────────────────►│                       │                         │
       │  ─── STEP 1 ─── TLS handshake (SNI=proxy.iogrid.org) ────                │
       │═══════════════════════►│                       │                         │
       │  (Traefik's iogrid-tls cert; HostSNI router selects the                  │
       │   proxy IngressRouteTCP and terminates TLS here)                         │
       │                        │  plain TCP forward    │                         │
       │                        ├──────────────────────►│                         │
       │  ─── STEP 2 ─── SOCKS5 greet (method=USERPASS) on the *tls.Conn* ───     │
       ├═══════════════════════►├──────────────────────►│                         │
       │  USERPASS sub-negotiate                                                   │
       │  user = workspace handle                                                  │
       │  pass = ig_live_xxxx                                                      │
       ├═══════════════════════►├──────────────────────►│── billing-svc.ValidateApiKey
       │                        │                       │── antiabuse-svc.CheckUrl(linkedin.com)
       │                        │                       │── scheduler picks provider with
       │                        │                       │   `social-intel` opt-in + US geo
       │                        │                       │                         │
       │  ─── STEP 3 ─── SOCKS5 CONNECT linkedin.com:443 ───                      │
       ├═══════════════════════►├──────────────────────►├─── mTLS tunnel ────────►│
       │                        │                       │                         │── outbound TCP from
       │                        │                       │                         │   provider's residential IP
       │  ─── STEP 4 ─── INNER TLS handshake (E2E client ↔ LinkedIn) ─────        │
       │═════════════════════════════════════════════════════════════════════════►
       │  HTTP GET /in/<vanity>                                                   │
       │═════════════════════════════════════════════════════════════════════════►
       │                                                                          │
       │  200 OK + HTML                  ← bytes metered every 1 MiB              │
       │◄═════════════════════════════════════════════════════════════════════════
       │                                                                          │
       │  parse name/title/company                                                │
```

Key points:

- **TWO TLS layers — both matter** (see issue #265 for what happens
  if you skip the outer one):
  - **Outer TLS** (`client ↔ Traefik`, STEP 1 above). Terminates at
    the iogrid edge with the public ACME cert for
    `proxy.iogrid.org`. The client MUST open this with `tls.Dial`
    *before* writing any SOCKS5 byte — Traefik's
    `HostSNI(proxy.iogrid.org)` router needs to see the
    `ClientHello`. Speaking SOCKS5 on raw TCP to port 443 hangs
    forever and the caller observes `context deadline exceeded`.
  - **Inner TLS** (`client ↔ LinkedIn`, STEP 4 above). End-to-end
    over the SOCKS5 tunnel; opaque bytes from the edge's and the
    proxy-gateway's perspective. Tunnels through the residential
    provider with no MITM possible.
- The proxy-gateway and the provider daemon **never see the
  plaintext**. They see bytes. The customer-supplied destination is
  used for routing + anti-abuse, nothing else.
- Bytes are metered at the gateway (in + out). Customer is invoiced
  per GB. Provider is credited per GB on the byte-level audit feed.
- The `social-intel` category is restricted to providers who
  explicitly opted in during onboarding. Default providers don't
  receive LinkedIn traffic — this is enforced at scheduling time, not
  post-hoc.

## Latency benchmark assumptions

The "p95 < 600 ms" target in `docs/PHASE0_FIRST_CUSTOMER.md` is built
on these assumptions:

| Hop | Budget |
|---|---|
| Client → proxy-gateway (TCP + SOCKS5 + USERPASS) | 60 ms |
| Gateway → billing-svc.ValidateApiKey (sub-100 µs hot, ~5 ms cold) | 5 ms |
| Gateway → antiabuse-svc.CheckUrl (in-memory hot cache) | 2 ms |
| Gateway → provider daemon (WireGuard tunnel) | 80 ms |
| Provider → LinkedIn (residential ISP US-East to LinkedIn DC) | 150 ms |
| LinkedIn render + send (profile page ~250 KB HTML) | 200 ms |
| Return path (LinkedIn → provider → gateway → client) | 80 ms |
| **Total p50** | **~430 ms** |
| **+ tail latency budget (network jitter + scheduling churn)** | **+170 ms** |
| **Total p95** | **~600 ms** |

Comparison to industry benchmarks:

- Proxycurl (LinkedIn enrichment SaaS): ~1 s p50 per profile, $0.49/call
- ScrapingBee (general residential proxy + JS render): ~1.5 s p50, ~$0.005/call
- iogrid Phase 0 target: ~430 ms p50, ~$0.30/GB (so a 250 KB profile
  costs roughly **$0.000075** — ~6500× cheaper than Proxycurl per call)

The Proxycurl comparison is the canonical pitch line: same data,
~10× cheaper at expected usage volumes, half the latency.

## LinkedIn ToS / scraping gray area

LinkedIn's [User Agreement section 8.2](https://www.linkedin.com/legal/user-agreement)
prohibits "scraping" of public profile data without explicit
permission. The legal landscape is unsettled — see
[hiQ Labs, Inc. v. LinkedIn Corp.](https://en.wikipedia.org/wiki/HiQ_Labs_v._LinkedIn)
which held that scraping publicly accessible LinkedIn data does not
violate the CFAA, but does NOT immunize the scraper from breach-of-
contract claims under LinkedIn's ToS.

**iogrid's position** (consistent with the bandwidth-proxy product
generally):

- We sell residential bandwidth. Customers choose their own
  destinations. **The customer is responsible for ToS compliance on
  the destinations they route through.** This is the same model
  Bright Data / Oxylabs / Smartproxy operate under.
- We enforce **anti-abuse** (CSAM hashes, PhishTank, port restrictions
  — see `docs/BUSINESS-STRATEGY.md` §6 (Legal risk landscape & mitigation)). We do NOT enforce destination ToS.
- Our [Customer ToS](../../legal/customer-tos.md) (Phase 1) will make
  this explicit and require customers to indemnify iogrid against
  destination-side legal claims.
- This demo is provided as **reference implementation only**. Running
  it commercially against LinkedIn is a customer-level decision and
  carries the customer's own ToS-compliance responsibility.

For Phase 0 specifically (Dynolabs vCard as the customer): the
founder is the operator, the use case is enriching the founder's own
contacts on their own product, and the traffic volume is low (single-
digit profiles per minute). This is the same risk profile every other
LinkedIn-enrichment tool operates under.

## Why a plain HTML parser, not headless Chrome?

- **Smaller blast radius.** A headless Chrome image is 1 GB+ and runs
  ~200 MB RSS. This binary is ~12 MB total and ~30 MB RSS.
- **Faster.** No JS execution. Profile pages render the canonical
  name / title / company in the static HTML (LinkedIn's SSR pass).
- **Cheaper to operate.** A CronJob fitting in 64 MiB Pod limits costs
  ~$0 on the existing cluster vs. an additional ~1 vCPU + 1 GB RAM
  for Chrome.
- **Easier to reason about.** The parser is ~120 lines of permissive
  HTML walking. Failure modes are obvious. The Chrome alternative
  hides scraping behind a black box.

## Running as a CronJob

```bash
# Apply to the iogrid k8s cluster (or any cluster with internet egress)
kubectl create namespace vcard-enrich
kubectl apply -k examples/phase0-vcard-customer

# Watch the first run
kubectl -n vcard-enrich logs -l job-name=vcard-linkedin-enrich --tail=100 -f
```

The CronJob picks up a list of vanities from a ConfigMap, batches them,
fires them through iogrid in parallel (semaphore = 4), and POSTs the
JSON results back to a webhook (default `https://vcard-api.dynolabs.io/v1/enrich/import`).

## Next steps

- For the marketing-facing customer success story see the
  [iogrid landing page customers section](../../marketing/app/page.tsx).
- For the full Phase 0 onboarding walkthrough see
  [`docs/PHASE0_FIRST_CUSTOMER.md`](../../docs/archive/2026-05-21-phase0-first-customer.md).
- For the proxy workload docs see
  [docs.iogrid.org/workloads/proxy](../../docs-site/src/content/docs/workloads/proxy.mdx).
