# iogrid e2e harness

End-to-end smoke + integration test harness. Boots a single-node `kind`
cluster, installs the lightweight infra needed by the coordinator
microservices, deploys their scaffold images, and exercises real customer
+ provider user paths against the live cluster.

This harness is intentionally **NOT a merge gate** — it's informational.
CI runs `e2e-ci.yml` with `continue-on-error: true` and uploads the JUnit
matrix so we can see what's drifting. As flows stabilise we'll promote
individual ones to PR-blocking.

## TL;DR — run locally

```
cd e2e
make full        # up + seed + smoke (with teardown left to `make down`)
```

Required tools on your PATH:

| Tool      | Min version | One-liner install                                                     |
|-----------|-------------|-----------------------------------------------------------------------|
| kind      | v0.20+      | `go install sigs.k8s.io/kind@v0.24.0`                                 |
| kubectl   | v1.30+      | `curl -L https://dl.k8s.io/release/v1.31.0/bin/linux/amd64/kubectl …` |
| helm      | v3.16+      | `curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 \| bash` |
| jq        | 1.6+        | `sudo apt-get install -y jq`                                          |
| envsubst  | any         | `sudo apt-get install -y gettext-base`                                |
| ncat      | any         | `sudo apt-get install -y ncat`                                        |
| openssl   | 3.x         | `sudo apt-get install -y openssl`                                     |
| docker    | 20+         | (system pkg)                                                          |

## Architecture overview

```
                ┌──────────────────────────────────────────────┐
                │   kind cluster (single node, kindnet CNI)     │
                │                                              │
   smoke ──────►│  ┌──────────────┐  ┌──────────────────────┐  │
   curl/nc      │  │ infra        │  │ iogrid microservices │  │
   from host    │  │ ─ Postgres   │◄─┤ ─ identity-svc       │  │
                │  │ ─ NATS       │  │ ─ providers-svc      │  │
                │  │ ─ MailHog    │◄─┤ ─ workloads-svc      │  │
                │  └──────────────┘  │ ─ antiabuse-svc      │  │
                │                    │ ─ billing-svc        │  │
                │                    │ ─ proxy-gateway      │  │
                │                    └──────────────────────┘  │
                └──────────────────────────────────────────────┘
```

We deliberately **DO NOT** use the prod manifests under `infra/k8s/base/`
because they assume:

  - CNPG (postgresql.cnpg.io/v1 Cluster)
  - Cilium CNI + ClusterMesh
  - cert-manager Issuers / Certificates
  - Sealed-secrets via Bitnami controller

Each of those takes 60-180s to install and adds CRDs we don't exercise
in the smoke flows. The e2e overlay swaps them for:

  - Plain Postgres 16 StatefulSet (`manifests/postgres/postgres.yaml`)
  - Plain NATS 2.10 single-replica (`manifests/nats/nats.yaml`)
  - MailHog 1.0.1 (`manifests/mailhog/mailhog.yaml`)
  - A self-signed test CA generated inline in `bootstrap.sh`
  - Static dev secrets (`manifests/secrets/test-secrets.yaml`)

When you need to validate prod manifests, use `make -C ../infra/k8s
kustomize-build` instead — that's covered by `.github/workflows/k8s-validate.yml`.

## Smoke flows

| # | Flow                              | What it exercises                                                |
|---|-----------------------------------|------------------------------------------------------------------|
| 1 | `provider-onboard.sh`             | providers-svc PairDaemon + ListProviders                         |
| 2 | `customer-workload-submit.sh`     | workloads-svc SubmitWorkload + GetWorkload                       |
| 3 | `bandwidth-proxy.sh`              | proxy-gateway SOCKS5 greeting + audit log emission               |
| 4 | `identity-flow.sh`                | identity-svc magic-link round-trip via MailHog + Google start    |
| 5 | `antiabuse-block.sh`              | antiabuse-svc CheckUrl block/allow + audit                       |
| 6 | `billing-subscription.sh`         | billing-svc subscription read + checkout structured-error path   |

Every flow is a self-contained bash script under `smoke/`. They share a
small library (`smoke/_lib.sh`) that wraps port-forwarding + cleanup +
assertion helpers.

## Adding a new flow

1. Create `smoke/<name>.sh`, start with `FLOW_NAME=<name>; . "$(dirname "$0")/_lib.sh"`.
2. Use `port_forward <svc> <local>:<remote>` and remember to
   `add_pf_pid "$PF"` so the trap cleanup catches it.
3. Use `flow_log` for tracing and `fail "<msg>"` for assertion errors.
4. Exit 0 on PASS, non-zero on FAIL. The runner captures stdout/stderr
   into `out/smoke/<name>.log`.
5. `chmod +x` so `bash` can resolve it directly.

## Debugging a failing flow

```
# 1. Boot + seed only, don't run smoke
make up seed
# 2. Run the single flow with verbose output
KUBECONFIG=$(pwd)/out/kubeconfig bash smoke/identity-flow.sh
# 3. Inspect pod state
KUBECONFIG=$(pwd)/out/kubeconfig kubectl -n iogrid get pods,svc,ep
KUBECONFIG=$(pwd)/out/kubeconfig kubectl -n iogrid logs deploy/identity-svc --tail=200
# 4. Tear down
make down
```

For a stack-wide log dump:

```
make logs
```

## Environment variables

| Var            | Default       | Effect                                                       |
|----------------|---------------|--------------------------------------------------------------|
| `CLUSTER_NAME` | `iogrid-e2e`  | kind cluster name (set if you run multiple harnesses)        |
| `NAMESPACE`    | `iogrid`      | Target namespace for all manifests                           |
| `IMAGE_TAG`    | `scaffold`    | Tag suffix on `ghcr.io/iogrid/<svc>:<tag>`                   |
| `BUILD_LOCAL`  | `0`           | Build images from `coordinator/` instead of pulling          |

## What's NOT covered (yet)

The current scaffold only smokes the JSON / Connect-RPC surfaces. Future
work (registered as separate tickets):

  - Daemon binary end-to-end (would require cross-compiling the Rust
    binary and running it in a sidecar pod)
  - Stripe webhook with `stripe-cli` listener
  - Multi-provider scheduling (only 1 provider stub registered today)
  - Cross-region failover (single-node kind has no region semantics)
  - Telemetry / OTLP collector smoke
  - VPN gateway WireGuard handshake
  - iOS build gateway / Mac runner
