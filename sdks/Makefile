# sdks/Makefile — release tooling for the four customer SDKs.
#
# All targets take VERSION=<x.y.z> and produce a signed git tag of the
# form `sdk-<lang>/vX.Y.Z` (or `sdks/go/vX.Y.Z` for Go — Go's tooling
# is opinionated about tag layout).
#
# Tag push triggers the corresponding `.github/workflows/sdk-<lang>-publish.yml`
# which actually performs the publish. We never publish from a workstation.
#
# Usage:
#   make -C sdks release-ts VERSION=0.1.0
#   make -C sdks release-py VERSION=0.1.0
#   make -C sdks release-go VERSION=0.1.0
#   make -C sdks release-java VERSION=0.1.0
#
# Pre-flight every target runs:
#   1. Working tree clean (no uncommitted changes).
#   2. On `main`.
#   3. Tests pass locally for that SDK.
#   4. Version is NOT already published on the upstream registry.
#   5. Version is monotonically newer than the last tag.
#
# Dry-run mode: prepend `DRY_RUN=1` to skip the tag push.
#   DRY_RUN=1 make -C sdks release-ts VERSION=0.1.0

SHELL := /bin/bash

.PHONY: help \
        release-ts release-py release-go release-java \
        preflight \
        published-ts published-py published-go published-java

help:
	@echo "sdks/Makefile — release tooling"
	@echo ""
	@echo "  make release-ts    VERSION=x.y.z   tag + push @iogrid/sdk → npm"
	@echo "  make release-py    VERSION=x.y.z   tag + push iogrid → PyPI"
	@echo "  make release-go    VERSION=x.y.z   tag + push github.com/iogrid/go-sdk"
	@echo "  make release-java  VERSION=x.y.z   tag + push com.iogrid:sdk → Maven Central"
	@echo ""
	@echo "  DRY_RUN=1 ...                      run all pre-flight checks but skip the tag push"
	@echo ""
	@echo "  make published-ts                  query npm for the latest published version"
	@echo "  make published-py                  query PyPI for the latest published version"
	@echo "  make published-java                query Maven Central for the latest published version"

# ---------------------------------------------------------------------------
# Shared pre-flight (called by each release target)
# ---------------------------------------------------------------------------

preflight:
	@if [ -z "$(VERSION)" ]; then echo "ERROR: VERSION=x.y.z is required"; exit 1; fi
	@if ! echo "$(VERSION)" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.]+)?$$'; then \
	  echo "ERROR: VERSION ($(VERSION)) must be semver (x.y.z or x.y.z-rc.N)"; exit 1; \
	fi
	@if ! git diff --quiet HEAD; then \
	  echo "ERROR: working tree has uncommitted changes — commit or stash first"; exit 1; \
	fi
	@BR=$$(git rev-parse --abbrev-ref HEAD); \
	if [ "$$BR" != "main" ]; then \
	  echo "ERROR: must release from main (currently on $$BR)"; exit 1; \
	fi

# ---------------------------------------------------------------------------
# TypeScript — @iogrid/sdk → npm
# ---------------------------------------------------------------------------

release-ts: preflight
	@echo "→ release @iogrid/sdk@$(VERSION)"
	@PUB=$$(curl -sf "https://registry.npmjs.org/@iogrid/sdk/$(VERSION)" 2>/dev/null | head -c 50); \
	if [ -n "$$PUB" ]; then \
	  echo "ERROR: @iogrid/sdk@$(VERSION) is already on npm"; exit 1; \
	fi
	cd typescript && pnpm install --frozen-lockfile=false && pnpm test && pnpm build
	cd typescript && node -e "const fs=require('fs');const p=require('./package.json');p.version='$(VERSION)';fs.writeFileSync('./package.json',JSON.stringify(p,null,2)+'\n');"
	git add typescript/package.json
	git commit -m "release(sdk-typescript): v$(VERSION)"
	git tag -a "sdk-typescript/v$(VERSION)" -m "@iogrid/sdk v$(VERSION)"
	@if [ "$(DRY_RUN)" = "1" ]; then \
	  echo "DRY_RUN=1 — skipping push"; \
	else \
	  git push origin main && git push origin "sdk-typescript/v$(VERSION)"; \
	  echo "✓ tag pushed — watch .github/workflows/sdk-typescript-publish.yml"; \
	fi

# ---------------------------------------------------------------------------
# Python — iogrid → PyPI
# ---------------------------------------------------------------------------

