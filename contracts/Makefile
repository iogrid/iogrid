# contracts/Makefile — $GRID Anchor workspace
#
# Founder/engineer-facing convenience targets. Wraps the scripts under scripts/ and the
# Anchor CLI so a new contributor (or auditor) can get from clone to a running local
# validator in 3 commands:
#
#   cd contracts
#   make install
#   make dev
#
# Targets are intentionally orthogonal — each does ONE thing — so they compose in CI.

# Defaults
KEYPAIR     ?= $(HOME)/.config/solana/id.json
CLUSTER     ?= devnet            # devnet | mainnet-beta (for production targets)
PROGRAM     ?=                   # optional: single program targeting (grid-token, emission, ...)
ANCHOR      ?= anchor
SOLANA      ?= solana
CARGO       ?= cargo
YARN        ?= yarn
WORKSPACE   := $(shell pwd)
PROGRAMS    := grid_token emission vesting staking burn

.PHONY: help install build test clippy fmt fmt-check clean dev dev-build dev-clean \
        deploy-devnet deploy-mainnet idl-init idl-upgrade airdrop audit-export \
        ledger-clean validator-stop upgrade

help:
	@echo "$(notdir $(WORKSPACE))/Makefile — common targets"
	@echo
	@echo "Developer flow:"
	@echo "  make install      Install JS deps (anchor TS client, mocha, etc.)"
	@echo "  make build        anchor build (compile all 5 programs to BPF)"
	@echo "  make test         anchor test --skip-deploy (math/IDL suite, fast)"
	@echo "  make dev          Boot a local validator with all 5 programs deployed"
	@echo "  make dev-build    Build, then boot the local validator (slower)"
	@echo "  make dev-clean    Clean ledger, then boot"
	@echo "  make clippy       cargo clippy --workspace --all-targets -- -D warnings"
	@echo "  make fmt          cargo fmt + anchor TS prettier"
	@echo "  make clean        cargo clean + rm target/idl target/types"
	@echo
	@echo "Deployment:"
	@echo "  make deploy-devnet         Interactive devnet deploy of all 5 programs"
	@echo "  make deploy-mainnet        Interactive mainnet deploy (requires CONFIRM_MAINNET)"
	@echo "  make idl-init CLUSTER=devnet     One-time IDL publish to <CLUSTER>"
	@echo "  make idl-upgrade CLUSTER=devnet  Refresh on-chain IDL after a rebuild"
	@echo "  make upgrade PROGRAM=emission CLUSTER=devnet   Re-deploy one program"
	@echo "  make airdrop                     Devnet SOL airdrop (loops until balance >= 10)"
	@echo
	@echo "Audit:"
	@echo "  make audit-export                Bundle everything an auditor needs into ./audit-bundle.tar.gz"
	@echo
	@echo "Variables:"
	@echo "  KEYPAIR   = $(KEYPAIR)"
	@echo "  CLUSTER   = $(CLUSTER)"
	@echo "  PROGRAM   = $(PROGRAM)"

install:
	$(YARN) install

build:
	$(ANCHOR) build

test: build
	$(ANCHOR) test --skip-deploy

clippy:
	$(CARGO) clippy --workspace --all-targets -- -D warnings

fmt:
	$(CARGO) fmt --all
	-$(YARN) prettier --write 'tests/**/*.ts' 'migrations/**/*.ts'

fmt-check:
	$(CARGO) fmt --all -- --check

clean:
	$(CARGO) clean
	rm -rf target/idl target/types

# --- Developer local validator ---

dev:
	./scripts/local-validator.sh --quiet

dev-build:
	./scripts/local-validator.sh --build --quiet

dev-clean:
	./scripts/local-validator.sh --clean --quiet

validator-stop:
	-pkill -f solana-test-validator || true

ledger-clean:
	rm -rf $(WORKSPACE)/test-ledger

# --- Deployment ---

deploy-devnet: build
	./scripts/devnet-deploy.sh --keypair $(KEYPAIR) $(if $(PROGRAM),--program $(PROGRAM))

deploy-mainnet: build
	@if [ "$(CONFIRM_MAINNET)" != "I_REALLY_MEAN_IT" ]; then \
		echo "Refusing mainnet deploy without CONFIRM_MAINNET=I_REALLY_MEAN_IT"; exit 3; \
	fi
	@echo "Mainnet deploy: use scripts/devnet-deploy.sh-pattern with cluster=mainnet-beta"
	@echo "or invoke ./scripts/upgrade.sh per program. See contracts/README.md §Deploy."
	@exit 4

idl-init:
	./scripts/idl-publish.sh --cluster $(CLUSTER) --keypair $(KEYPAIR) --init \
	  $(if $(PROGRAM),--program $(PROGRAM))

