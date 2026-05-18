# DNS + TLS architecture for iogrid.org

End-state decisions are locked. This document explains *why* each piece is the way it is and how to operate it day-to-day.

---

## TL;DR

| Concern               | Choice                                               | Why                                                                                 |
|-----------------------|------------------------------------------------------|-------------------------------------------------------------------------------------|
| Registrar             | Dynadot                                              | Founder's account; same account as `openova.io` and `dynolabs.io`                  |
| Authoritative DNS     | **Dynadot Hosted DNS** (`ns1.dyna-ns.net`, `ns2.dyna-ns.net`) | Keeps iogrid independent from OpenOva's PowerDNS stack — see "Independence" below |
| Record mutation       | Dynadot API `set_dns2` from a kubectl-execed pod on the OpenOva mothership | Mothership public IP is allowlisted by Dynadot; bastion is not                    |
| Source of truth       | `infra/dynadot/iogrid-org-records.json` in this repo | Code-reviewed, branch-protected, auditable                                          |
| TLS certificates      | Let's Encrypt via cert-manager, HTTP-01 over Cilium Gateway API | Re-uses the existing cluster-wide `letsencrypt-prod` ClusterIssuer                  |
| Ingress / L7 routing  | Gateway API (Cilium gateway class), TLSRoute + HTTPRoute | Matches end-state architecture (commit `f49fe50`)                                  |
| Public ingress IP     | `45.151.123.50` (OpenOva mothership Hetzner LB)       | Single-VM today, will move to a dedicated iogrid LB before public launch           |

---

## DNS record set

All 8 records point at the mothership LB IP `45.151.123.50`. TTL 300s so we can flip to a dedicated LB IP without operational pain.

| Hostname               | Type | Value           | Backend                                          |
|------------------------|------|-----------------|--------------------------------------------------|
| `iogrid.org`           | A    | 45.151.123.50   | marketing-site                                   |
| `www.iogrid.org`       | A    | 45.151.123.50   | marketing-site                                   |
| `api.iogrid.org`       | A    | 45.151.123.50   | gateway-bff :8080                                |
| `app.iogrid.org`       | A    | 45.151.123.50   | web (Next.js) :3000                              |
| `proxy.iogrid.org`     | A    | 45.151.123.50   | proxy-gateway :443 (TLS passthrough)             |
| `build.iogrid.org`     | A    | 45.151.123.50   | build-gateway :8080                              |
| `docs.iogrid.org`      | A    | 45.151.123.50   | docs-site (placeholder)                          |
| `status.iogrid.org`    | A    | 45.151.123.50   | gateway-bff :8080 (URLRewrite to `/status`)      |

Source of truth: [`infra/dynadot/iogrid-org-records.json`](../infra/dynadot/iogrid-org-records.json).

### Why no wildcard

A wildcard `*.iogrid.org A 45.151.123.50` would cover any new subdomain automatically, but:

1. Wildcards encourage uncontrolled subdomain sprawl ("just spin up `experimental.iogrid.org`") which makes security-scope arguments harder.
2. The matching wildcard TLS cert needs DNS-01 ACME, which couples cert renewal to a Dynadot API key with write access. HTTP-01 over a discrete subdomain list keeps the renewal path read-only on DNS.
3. The subdomain set is small and well-defined per the architecture doc — explicit > implicit.

If a new subdomain is needed, edit `infra/dynadot/iogrid-org-records.json` + the certificate SAN list + add an HTTPRoute, one PR.

---

## Independence from OpenOva DNS

OpenOva runs a central PowerDNS in `openova-system` (commit `b6e60e07` on `openova-private`) and delegates `omani.works` etc. to `ns1.openova.io`/`ns2.openova.io`. We deliberately do NOT do this for iogrid:

- Per the end-state lock (commit `f49fe50` in this repo), iogrid is an independent brand. Its DNS resolution path must not transit OpenOva control.
- Outages on the OpenOva PowerDNS pods should not blacken iogrid.
- iogrid will eventually move to its own k8s cluster + LB; collapsing the move is easier when DNS is already first-party.

