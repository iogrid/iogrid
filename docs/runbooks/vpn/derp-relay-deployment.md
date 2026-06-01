# DERP relay deployment — Phase-4 hardening (Closes #521)

> Encrypted-WG-packet relay for customer ↔ provider pairs that can't
> establish a direct ICE path. Code lives in
> `coordinator/services/vpn-gateway/internal/derp`. This runbook
> covers when to enable it, how to deploy it, and the client-side
> wiring needed to use it.

## When to enable

The Phase-1 ICE flow handles the majority of residential NAT
configurations directly. Customers that fall through (predominantly
symmetric-NAT both sides, double-CGNAT carriers, dual-stack
mismatches) currently see "session failed" with no recourse. The
relay is the fallback.

**Don't deploy preemptively.** Relay traffic carries 100% of customer
bandwidth at the operator's expense; running it for everyone burns
the unit economics. Enable when:

1. The session-failure rate visible in `vpn-svc` `/metrics`
   (`vpn_session_failed_total{reason="ice_no_candidates"}`) exceeds
   5% sustained over a week, OR
2. Customer support tickets named-pattern "VPN won't connect from
   <ISP>" exceed 3 in a week from one geographic region.

Either signal indicates symmetric-NAT customers are present. Until
then the relay is idle infrastructure cost.

## Architecture

```
                              ┌──────────────┐
                              │ DERP relay   │
                              │ (per region) │
            ICE fails         │              │      ICE fails
   ┌──────────────────────────│ TCP/TLS      │──────────────────────────┐
   │                          │ frame router │                          │
   ▼                          └──────────────┘                          ▼
┌───────┐                                                          ┌────────┐
│ Cust  │←──────── ICE direct (preferred) ────────→ ✗ ✗ ✗ ←──────→│ Provider│
│ SDK   │                                                          │ daemon │
└───────┘                                                          └────────┘
```

- One relay per region (matches the existing `vpn-gateway`
  per-region deployment topology).
- TLS-terminated TCP, length-prefixed binary frames.
- Server is OPAQUE — it never decrypts WG payloads. Compromise of
  the relay box leaks ciphertext + traffic timing, never plaintext.
- Frame format documented in `relay.go` package comment.

## Deploy

1. **Build vpn-gateway** with no extra flags — the relay is
   compiled in unconditionally; the listener starts only if the env
   variable below is set.

2. **k8s wiring.** Add a Deployment env var to
   `infra/k8s/base/vpn-gateway/deployment.yaml`:

   ```yaml
   env:
   - name: DERP_LISTEN_ADDR
     value: ":51821"
   ```

   And a LoadBalancer Service exposing TCP :51821 to the public
   internet. TLS is terminated at the LB (existing cert-manager
   wildcard cert covers `*.iogrid.org`).

3. **DNS.** Publish `derp-<region>.iogrid.org` → LB IP via the
   existing external-dns operator.

4. **/v1/vpn/regions discovery.** Add `derp_url` field on each
   region row in vpn-svc — clients pick it up automatically.

5. **Client-side wiring.** Customer SDK + provider daemon already
   carry the `Tunnel` abstraction; the relay candidate type
   (`type=relay`) was added to the proto earlier in this session.
   Coordinator returns a `relay` ICE candidate alongside `host` /
   `srflx` / `relay` candidates when the operator-side
   `DERP_ENABLED=1` flag is set on vpn-svc; the SDK prefers direct
   paths and falls back to the relay only after N seconds of failed
   ICE checks.

## Operator levers

- **Hard-off:** unset `DERP_LISTEN_ADDR` + redeploy. Existing
  sessions on relay die immediately; new sessions get only direct
  candidates.
- **Drain:** scale the relay's Deployment to 0 with a `preStop`
  hook that closes the relay's listener — existing sessions drop
  cleanly; new ones miss this region's relay and try the next.
- **Per-region disable:** flip `derp_url` to empty in
  vpn-svc's regions config — clients stop discovering this region's
  relay without redeploying.

## Wire format

See the package doc-comment in `relay.go`. Three frame kinds:

| Kind | Direction | Payload |
|---|---|---|
| 0x01 REGISTER | client → relay | empty; peer field = sender's static pubkey |
| 0x02 DATA | bidirectional | encrypted WG datagram, ≤ 2 KiB |
| 0x03 PEER_GONE | relay → client | empty; peer field = absent peer's pubkey |

Frames are little-endian 16-bit length-prefixed inside a 35-byte
header (1 byte kind + 32 byte peer + 2 byte length). Max payload is
`MaxFrameBytes = 2048`.

## Tests

`coordinator/services/vpn-gateway/internal/derp/relay_test.go` —
4 cases cover the protocol surface end-to-end:

- bidirectional DATA forwarding
- PEER_GONE on unknown destination
- oversize-frame rejection
- duplicate-REGISTER rejection

Run: `go test ./coordinator/services/vpn-gateway/internal/derp/`.