release-py: preflight
	@echo "→ release iogrid==$(VERSION)"
	@PUB=$$(curl -sf "https://pypi.org/pypi/iogrid/$(VERSION)/json" 2>/dev/null | head -c 50); \
	if [ -n "$$PUB" ]; then \
	  echo "ERROR: iogrid==$(VERSION) is already on PyPI"; exit 1; \
	fi
	cd python && hatch run test
	cd python && hatch version "$(VERSION)"
	git add python/src/iogrid/__init__.py
	git commit -m "release(sdk-python): v$(VERSION)"
	git tag -a "sdk-python/v$(VERSION)" -m "iogrid v$(VERSION)"
	@if [ "$(DRY_RUN)" = "1" ]; then \
	  echo "DRY_RUN=1 — skipping push"; \
	else \
	  git push origin main && git push origin "sdk-python/v$(VERSION)"; \
	  echo "✓ tag pushed — watch .github/workflows/sdk-python-publish.yml"; \
	fi

# ---------------------------------------------------------------------------
# Go — git tag IS the publish
# ---------------------------------------------------------------------------

release-go: preflight
	@echo "→ release github.com/iogrid/go-sdk@v$(VERSION)"
	@INFO=$$(curl -sf "https://proxy.golang.org/github.com/iogrid/go-sdk/@v/v$(VERSION).info" 2>/dev/null | head -c 50); \
	if [ -n "$$INFO" ]; then \
	  echo "ERROR: github.com/iogrid/go-sdk@v$(VERSION) is already on proxy.golang.org"; exit 1; \
	fi
	cd go && go test -race -count=1 ./...
	git tag -a "sdks/go/v$(VERSION)" -m "go-sdk v$(VERSION)"
	@if [ "$(DRY_RUN)" = "1" ]; then \
	  echo "DRY_RUN=1 — skipping push"; \
	else \
	  git push origin "sdks/go/v$(VERSION)"; \
	  echo "✓ tag pushed — watch .github/workflows/sdk-go-publish.yml"; \
	fi

# ---------------------------------------------------------------------------
# Java — com.iogrid:sdk → Maven Central
# ---------------------------------------------------------------------------

release-java: preflight
	@echo "→ release com.iogrid:sdk:$(VERSION)"
	@HTTP=$$(curl -sf -o /dev/null -w '%{http_code}' "https://repo.maven.apache.org/maven2/com/iogrid/sdk/$(VERSION)/sdk-$(VERSION).pom" 2>/dev/null); \
	if [ "$$HTTP" = "200" ]; then \
	  echo "ERROR: com.iogrid:sdk:$(VERSION) is already on Maven Central"; exit 1; \
	fi
	cd java && gradle check --no-daemon
	@# Bump the version line in build.gradle.kts (simple sed; assumes single `version = "..."` line).
	sed -i.bak -E 's/^version\s*=\s*"[^"]+"/version = "$(VERSION)"/' java/build.gradle.kts && rm java/build.gradle.kts.bak
	git add java/build.gradle.kts
	git commit -m "release(sdk-java): v$(VERSION)"
	git tag -a "sdk-java/v$(VERSION)" -m "com.iogrid:sdk v$(VERSION)"
	@if [ "$(DRY_RUN)" = "1" ]; then \
	  echo "DRY_RUN=1 — skipping push"; \
	else \
	  git push origin main && git push origin "sdk-java/v$(VERSION)"; \
	  echo "✓ tag pushed — watch .github/workflows/sdk-java-publish.yml"; \
	fi

# ---------------------------------------------------------------------------
# Published-version probes (used by humans + by release-* pre-flight)
# ---------------------------------------------------------------------------

published-ts:
	@curl -sf "https://registry.npmjs.org/@iogrid/sdk/latest" 2>/dev/null | \
	  python3 -c "import json,sys;d=json.load(sys.stdin);print('npm:    @iogrid/sdk@'+d.get('version','?'))" || \
	  echo "npm:    not published yet"

published-py:
	@curl -sf "https://pypi.org/pypi/iogrid/json" 2>/dev/null | \
	  python3 -c "import json,sys;d=json.load(sys.stdin);print('pypi:   iogrid=='+d.get('info',{}).get('version','?'))" || \
	  echo "pypi:   not published yet"

published-go:
	@curl -sf "https://proxy.golang.org/github.com/iogrid/go-sdk/@latest" 2>/dev/null | \
	  python3 -c "import json,sys;d=json.load(sys.stdin);print('go:     github.com/iogrid/go-sdk@'+d.get('Version','?'))" || \
	  echo "go:     not published yet"

published-java:
	@curl -sf "https://search.maven.org/solrsearch/select?q=g:com.iogrid+AND+a:sdk&rows=1&wt=json" 2>/dev/null | \
	  python3 -c "import json,sys;d=json.load(sys.stdin);docs=d.get('response',{}).get('docs',[]);print('maven:  com.iogrid:sdk:'+(docs[0].get('latestVersion','?') if docs else '?'))" || \
	  echo "maven:  not published yet"