The price we pay: every iogrid DNS change is a Dynadot API call rather than a `kubectl apply -f zone.yaml`. We mitigate via the script in `scripts/dynadot-apply.sh` which makes the change auditable and gated by code review.

---

## Operational procedure — adding/changing a record

1. Edit `infra/dynadot/iogrid-org-records.json`.
2. Run `./scripts/dynadot-apply.sh` (dry-run; prints the URL it *would* call).
3. Open a PR.
4. After merge, on the bastion: `./scripts/dynadot-apply.sh --apply`. Requires kubectl access to the OpenOva mothership cluster (mothership IP is Dynadot-allowlisted; bastion is not, so the script kubectl-execs a one-shot Alpine pod on the mothership).
5. `./scripts/dynadot-apply.sh --verify` polls public resolvers; expect convergence within 5 minutes.

**WARNING**: Dynadot `set_dns2` is a *replace-whole-zone* call. Records not in the JSON file are deleted on apply. Always merge from JSON, never hand-edit via the Dynadot web UI.

---

## TLS certificate lifecycle

### Issuer

The ClusterIssuer `letsencrypt-prod` is shared with the OpenOva tenant on the mothership (`kubectl get clusterissuer letsencrypt-prod`, age 78d, Ready=True). Manifest in this repo: [`infra/k8s/cert-manager/cluster-issuer-letsencrypt.yaml`](../infra/k8s/cert-manager/cluster-issuer-letsencrypt.yaml).

It carries two HTTP-01 solvers:

- Primary: `gatewayHTTPRoute` targeting the iogrid Gateway. cert-manager auto-creates a transient HTTPRoute on port 80 hitting `/.well-known/acme-challenge` during validation.
- Fallback: `ingress.ingressClassName=traefik`. Used while the mothership is still on Traefik (transitional). Safe to delete once Cilium Gateway is the only ingress.

### Certificate CRs

[`infra/k8s/certificates/iogrid-org-cert.yaml`](../infra/k8s/certificates/iogrid-org-cert.yaml) declares two `Certificate` objects:

- `iogrid-org-tls` — SAN list of all TLS-terminated hostnames (apex + www + 5 subdomains), ECDSA P-256, 90d duration, 30d renewBefore. Stored as `Secret/iogrid-org-tls` in the `iogrid` namespace.
- `iogrid-proxy-tls` — separate single-SAN cert for `proxy.iogrid.org`. Even though that listener does TLS passthrough, we still hold a public cert so the Gateway can do hostname-based SNI routing and so future direct-terminate experiments are one annotation flip away.

### Renewal

cert-manager auto-renews 30 days before expiry. The renewal HTTPRoute is short-lived (typically < 60s) and does not interfere with normal traffic — Gateway API listeners on port 80 accept the challenge alongside the http→https redirect routes.

### Troubleshooting

```bash
# Status of all certs
kubectl -n iogrid get cert,certificaterequest,order,challenge

# Verbose on a failing cert
kubectl -n iogrid describe cert iogrid-org-tls
kubectl -n iogrid describe challenge | tail -50

# cert-manager controller logs
kubectl -n cert-manager logs -l app.kubernetes.io/name=cert-manager --tail=200 | grep -i iogrid
```

Common failures:

- **Pending challenge, `propagation check failed`**: the temporary HTTPRoute hasn't been routed by Cilium yet. Check `kubectl -n iogrid get httproute,gateway` and look for `Programmed=True`.
- **Rate-limit error**: LE allows 50 certs per registered domain per week. If you blew the budget by re-creating, switch the issuer to `letsencrypt-staging` to develop, then flip back.
- **DNS NXDOMAIN at the challenge step**: a record didn't propagate yet. `dig +short <host> A @1.1.1.1`. Wait 5 min after a `dynadot-apply.sh --apply`.

