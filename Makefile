# iogrid top-level Makefile. Hubs into per-area sub-makefiles as they land.
# Wires proto/ + the customer SDK pipelines (sdks/).

.PHONY: help proto proto-lint proto-format proto-format-check proto-check proto-breaking \
        openapi openapi-lint sdks sdk-typescript sdk-python sdk-go sdk-java

help:
	@echo "iogrid Makefile targets:"
	@echo "  make proto              - regenerate Go + TypeScript stubs from proto/"
	@echo "  make proto-lint         - run 'buf lint' over proto/"
	@echo "  make proto-format       - run 'buf format -w' (write in place)"
	@echo "  make proto-format-check - run 'buf format --diff --exit-code' (CI parity)"
	@echo "  make proto-breaking     - run 'buf breaking' against origin/main"
	@echo "  make proto-check        - full CI parity: lint + format-check + generate-and-diff"
	@echo "  make openapi            - regenerate OpenAPI 3.1 spec from proto/"
	@echo "  make openapi-lint       - run Spectral over the OpenAPI spec"
	@echo "  make sdks               - build all customer SDKs (typescript+python+go+java)"
	@echo "  make sdk-typescript     - build the @iogrid/sdk npm package"
	@echo "  make sdk-python         - build the iogrid PyPI package"
	@echo "  make sdk-go             - go vet + test for the Go SDK"
	@echo "  make sdk-java           - gradle build for the Java SDK"
	@echo "  make vpn-test           - VPN P2P Phase 1 full test suite (#504)"

proto:
	cd proto && buf generate

proto-lint:
	cd proto && buf lint

proto-format:
	cd proto && buf format -w

proto-format-check:
	cd proto && buf format --diff --exit-code

proto-breaking:
	cd proto && buf breaking --against '../.git#branch=main,subdir=proto'

proto-check: proto-lint proto-format-check
	cd proto && buf generate
	@if ! git diff --quiet -- coordinator/internal/pb web/src/lib/pb; then \
		echo "ERROR: 'buf generate' produced a diff — commit the regenerated stubs."; \
		git --no-pager diff --stat -- coordinator/internal/pb web/src/lib/pb; \
		exit 1; \
	fi
	@echo "proto-check OK"

openapi:
	cd proto && buf generate --template buf.gen.openapi.yaml

openapi-lint:
	cd proto/gen/openapi && spectral lint iogrid.yaml --ruleset .spectral.yaml

sdks: sdk-typescript sdk-python sdk-go sdk-java
	@echo "All SDKs built."

sdk-typescript:
	cd sdks/typescript && pnpm install --frozen-lockfile && pnpm test && pnpm build

sdk-python:
	cd sdks/python && hatch env create && hatch run test

sdk-go:
	cd sdks/go && go vet ./... && go test ./...

sdk-java:
	cd sdks/java && ./gradlew check

# --- VPN P2P Phase 1 (EPIC #504) -------------------------------------------
# `make vpn-test` runs the full Phase 1 test suite (SDK + Coordinator)
# in one go — useful for quickly validating any VPN-touching change.
.PHONY: vpn-test vpn-test-sdk vpn-test-coordinator

vpn-test: vpn-test-sdk vpn-test-coordinator
	@echo
	@echo "✓ all VPN Phase 1 tests passed"

vpn-test-sdk:
	@echo "=== sdks/go/vpn ==="
	cd sdks/go/vpn && go test ./...

vpn-test-coordinator:
	@echo "=== coordinator/services/vpn-svc ==="
	cd coordinator && go test ./services/vpn-svc/...
