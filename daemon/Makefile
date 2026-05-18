# iogridd workspace Makefile
#
# All targets must be runnable from `daemon/`. CI mirrors these targets.

CARGO ?= cargo
WORKSPACE := --workspace

.PHONY: help fmt fmt-check lint test check build build-release clean \
        build-release-aarch64-apple-darwin \
        build-release-x86_64-apple-darwin \
        build-release-x86_64-unknown-linux-gnu \
        build-release-aarch64-unknown-linux-gnu \
        build-release-x86_64-pc-windows-msvc

help: ## Show this help.
	@awk 'BEGIN{FS=":.*##"} /^[a-zA-Z0-9_-]+:.*##/{printf "  %-45s %s\n",$$1,$$2}' $(MAKEFILE_LIST)

fmt: ## Format the workspace.
	$(CARGO) fmt --all

fmt-check: ## Check formatting only.
	$(CARGO) fmt --all -- --check

lint: ## Clippy with -D warnings (workspace-wide, all targets).
	$(CARGO) clippy $(WORKSPACE) --all-targets -- -D warnings

test: ## Run the workspace test suite.
	$(CARGO) test $(WORKSPACE) --all-targets

check: ## Cheap compile check.
	$(CARGO) check $(WORKSPACE) --all-targets

build: ## Debug build of everything.
	$(CARGO) build $(WORKSPACE) --all-targets

build-release: ## Release build of everything for the host target.
	$(CARGO) build $(WORKSPACE) --release

# ---------------------------------------------------------------------------
# Per-target release builds. Run on the matching host (CI uses GitHub Actions
# `runs-on` to select the right runner OS); local cross-compile needs the
# vendor toolchain installed (e.g. `messense/macos-cross-toolchains` brew tap
# for the apple targets, `mingw-w64` for the windows-gnu target).
# ---------------------------------------------------------------------------

build-release-aarch64-apple-darwin: ## Apple Silicon macOS release build.
	$(CARGO) build $(WORKSPACE) --release --target aarch64-apple-darwin

build-release-x86_64-apple-darwin: ## Intel macOS release build.
	$(CARGO) build $(WORKSPACE) --release --target x86_64-apple-darwin

build-release-x86_64-unknown-linux-gnu: ## x86_64 Linux release build.
	$(CARGO) build $(WORKSPACE) --release --target x86_64-unknown-linux-gnu

build-release-aarch64-unknown-linux-gnu: ## aarch64 Linux release build.
	$(CARGO) build $(WORKSPACE) --release --target aarch64-unknown-linux-gnu

build-release-x86_64-pc-windows-msvc: ## Windows x86_64 MSVC release build.
	$(CARGO) build $(WORKSPACE) --release --target x86_64-pc-windows-msvc

clean: ## Remove build artifacts.
	$(CARGO) clean