---

## Gateway API routing

[`infra/k8s/gateways/gateway.yaml`](../infra/k8s/gateways/gateway.yaml) declares one Gateway (`iogrid-gateway`) with:

- Two HTTP listeners (`http`, `http-apex`) on port 80 for ACME + http→https redirect.
- Seven HTTPS listeners on port 443 (`https-www`, `https-www-www`, `https-app`, `https-api`, `https-build`, `https-docs`, `https-status`), each pinned to a hostname and presenting the matching cert from `iogrid-org-tls`.
- One TLS Passthrough listener (`tls-proxy`) on port 443 for `proxy.iogrid.org`.

Each HTTPRoute targets the listener via `parentRefs[].sectionName`:

| Route file                        | sectionName    | Backend service        | Notes                              |
|-----------------------------------|----------------|------------------------|------------------------------------|
| `httproute-apex-www.yaml`         | https-www      | marketing-site :80     | Apex + www, plus http→https redir  |
| `httproute-app.yaml`              | https-app      | web :3000              | Next.js mgmt plane                 |
| `httproute-api.yaml`              | https-api      | gateway-bff :8080      | REST + gRPC-web BFF                |
| `httproute-build.yaml`            | https-build    | build-gateway :8080    | iOS-build orchestrator             |
| `httproute-docs.yaml`             | https-docs     | docs-site :80          | Static docs (placeholder)          |
| `httproute-status.yaml`           | https-status   | gateway-bff :8080      | URLRewrite to /status              |
| `tlsroute-proxy.yaml`             | tls-proxy      | proxy-gateway :443     | TLS passthrough                    |

### Transitional Traefik bridge

The mothership currently runs Traefik (the OpenOva ingress controller) bound to `45.151.123.50:80/443`. Cilium Gateway is not yet the default IngressController on this cluster. Until it is, the iogrid `Gateway` object is the canonical routing intent — Flux on the iogrid-ops repo materialises it; a separate PR will add a Traefik IngressRoute shim derived from the same JSON record source.

When the mothership migrates to Cilium Gateway (or iogrid moves to its own LB), the Gateway object becomes the live router with zero rewrites.

---

## Provisioning order on a fresh cluster

```bash
# 1. Apply infra/k8s in order (cert-manager assumed already running)
kubectl apply -f infra/k8s/namespaces/iogrid.yaml
kubectl apply -f infra/k8s/cert-manager/cluster-issuer-letsencrypt.yaml
kubectl apply -f infra/k8s/gateways/gateway.yaml
kubectl apply -f infra/k8s/gateways/      # HTTPRoutes + TLSRoute
kubectl apply -f infra/k8s/certificates/  # triggers cert-manager order

# 2. DNS (must come AFTER LB IP allocated; cert solving needs DNS to resolve to the LB)
./scripts/dynadot-apply.sh --apply

# 3. Wait for certs
kubectl -n iogrid wait --for=condition=Ready cert/iogrid-org-tls --timeout=10m
kubectl -n iogrid wait --for=condition=Ready cert/iogrid-proxy-tls --timeout=10m

# 4. Sanity
curl -sI https://app.iogrid.org/ | head -5
curl -sI https://api.iogrid.org/ | head -5
```

---

## What is intentionally NOT here

- **DNSSEC**: Not enabled. Dynadot supports it; we add it once the registrar→authoritative key-handover is automated. Tracked as a follow-up issue.
- **CAA records**: Not yet set. Add `iogrid.org CAA 0 issue "letsencrypt.org"` once the cert issuance is steady-state to prevent rogue CA misissuance.
- **HSTS preload**: Cert pinning is enabled per host via the future Cilium L7 policy. HSTS headers are set by the marketing-site and web services, not at the Gateway.
- **Per-region anycast**: Single-region today (Hetzner Nuremberg via Contabo VPS). Multi-region rolls in once the second mothership exists.
