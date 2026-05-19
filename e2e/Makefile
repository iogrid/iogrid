# iogrid e2e smoke + integration test harness.
#
# Targets are intentionally pragmatic — kind + plain Postgres/NATS/MailHog
# (no CNPG/Cilium/cert-manager operators) so the harness boots in under
# 3 minutes on a standard 4-CPU GitHub Actions runner.
#
# Usage:
#   make up        — boot the kind cluster + install lightweight infra
#   make seed      — apply the test overlay (services + secrets + DBs)
#   make smoke     — run all smoke flows under e2e/smoke/, JUnit -> e2e/out/
#   make full      — up + seed + smoke + down
#   make down      — destroy the kind cluster
#   make logs      — dump pod logs from iogrid namespace
#   make port-forward — open port-forwards so smoke flows can hit services
#
# Every target is idempotent. Re-running `make up` after a partial failure
# tears the cluster down and rebuilds it.

SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c

# Cluster + image config
CLUSTER_NAME ?= iogrid-e2e
KIND_CONFIG  ?= kind-config.yaml
NAMESPACE    ?= iogrid
KUBECONFIG_FILE ?= $(CURDIR)/out/kubeconfig
OUT_DIR      ?= $(CURDIR)/out

# Pin tool versions for reproducibility in CI
KIND_VERSION ?= v0.24.0
KUBECTL_VERSION ?= v1.31.0
HELM_VERSION ?= v3.16.2

# Whether to build local images (slow, ~5 min) or use the published scaffold
# tags from ghcr.io. CI sets BUILD_LOCAL=0 by default; flip to 1 when
# iterating locally on a service.
BUILD_LOCAL ?= 0

.PHONY: help up down seed smoke full logs port-forward kill-port-forwards \
        check-tools build-local-images load-images wait-ready clean

help:
	@echo "iogrid e2e harness — targets:"
	@echo "  make up        Boot kind + namespaces + infra (postgres/nats/mailhog)"
	@echo "  make seed      Apply service manifests + test secrets"
	@echo "  make smoke     Run all smoke flows in e2e/smoke/"
	@echo "  make full      up + seed + smoke (default e2e pipeline)"
	@echo "  make down      Tear down the kind cluster"
	@echo "  make logs      Tail logs from every iogrid pod"
	@echo "  make clean     Wipe out/ and unique tmp state"

check-tools:
	@for t in kind kubectl helm jq curl; do \
	  if ! command -v $$t >/dev/null 2>&1; then \
	    echo "ERROR: $$t not installed (see e2e/README.md for one-liner installers)"; \
	    exit 1; \
	  fi; \
	done
	@mkdir -p $(OUT_DIR)

# ----------------------------------------------------------------------------
# Phase 1 — cluster up + infra
# ----------------------------------------------------------------------------
up: check-tools
	@echo "==> kind: create cluster '$(CLUSTER_NAME)'"
	@if kind get clusters | grep -qx "$(CLUSTER_NAME)"; then \
	  echo "==> kind: cluster already exists, recreating for a clean slate"; \
	  kind delete cluster --name $(CLUSTER_NAME); \
	fi
	kind create cluster --name $(CLUSTER_NAME) --config $(KIND_CONFIG) --wait 120s
	kind export kubeconfig --name $(CLUSTER_NAME) --kubeconfig $(KUBECONFIG_FILE)
	@echo "==> bootstrap: namespaces + infra"
	KUBECONFIG=$(KUBECONFIG_FILE) bash bootstrap.sh
	@echo "==> up: complete"

# ----------------------------------------------------------------------------
# Phase 2 — apply iogrid service manifests + seed dev secrets
# ----------------------------------------------------------------------------
seed: check-tools
	@echo "==> seed: applying iogrid service stack"
	KUBECONFIG=$(KUBECONFIG_FILE) bash seed.sh
	@echo "==> seed: complete"

# ----------------------------------------------------------------------------
# Phase 3 — smoke flows
# ----------------------------------------------------------------------------
smoke: check-tools
	@mkdir -p $(OUT_DIR)/smoke
	@echo "==> smoke: running flows under e2e/smoke/"
	@KUBECONFIG=$(KUBECONFIG_FILE) bash scripts/run-smoke.sh $(OUT_DIR)/smoke

full: up seed smoke
	@echo "==> full: e2e cycle complete (see $(OUT_DIR)/smoke for JUnit + logs)"

down:
	@echo "==> down: destroying kind cluster '$(CLUSTER_NAME)'"
	-kind delete cluster --name $(CLUSTER_NAME)
	-rm -rf $(OUT_DIR)

logs:
	@KUBECONFIG=$(KUBECONFIG_FILE) kubectl -n $(NAMESPACE) get pods -o wide
	@echo
	@for pod in $$(KUBECONFIG=$(KUBECONFIG_FILE) kubectl -n $(NAMESPACE) get pods -o name); do \
	  echo "=== $$pod ==="; \
	  KUBECONFIG=$(KUBECONFIG_FILE) kubectl -n $(NAMESPACE) logs --tail=80 $$pod || true; \
	done

clean:
	rm -rf $(OUT_DIR)
