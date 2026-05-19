# Releasing the iogrid SDKs

This document describes how to cut a release for each of the four
customer-facing SDKs:

| SDK         | Package coordinate                  | Registry                | Trigger tag                |
|-------------|-------------------------------------|-------------------------|----------------------------|
| TypeScript  | `@iogrid/sdk`                       | npmjs.org               | `sdk-typescript/vX.Y.Z`    |
| Python      | `iogrid`                            | pypi.org                | `sdk-python/vX.Y.Z`        |
| Go          | `github.com/iogrid/go-sdk`          | proxy.golang.org        | `sdks/go/vX.Y.Z`           |
| Java        | `com.iogrid:sdk`                    | Maven Central (OSSRH)   | `sdk-java/vX.Y.Z`          |

Every release is initiated by **pushing a git tag**. The matching
GitHub Actions workflow under `.github/workflows/sdk-<lang>-publish.yml`
takes over from there. **We never publish from a workstation.**

## TL;DR

```bash
# from the repo root
make -C sdks release-ts   VERSION=0.1.0
make -C sdks release-py   VERSION=0.1.0
make -C sdks release-go   VERSION=0.1.0
make -C sdks release-java VERSION=0.1.0
```

Each target runs the SDK's tests locally, bumps the version in the
package metadata, commits, tags, and pushes. Use `DRY_RUN=1` to
preview without pushing.

## Versioning policy

