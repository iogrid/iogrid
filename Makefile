# iogrid top-level Makefile. Hubs into per-area sub-makefiles as they land.
# For now, only proto/ is wired up.

.PHONY: help proto proto-lint proto-format proto-format-check proto-check proto-breaking

help:
	@echo "iogrid Makefile targets:"
	@echo "  make proto              - regenerate Go + TypeScript stubs from proto/"
	@echo "  make proto-lint         - run 'buf lint' over proto/"
	@echo "  make proto-format       - run 'buf format -w' (write in place)"
	@echo "  make proto-format-check - run 'buf format --diff --exit-code' (CI parity)"
	@echo "  make proto-breaking     - run 'buf breaking' against origin/main"
	@echo "  make proto-check        - full CI parity: lint + format-check + generate-and-diff"

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
