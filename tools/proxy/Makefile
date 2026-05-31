.PHONY: help build clean install test

# Colors for output
BLUE := \033[0;34m
GREEN := \033[0;32m
RED := \033[0;31m
NC := \033[0m # No Color

help:
	@echo "$(BLUE)iogrid Proxy Tools - Build targets:$(NC)"
	@echo ""
	@echo "  $(GREEN)make build$(NC)        - Build all tools (get-ip binary + make scripts executable)"
	@echo "  $(GREEN)make clean$(NC)        - Remove build artifacts"
	@echo "  $(GREEN)make test$(NC)         - Run smoke tests"
	@echo "  $(GREEN)make install$(NC)      - Install tools to ~/.local/bin"
	@echo "  $(GREEN)make uninstall$(NC)    - Uninstall tools"
	@echo ""
	@echo "$(BLUE)Usage:$(NC)"
	@echo "  export IOGRID_API_KEY='iog_c9a6...'"
	@echo "  make build"
	@echo "  ./speed-test.sh all"
	@echo ""

build: get-ip scripts-executable
	@echo "$(GREEN)✓ All tools built successfully$(NC)"

get-ip: get-ip.go
	@echo "$(BLUE)Building get-ip binary...$(NC)"
	go build -o get-ip get-ip.go
	@echo "$(GREEN)✓ Built get-ip ($(shell ls -lh get-ip | awk '{print $$5}'))$(NC)"

scripts-executable:
	@echo "$(BLUE)Making scripts executable...$(NC)"
	chmod +x speed-test.sh monitor-provider.sh cleanup-pipeline.sh 2>/dev/null || true
	@echo "$(GREEN)✓ Scripts executable$(NC)"

clean:
	@echo "$(BLUE)Cleaning build artifacts...$(NC)"
	rm -f get-ip
	@echo "$(GREEN)✓ Cleaned$(NC)"

test: build
	@echo "$(BLUE)Running smoke tests...$(NC)"
	@echo ""
	@if [ -z "$$IOGRID_API_KEY" ]; then \
		echo "$(RED)✗ IOGRID_API_KEY not set$(NC)"; \
		echo "  export IOGRID_API_KEY='your_key'"; \
		exit 1; \
	fi
	@echo "Testing get-ip binary..."
	@timeout 5 ./get-ip -ipversion auto >/dev/null 2>&1 && \
		echo "$(GREEN)✓ get-ip works$(NC)" || \
		(echo "$(RED)✗ get-ip failed$(NC)" && exit 1)
	@echo ""
	@echo "Testing speed-test.sh routing..."
	@./speed-test.sh routing >/dev/null 2>&1 && \
		echo "$(GREEN)✓ speed-test.sh works$(NC)" || \
		(echo "$(RED)✗ speed-test.sh failed$(NC)" && exit 1)
	@echo ""
	@echo "$(GREEN)✓ All smoke tests passed$(NC)"

install: build
	@echo "$(BLUE)Installing tools...$(NC)"
	@mkdir -p ~/.local/bin
	cp get-ip ~/.local/bin/iogrid-get-ip
	cp speed-test.sh ~/.local/bin/iogrid-speed-test
	cp monitor-provider.sh ~/.local/bin/iogrid-monitor-provider
	cp cleanup-pipeline.sh ~/.local/bin/iogrid-cleanup-pipeline
	@echo "$(GREEN)✓ Installed to ~/.local/bin$(NC)"
	@echo ""
	@echo "$(BLUE)Add to PATH:$(NC)"
	@echo "  export PATH=\$$HOME/.local/bin:\$$PATH"

uninstall:
	@echo "$(BLUE)Uninstalling tools...$(NC)"
	rm -f ~/.local/bin/iogrid-get-ip ~/.local/bin/iogrid-speed-test ~/.local/bin/iogrid-monitor-provider ~/.local/bin/iogrid-cleanup-pipeline
	@echo "$(GREEN)✓ Uninstalled$(NC)"

.DEFAULT_GOAL := help