idl-upgrade:
	./scripts/idl-publish.sh --cluster $(CLUSTER) --keypair $(KEYPAIR) --upgrade \
	  $(if $(PROGRAM),--program $(PROGRAM))

upgrade:
	@if [ -z "$(PROGRAM)" ]; then echo "PROGRAM=<grid-token|emission|vesting|staking|burn> required"; exit 2; fi
	./scripts/upgrade.sh $(PROGRAM) $(CLUSTER) --keypair $(KEYPAIR)

airdrop:
	./scripts/airdrop.sh --cluster devnet --keypair $(KEYPAIR) --amount 10

# --- Audit ---
#
# audit-export produces a single tarball containing everything an auditor needs to start
# work without having to clone the full mono-repo:
#   * Rust source for all 5 programs
#   * Cargo.toml (workspace + per-crate)
#   * Anchor.toml + Anchor.lock
#   * IDLs (target/idl/*.json) and types (target/types/*.ts) if built
#   * Tests (tests/*.ts)
#   * Audit docs (audit/*.md)
#   * sha256 manifest of every file included
#
# The auditor can then sign the manifest hash to commit to the exact bytes reviewed.

AUDIT_BUNDLE_NAME ?= audit-bundle-$(shell date -u +%Y%m%d-%H%M%S).tar.gz
AUDIT_BUNDLE_ROOT := /tmp/iogrid-audit-bundle

audit-export: build
	@rm -rf $(AUDIT_BUNDLE_ROOT)
	@mkdir -p $(AUDIT_BUNDLE_ROOT)/contracts
	# Source + manifests
	@cp -a programs $(AUDIT_BUNDLE_ROOT)/contracts/programs
	@cp -a tests    $(AUDIT_BUNDLE_ROOT)/contracts/tests
	@cp -a audit    $(AUDIT_BUNDLE_ROOT)/contracts/audit
	@cp -a scripts  $(AUDIT_BUNDLE_ROOT)/contracts/scripts
	@cp Cargo.toml Anchor.toml package.json tsconfig.json README.md Makefile \
	    $(AUDIT_BUNDLE_ROOT)/contracts/
	@[ -f Cargo.lock ]  && cp Cargo.lock  $(AUDIT_BUNDLE_ROOT)/contracts/ || true
	@[ -f Anchor.lock ] && cp Anchor.lock $(AUDIT_BUNDLE_ROOT)/contracts/ || true
	@[ -f yarn.lock ]   && cp yarn.lock   $(AUDIT_BUNDLE_ROOT)/contracts/ || true
	# Compiled IDLs + types (if present)
	@if [ -d target/idl   ]; then mkdir -p $(AUDIT_BUNDLE_ROOT)/contracts/target/idl   && cp -a target/idl/*   $(AUDIT_BUNDLE_ROOT)/contracts/target/idl/   2>/dev/null || true; fi
	@if [ -d target/types ]; then mkdir -p $(AUDIT_BUNDLE_ROOT)/contracts/target/types && cp -a target/types/* $(AUDIT_BUNDLE_ROOT)/contracts/target/types/ 2>/dev/null || true; fi
	@if [ -d target/deploy ]; then mkdir -p $(AUDIT_BUNDLE_ROOT)/contracts/target/deploy && cp -a target/deploy/*.so $(AUDIT_BUNDLE_ROOT)/contracts/target/deploy/ 2>/dev/null || true; fi
	# Tokenomics + disclaimer + whitepaper for full economic context
	@mkdir -p $(AUDIT_BUNDLE_ROOT)/docs $(AUDIT_BUNDLE_ROOT)/legal
	@cp ../docs/TOKENOMICS.md       $(AUDIT_BUNDLE_ROOT)/docs/
	@[ -f ../docs/whitepaper.md ] && cp ../docs/whitepaper.md $(AUDIT_BUNDLE_ROOT)/docs/ || true
	@cp ../legal/token-disclaimer.md $(AUDIT_BUNDLE_ROOT)/legal/
	# sha256 manifest
	@cd $(AUDIT_BUNDLE_ROOT) && find . -type f -print0 | sort -z | xargs -0 sha256sum > MANIFEST.sha256
	# Tarball
	@cd $(AUDIT_BUNDLE_ROOT)/.. && tar czf $(WORKSPACE)/$(AUDIT_BUNDLE_NAME) -C $(AUDIT_BUNDLE_ROOT) .
	@echo "Audit bundle written: $(WORKSPACE)/$(AUDIT_BUNDLE_NAME)"
	@echo "Bundle root sha256:   $$(sha256sum $(WORKSPACE)/$(AUDIT_BUNDLE_NAME) | awk '{print $$1}')"
	@echo "Manifest hash:        $$(sha256sum $(AUDIT_BUNDLE_ROOT)/MANIFEST.sha256 | awk '{print $$1}')"
