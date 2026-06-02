// k6 load smoke for POST /v1/vpn/sessions/mobile (vpn-svc, #605).
//
// PURPOSE
// -------
// The unit + integration tests on #605/#608 prove correctness for a
// single caller. This script exercises the endpoint under realistic
// concurrency so we catch:
//
//   - Lock contention in the session-allocation path (the in-memory
//     peer-selection mutex).
//   - DB connection pool exhaustion (sessions are persisted in the
//     vpn-svc Postgres pool — default max_conns=25 in prod).
//   - Ingress / Cloudflare connection limits on api.iogrid.org.
//
// USAGE
// -----
//   k6 run \
//     -e TARGET_ENDPOINT=https://api.iogrid.org/v1/vpn/sessions/mobile \
//     -e CONCURRENCY=50 \
//     -e DURATION=30s \
//     --summary-export=k6-summary.json \
//     e2e/loadtest/sessions-mobile.js
//
// EXIT CODES
// ----------
// k6 exits non-zero if any defined threshold is breached. The CI
// workflow at .github/workflows/sessions-mobile-loadtest.yml relies
// on that to fail the build — DO NOT swallow the exit code.
//
// EXPECTED STATUS DISTRIBUTION
// ----------------------------
// Per the sessions-mobile-smoke.yml documented healthy-outcomes:
//   201 — real session minted (peer mesh has capacity)
//   401 — BillingValidator rejected the stub request (auth gate live)
//   404 — route not yet deployed (Flux reconcile window)
//   405 — chi router sees path but no POST handler bound (deploy pending)
//   503 — degraded-but-alive (no peers in region; Retry-After present)
//
// Anything else is a regression and fails the run.

import http from 'k6/http';
import { check } from 'k6';
import { uuidv4 } from 'https://jslib.k6.io/k6-utils/1.4.0/index.js';
import { Counter, Trend } from 'k6/metrics';

// -- Tunables from the workflow inputs / `-e` flags --------------------

const TARGET_ENDPOINT =
  __ENV.TARGET_ENDPOINT ||
  'https://api.iogrid.org/v1/vpn/sessions/mobile';

// `concurrency` workflow input. k6 calls this "vus" (virtual users).
const VUS = parseInt(__ENV.CONCURRENCY || '50', 10);

// `duration` workflow input. k6 accepts go-style durations ('30s','2m').
const DURATION = __ENV.DURATION || '30s';

// -- Custom metrics for richer artifact output -------------------------
//
// k6 emits its own http_req_duration / http_reqs / iteration_duration
// trends by default. We add per-status counters so the workflow's
// post-step jq query can show the exact response-class breakdown
// without re-parsing every sample.

const status_2xx = new Counter('status_2xx');
const status_401 = new Counter('status_401');
const status_404 = new Counter('status_404');
const status_405 = new Counter('status_405');
const status_503 = new Counter('status_503');
const status_other_4xx = new Counter('status_other_4xx');
const status_other_5xx = new Counter('status_other_5xx');
const status_unreachable = new Counter('status_unreachable');

// Latency split per status class. p99 on 201 vs 503 means different
// things — the former is the real allocator path, the latter is the
// fast-degraded path.
const duration_201 = new Trend('duration_201', true);
const duration_503 = new Trend('duration_503', true);

// -- Options -----------------------------------------------------------
//
// The thresholds here are the workflow's pass/fail contract. If you
// loosen them, update sessions-mobile-loadtest.yml comments to match.

export const options = {
  scenarios: {
    mobile_sessions: {
      executor: 'constant-vus',
      vus: VUS,
      duration: DURATION,
      gracefulStop: '10s',
    },
  },
  thresholds: {
    // Tail latency MUST stay under 5s — anything beyond is "something
    // serious is wrong" per the task spec. p95 < 500ms is the SLO
    // target but we DON'T fail on it (degraded 503 path may bump p95
    // for legitimate reasons). p99 is the hard gate.
    http_req_duration: ['p(95)<500', 'p(99)<5000'],

    // Unhandled errors — fail the workflow on ANY count > 0.
    // 5xx-other-than-503: vpn-svc panic, DB outage, unhandled
    // dependency failure. Indistinguishable from a real prod incident
    // — must not happen under load.
    status_other_5xx: ['count==0'],

    // 4xx-other-than-401/404/405: bad-request schema regression,
    // ingress route misconfig, etc. The task spec explicitly
    // whitelists 401/404/405 as healthy; everything else is a bug.
    status_other_4xx: ['count==0'],

    // Network-level unreachability (DNS, TLS, connection refused).
    // Anything >0 means the endpoint is not reachable from the CI
    // runner — could be transient Cloudflare or a real outage.
    // Allow up to 1% for flake (CI runners hit egress limits).
    status_unreachable: ['count<10'],
  },

  // Tag every sample with the endpoint so multi-target runs (staging
  // vs prod) can be diff'd in Grafana later.
  tags: {
    endpoint: TARGET_ENDPOINT,
  },
};

// -- Payload generator -------------------------------------------------
//
// Fresh UUID per iteration per VU — otherwise the server-side
// idempotency layer (#605 returns the same session_id for repeated
// (customer_id, client_public_key) tuples) would mask real allocator
// contention behind a fast cache hit.