All four SDKs follow [SemVer 2.0](https://semver.org/spec/v2.0.0.html).
The four SDKs version *independently* — bumping the Python package
to `0.2.0` does **not** force a TypeScript bump. The underlying
protobuf contract in `proto/` carries its own back-compat guarantees
(checked by `buf breaking` in CI), which is what keeps the four SDKs
behaviourally aligned.

Pre-release versions use the standard suffix:

* `0.2.0-rc.1` — release candidate (tag: `sdk-typescript/v0.2.0-rc.1`)
* `0.2.0-alpha.3` — alpha
* `0.2.0-beta.2` — beta

The publish workflows treat pre-releases as normal publishes
(`--tag next` on npm, normal upload on PyPI, etc.). For the Java SDK,
versions ending in `SNAPSHOT` land on the OSSRH snapshots repository
instead of Maven Central staging.

## Founder one-time setup

Each registry requires a one-time registration **by the founder**
before any of the publish workflows can succeed. None of these
registrations cost anything; all four are free public packages.

### npm — reserve `@iogrid` scope

1. Log into [npmjs.com](https://www.npmjs.com/) with the founder's
   account (same account that owns the `iogrid` org).
2. Create the `iogrid` organization (free tier).
3. Add the GitHub repository `iogrid/iogrid` as a Trusted Publisher
   under **Org → Settings → Trusted Publishers** (this enables the
   OIDC publish flow used by the workflow).
4. No `NPM_TOKEN` secret is required for a public package on a public
   repo — npm's OIDC + provenance handles the auth.

Validation: `curl -sf https://registry.npmjs.org/-/org/iogrid/user`
returns the org membership.

### PyPI — configure Trusted Publisher

1. Log into [pypi.org](https://pypi.org/) with the founder's account.
2. Pre-reserve the package name by uploading a placeholder `0.0.0`
   build **OR** by adding a Pending Trusted Publisher entry under
   **Your projects → Publishing → Add a pending publisher**:

   | Field            | Value                                          |
   |------------------|------------------------------------------------|
   | PyPI project     | `iogrid`                                       |
   | Owner            | `iogrid`                                       |
   | Repository       | `iogrid`                                       |
   | Workflow         | `sdk-python-publish.yml`                       |
   | Environment      | `pypi`                                         |

3. Once the first `sdk-python/v*` tag is pushed and the workflow
   succeeds, the Pending entry is converted to a permanent one and
   subsequent releases auto-publish.

Validation: `curl -sf https://pypi.org/pypi/iogrid/json` returns
`{"info": {"name": "iogrid", ...}}`.

### Go — no registry to register on

Go modules are served by `proxy.golang.org` which lazily fetches from
GitHub on first request. The workflow warms that cache for us.

Two layout options:

* **Monorepo (current):** the module path is `github.com/iogrid/go-sdk`
  but the source lives under `iogrid/iogrid/sdks/go/`. This is what
  the SDK README documents. Importing works as
  `import "github.com/iogrid/go-sdk"` and `go get` is happy because
  the redirect lives in `vanity.iogrid.org` (Phase 2) or — until
  vanity DNS is wired — in a 1-file `github.com/iogrid/go-sdk` repo
  that re-exports the monorepo path with a `replace` directive.

* **Split repo (alternative):** mirror `sdks/go/` to a dedicated
  `iogrid/go-sdk` GitHub repo with a `release-please`-style sync.
  Pros: cleaner `go get` UX. Cons: two repos to keep in lockstep.

We start with the monorepo layout. If first-customer feedback prefers
split, the migration is a one-time push + tag-rewrite.

### Maven Central — register `com.iogrid` namespace

1. Create a Sonatype account at
   [central.sonatype.org](https://central.sonatype.org/).
2. Open a [namespace registration ticket](https://central.sonatype.org/publish/publish-guide/)
   for `com.iogrid` — requires proving control of `iogrid.org` (DNS
   TXT record or a `META-INF/maven` file served from the domain).
3. Generate a GPG key, publish the public key to a keyserver
   (`gpg --keyserver hkp://keys.openpgp.org --send-keys <KEY_ID>`).
4. Create a Sonatype OSSRH user token under
   **Account → Profile → User Token**.
5. Add the four secrets to the `iogrid/iogrid` repo (under
   `Settings → Secrets and variables → Actions`):

   * `SIGNING_KEY` — ASCII-armoured private key
     (`gpg --armor --export-secret-keys <KEY_ID>`)
   * `SIGNING_PASSWORD` — passphrase for the GPG key
   * `OSSRH_USERNAME` — Sonatype user-token username
   * `OSSRH_TOKEN` — Sonatype user-token password

6. The first ~5 releases require manually closing + releasing the
   staging bucket at <https://s01.oss.sonatype.org/>. After Sonatype
   has observed several successful releases, they enable auto-release.

Validation: `curl -sf "https://search.maven.org/solrsearch/select?q=g:com.iogrid"`
returns a non-empty `docs` array after the first release.

## Per-release procedure

### TypeScript

```bash
# 1. Update CHANGELOG.md (move Unreleased → 0.X.Y).
$EDITOR sdks/typescript/CHANGELOG.md

# 2. Cut the release.
make -C sdks release-ts VERSION=0.1.0
```

The workflow at `.github/workflows/sdk-typescript-publish.yml`:

1. Installs deps via pnpm, runs tests, builds with `tsup`.
2. Asserts `package.json` version matches the tag.
3. Dry-runs publish (prints tarball contents to the action log).
4. Publishes to npm with `--provenance` — GitHub's OIDC token signs
   the publish, npm stores the sigstore bundle.

After a successful release, the package is visible at
<https://www.npmjs.com/package/@iogrid/sdk>.

### Python

```bash
$EDITOR sdks/python/CHANGELOG.md
make -C sdks release-py VERSION=0.1.0
```

The workflow at `.github/workflows/sdk-python-publish.yml`:

1. Installs hatch, runs `hatch run test`.
2. Asserts `hatch version` matches the tag.
3. Builds wheel + sdist with `hatch build`.
4. Publishes via `pypa/gh-action-pypi-publish@release/v1` using the
   Trusted Publisher OIDC flow (no API token).

After a successful release, the package is visible at
<https://pypi.org/project/iogrid/>.

### Go

```bash
$EDITOR sdks/go/CHANGELOG.md
make -C sdks release-go VERSION=0.1.0
```

The workflow at `.github/workflows/sdk-go-publish.yml`:

1. Runs `go test -race`.
2. Warms `proxy.golang.org` and `pkg.go.dev` for the new tag.

After ~30 s the new version is visible at
<https://pkg.go.dev/github.com/iogrid/go-sdk> and installable via
`go get github.com/iogrid/go-sdk@v0.1.0`.

### Java

```bash
$EDITOR sdks/java/CHANGELOG.md
make -C sdks release-java VERSION=0.1.0
```

The workflow at `.github/workflows/sdk-java-publish.yml`:

1. Asserts `build.gradle.kts` version matches the tag.
2. Runs `gradle check`.
3. Signs every artifact (jar, sources, javadoc, pom) with GPG.
4. Uploads to the OSSRH staging repository at
   <https://s01.oss.sonatype.org/>.

Then the founder must:

5. Log into <https://s01.oss.sonatype.org/>.
6. Open **Staging Repositories**.
7. Select the new `comiogrid-NNNN` bucket → **Close** → wait for
   validation (~2 minutes) → **Release**.

After ~30 minutes the artifact is mirrored to Maven Central at
<https://repo.maven.apache.org/maven2/com/iogrid/sdk/>.

## Pre-release checklist

For each release, the cutting engineer confirms:

- [ ] All tests pass locally (`make -C sdks sdk-<lang>`).
- [ ] CHANGELOG.md is updated.
- [ ] The new version is **strictly greater** than the latest
      published version (use `make -C sdks published-<lang>`).
- [ ] Breaking changes (if any) are called out in CHANGELOG.md
      AND covered by a major version bump.
- [ ] The OpenAPI contract has been regenerated if upstream protos
      moved (`make openapi`).
- [ ] Working tree is clean and we're on `main`.

## Rollback

A bad release is rolled forward, not yanked.

* **npm:** `npm deprecate @iogrid/sdk@0.X.Y "use 0.X.Y+1 instead"`.
  Don't `unpublish` — npm forbids it after 72 h and yanking creates
  problems for downstream lockfiles.
* **PyPI:** `pypi yank iogrid 0.X.Y -m "..."` — yanking hides the
  version from new installs but keeps existing lockfiles working.
* **Go:** push a `vX.Y.Z+incompatible` retract directive to `go.mod`
  in a new tag.
* **Maven Central:** central artifacts are immutable. Publish a new
  patch version with the fix.

## Future work

* **Changesets** for TypeScript (replaces manual CHANGELOG.md
  bookkeeping with `pnpm changeset` workflow).
* **towncrier** for Python (replaces manual CHANGELOG.md with
  per-feature news fragments).
* **release-please** for automating the version-bump PR for all four
  SDKs from conventional-commit history.
* **Vanity import path** `pkg.iogrid.org/sdk` for the Go SDK so the
  Go import path is registry-agnostic.

Tracked in EPIC #74 (Customer-facing API + OpenAPI spec).
