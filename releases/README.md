# releases — `releases.iogrid.org` redirect server

A 5-line nginx image whose only job is to translate the branded
`releases.iogrid.org/...` URL space into 302 redirects pointing at the
underlying `github.com/iogrid/iogrid/releases/...` asset URLs.

We deliberately do **not** mirror release blobs on-cluster:

- GitHub Releases is the source-of-truth blob store. Mirroring would
  double the storage bill, double the surface to keep in sync, and add
  a second integrity-check layer (the Sparkle ed25519 signature
  already gives us provenance once the appcast resolves to a .pkg).
- A 302 to `objects.githubusercontent.com` is served from GitHub's
  CDN — better global latency than a single-region pod could offer.
- Egress from the pod is locked to cluster DNS only (see
  `infra/k8s/base/releases/networkpolicy.yaml`); the redirect chain
  never traverses our infrastructure for the bytes themselves.

## URL surface

| Path                                                    | Behaviour                                                                          |
| ------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `/healthz`                                              | 200 OK, used by k8s probes.                                                        |
| `/macos/appcast.xml`                                    | 302 → `github.com/iogrid/iogrid/releases/latest/download/appcast.xml`              |
| `/macos/<version>/iogrid-<version>-<arch>.pkg`          | 302 → `github.com/iogrid/iogrid/releases/download/<version>/iogrid-<version>-<arch>.pkg` |
| `/latest/iogrid-<os>-<arch>.<ext>`                      | 302 → `github.com/iogrid/iogrid/releases/latest/download/iogrid-<os>-<arch>.<ext>`   |
| `/`                                                     | 200 HTML stub linking to the GH Releases page.                                     |

## Verifying

```bash
# Once releases-ci has shipped the image + deploy-bot has bumped the
# manifest pin + cert-manager has re-issued iogrid-org-tls with the
# releases.iogrid.org SAN:

# Sparkle appcast (returns 302 even if no release exists yet — that's
# fine; Sparkle treats the eventual 404 as "no updates available").
curl -sI https://releases.iogrid.org/macos/appcast.xml | grep -E '^(HTTP|location)'

# Versioned .pkg (replace v0.1.1 with a real tag).
curl -sI https://releases.iogrid.org/macos/v0.1.1/iogrid-0.1.1-arm64.pkg | grep -E '^(HTTP|location)'

# Phase-0 legacy.
curl -sI https://releases.iogrid.org/latest/iogrid-darwin-arm64.pkg | grep -E '^(HTTP|location)'
```

## How releases get published

- `daemon-ci.yml` → `generate-appcast` job: on `v*` tag push, downloads
  the matching `installer-ci` .pkg artifacts, signs the appcast with
  the Sparkle ed25519 key, and uploads `appcast.xml` + both .pkg files
  as **GitHub Release assets** on the tag.
- `releases.iogrid.org/macos/appcast.xml` then resolves (via 302) to
  the freshly uploaded asset.

## Refs

- Issue #392 — wires this up.
- Issue #348 — parent EPIC (macOS auto-update via Sparkle).
- PR #387 — generates the appcast.xml in CI (Phase 1).