function buildPayload() {
  // The customer_id is whatever UUID the mobile app generates on
  // first launch. Synthesising a fresh one per iter mirrors the
  // worst-case fan-in: many unknown customers hitting at once.
  const customer_id = uuidv4();

  // Stub WireGuard pubkey. The coordinator validates length/shape
  // (32 bytes base64, ends with '='), not provenance. A constant
  // would let the server's recent-pubkey LRU short-circuit, so we
  // vary it per iter too — last 4 bytes derived from the UUID
  // entropy.
  const tail = customer_id.replace(/-/g, '').slice(0, 8);
  const client_public_key = `AAAA1111BBBB2222CCCC3333DDDD${tail}=`;

  return JSON.stringify({
    customer_id: customer_id,
    region: 'auto',
    client_public_key: client_public_key,
  });
}

// -- Default VU function ----------------------------------------------

export default function () {
  const params = {
    headers: {
      'Content-Type': 'application/json',
      'Accept': 'application/json',
      // Tag the load test so prod log scrapers can filter/exclude
      // these probes from real customer signals.
      'X-Iogrid-Load-Test': 'sessions-mobile-loadtest',
    },
    // Be generous on cold-path: ingress + Postgres pool warm-up can
    // push the very first request per VU above 5s. The threshold
    // catches sustained degradation, not the first sample.
    timeout: '30s',
  };

  const res = http.post(TARGET_ENDPOINT, buildPayload(), params);

  // Per-status bookkeeping — feeds the thresholds + the artifact
  // summary. http.post returns status 0 for network-level failures.
  if (res.status === 0) {
    status_unreachable.add(1);
  } else if (res.status >= 200 && res.status < 300) {
    status_2xx.add(1);
    if (res.status === 201) {
      duration_201.add(res.timings.duration);
    }
  } else if (res.status === 401) {
    status_401.add(1);
  } else if (res.status === 404) {
    status_404.add(1);
  } else if (res.status === 405) {
    status_405.add(1);
  } else if (res.status === 503) {
    status_503.add(1);
    duration_503.add(res.timings.duration);
  } else if (res.status >= 400 && res.status < 500) {
    status_other_4xx.add(1);
  } else if (res.status >= 500) {
    status_other_5xx.add(1);
  }

  // Soft checks — these DO NOT fail the run on their own (the
  // threshold counters above do). They surface in the run summary
  // for human inspection and trigger the `checks` k6 metric so
  // operators can see "98% of responses were in the allowed set".
  check(res, {
    'status is in allowed set [201, 401, 404, 405, 503]': (r) =>
      r.status === 201 ||
      r.status === 401 ||
      r.status === 404 ||
      r.status === 405 ||
      r.status === 503,
    'response is not 5xx-other-than-503': (r) =>
      r.status < 500 || r.status === 503,
    'response body is non-empty when 2xx/4xx/5xx': (r) =>
      r.status === 0 || (r.body && r.body.length > 0),
  });
}

// -- handleSummary: emit a compact stdout report + JSON artifact ------
//
// k6's default text summary is verbose. We re-shape it to surface the
// numbers the workflow comments call out: p95/p99, RPS, per-status
// counters. The JSON is uploaded as the workflow artifact for
// post-run inspection.

export function handleSummary(data) {
  const metrics = data.metrics;
  const get = (name, field) =>
    metrics[name] && metrics[name].values
      ? metrics[name].values[field]
      : undefined;

  const fmt = (v, digits = 2) =>
    v === undefined || v === null ? 'n/a' : Number(v).toFixed(digits);

  const lines = [];
  lines.push('======================================================');
  lines.push(' k6 sessions-mobile load smoke — summary');
  lines.push('======================================================');
  lines.push(`  endpoint:     ${TARGET_ENDPOINT}`);
  lines.push(`  concurrency:  ${VUS} VUs`);
  lines.push(`  duration:     ${DURATION}`);
  lines.push('');
  lines.push('  Latency (http_req_duration, ms)');
  lines.push(`    avg:    ${fmt(get('http_req_duration', 'avg'))}`);
  lines.push(`    p50:    ${fmt(get('http_req_duration', 'med'))}`);
  lines.push(`    p95:    ${fmt(get('http_req_duration', 'p(95)'))}`);
  lines.push(`    p99:    ${fmt(get('http_req_duration', 'p(99)'))}`);
  lines.push(`    max:    ${fmt(get('http_req_duration', 'max'))}`);
  lines.push('');
  lines.push('  Throughput');
  lines.push(`    total reqs:  ${fmt(get('http_reqs', 'count'), 0)}`);
  lines.push(`    rps:         ${fmt(get('http_reqs', 'rate'))}`);
  lines.push('');
  lines.push('  Status breakdown');
  lines.push(`    2xx:           ${fmt(get('status_2xx', 'count'), 0)}`);
  lines.push(`    401:           ${fmt(get('status_401', 'count'), 0)}`);
  lines.push(`    404:           ${fmt(get('status_404', 'count'), 0)}`);
  lines.push(`    405:           ${fmt(get('status_405', 'count'), 0)}`);
  lines.push(`    503:           ${fmt(get('status_503', 'count'), 0)}`);
  lines.push(`    other 4xx:     ${fmt(get('status_other_4xx', 'count'), 0)}  (FAIL if >0)`);
  lines.push(`    other 5xx:     ${fmt(get('status_other_5xx', 'count'), 0)}  (FAIL if >0)`);
  lines.push(`    unreachable:   ${fmt(get('status_unreachable', 'count'), 0)}  (FAIL if >=10)`);
  lines.push('');
  lines.push('  Thresholds');
  for (const [name, t] of Object.entries(data.thresholds || {})) {
    lines.push(`    ${name}: ${t.ok ? 'OK' : 'FAIL'}`);
  }
  lines.push('======================================================');

  return {
    stdout: lines.join('\n') + '\n',
    // Full machine-readable summary for the artifact upload.
    'k6-summary.json': JSON.stringify(data, null, 2),
  };
}
