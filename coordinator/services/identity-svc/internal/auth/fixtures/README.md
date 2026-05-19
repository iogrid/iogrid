# JWT dev fixtures

**These keys are test fixtures. They are committed to git, public, and
MUST NEVER be used in any environment that issues tokens consumed by a
real user, gateway, or downstream service.**

| File | Purpose |
|------|---------|
| `jwt_test.key` | RS256 private key (2048-bit PEM, PKCS#8) — signs access tokens in unit + integration tests |
| `jwt_test.pub` | Corresponding public key for verification |

## Usage

### Unit / integration tests

`internal/auth/testing.go` exposes a helper that loads these files from
disk for tests that need a deterministic keypair. Tests that just need
*any* signer should call `tokens.NewSignerFromKeys(rsa.GenerateKey(...))`
instead — fresh keys are cheaper than file IO.

### Local-dev boot (option A — point env at the fixture)

```bash
export JWT_PRIVATE_KEY_PATH=$(git rev-parse --show-toplevel)/coordinator/services/identity-svc/internal/auth/fixtures/jwt_test.key
export JWT_PUBLIC_KEY_PATH=$(git rev-parse --show-toplevel)/coordinator/services/identity-svc/internal/auth/fixtures/jwt_test.pub
go run ./cmd/identity-svc
```

### Local-dev boot (option B — autogen ephemeral)

```bash
export JWT_KEYPAIR_AUTOGEN=1
# Optional — defaults to /tmp/jwt-keys (writeable on read-only-root pods
# via an emptyDir mount at /tmp/jwt-keys; see infra/k8s/base/identity-svc).
export JWT_AUTOGEN_DIR=/tmp/jwt-keys
go run ./cmd/identity-svc
```

In autogen mode the binary generates a fresh RSA-2048 keypair at boot,
writes both PEM files to `$JWT_AUTOGEN_DIR`, logs a loud WARNING, and
points `JWT_PRIVATE_KEY_PATH` / `JWT_PUBLIC_KEY_PATH` at the generated
files. Tokens are valid only for the lifetime of the pod — verifiers
that cached the previous public key will reject them after a restart.

## Rotation

Re-generate with OpenSSL whenever a sec audit calls for it (no schedule
yet — these keys never touch prod):

```bash
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out jwt_test.key
openssl rsa -pubout -in jwt_test.key -out jwt_test.pub
```
