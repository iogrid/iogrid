# JWT signing keypair rotation

> Operator runbook for rotating identity-svc's persistent JWT signing
> keypair without signing every active user out. Follows from #452.

## When to rotate

- **Scheduled**: every 90 days (quarterly).
- **Forced**: any time the private key was exposed (laptop loss, leaked
  shell, suspicious access pattern in audit log).
- **First time**: only after `scripts/identity-svc-jwt-keypair-gen.sh` +
  kubeseal land the initial SealedSecret + `JWT_KEYPAIR_AUTOGEN=1` is
  removed from `infra/k8s/base/identity-svc/deployment.yaml`.

## Why dual-key (not blue/green)

identity-svc's JWT validator accepts a comma-separated list of public
keys. During a rotation window:

1. **T+0**: append the NEW public key to the validator list; signing
   still uses the OLD private key. Both old + new keys are valid for
   verification.
2. **T+5 min**: swap the signing key to the NEW private key. Outstanding
   JWTs signed with the OLD key continue to verify (validator still
   accepts both).
3. **T+access-token-TTL (1h)**: every active JWT has been minted with
   the NEW key. Refresh-tokens carry a kid that auto-rotates on first
   use.
4. **T+refresh-token-TTL (30 days)**: every active session has refreshed
   at least once. Safe to drop the OLD public key from the validator.

Total user-facing impact: zero forced sign-outs (vs blue/green which
would invalidate every outstanding session).

## Step-by-step

```bash
# 1. Generate the new keypair offline (founder's laptop).
./scripts/identity-svc-jwt-keypair-gen.sh ~/iogrid-jwt-keys-rotation-2026q3

# 2. Seal the new keypair under a versioned name.
kubectl -n iogrid create secret generic identity-svc-jwt-2026q3 \
  --from-file=private.pem=~/iogrid-jwt-keys-rotation-2026q3/private.pem \
  --from-file=public.pem=~/iogrid-jwt-keys-rotation-2026q3/public.pem \
  --dry-run=client -o yaml | \
  kubeseal --controller-namespace sealed-secrets -o yaml \
    > infra/k8s/base/identity-svc/sealed-jwt-2026q3.yaml

# 3. Append the new public key to identity-svc's validator env.
#    (Edit infra/k8s/base/identity-svc/deployment.yaml; add a new
#    JWT_VERIFIER_PUBLIC_KEY_PATHS env that lists both old + new pem
#    paths; mount both Secrets.)
git add infra/k8s/base/identity-svc/{sealed-jwt-2026q3.yaml,deployment.yaml}
git commit -m "ops(identity-svc): begin JWT rotation 2026q3 — accept both old + new keys"
git push

# 4. Apply the change and roll identity-svc. iogrid is NOT Flux-wired,
#    so the git push above does NOT auto-reconcile — apply the manifest
#    yourself, then restart the deployment:
#      kubectl -n iogrid apply -f infra/k8s/base/identity-svc/sealed-jwt-2026q3.yaml
#      kubectl -n iogrid apply -f infra/k8s/base/identity-svc/deployment.yaml
#      kubectl -n iogrid rollout restart deploy/identity-svc
#    Verify both keys are loaded:
#      kubectl -n iogrid logs deploy/identity-svc | grep 'jwt verifier loaded'

# 5. (5 min later, traffic stable) Swap the SIGNING key to the new pem.
#    Edit infra/k8s/base/identity-svc/deployment.yaml — change
#    JWT_PRIVATE_KEY_PATH from /etc/identity/jwt/private.pem to
#    /etc/identity/jwt-2026q3/private.pem.
git commit -am "ops(identity-svc): JWT rotation 2026q3 — swap signer to new key"
git push

# 6. (30 days later, refresh-token TTL drained) Drop the old key.
git rm infra/k8s/base/identity-svc/sealed-jwt-2026q2.yaml  # last quarter's
# edit deployment.yaml to remove the old verifier-path entry
git commit -am "ops(identity-svc): JWT rotation 2026q3 — drop old key after refresh-token drain"
git push
```

## Verification

After step 3 + 5:

```bash
# Sign in fresh + decode a new token — kid header should be the new key's
# sha256 (printed by identity-svc-jwt-keypair-gen.sh).
curl -sS -c /tmp/cookies.txt -d 'email=hatice.yildiz@openova.io' \
  https://iogrid.org/api/v1/account/sign-in/magic
# (click the email; complete the flow)
jwt=$(curl -sS -b /tmp/cookies.txt https://iogrid.org/api/v1/me \
  | jq -r '.access_token')
echo "$jwt" | cut -d. -f1 | base64 -d | jq .kid
# → matches the new key's fingerprint from generate-keypair output
```

After step 6:

```bash
# Verify the old kid no longer appears in any active JWT.
kubectl -n iogrid logs deploy/identity-svc --since=24h | grep -i 'kid=' | sort -u
# → only the new kid should appear
```

## Failure modes

| Symptom | Likely cause | Recovery |
|---|---|---|
| All users signed out after step 5 | Validator missing the new key (step 3 didn't land before step 5) | `git revert` step 5; verify step 3's commit reached the cluster |
| 500s on `/api/v1/me` | Both pem files exist but mount permissions wrong (need 0400 readable by user 65532) | Re-apply the SealedSecret with correct `defaultMode`; restart pods |
| Refresh-token rejection after step 6 | Some refresh tokens were minted with the old key + last-used > 30d ago (rare but possible) | Force-sign-out those sessions; the user re-signs in cleanly |

## Refs

- [#452](https://github.com/iogrid/iogrid/issues/452) — root issue (JWT_KEYPAIR_AUTOGEN=1 in prod)
- `scripts/identity-svc-jwt-keypair-gen.sh` — initial + rotation keypair generator
- `coordinator/services/identity-svc/internal/tokens/` — validator + signer code (multi-key support already in place)
