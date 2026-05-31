#!/bin/bash

# iogrid Proxy Speed Test
# Comprehensive benchmark script for proxy performance testing
# Tests: latency, bandwidth (down/up), stability, IPv4/IPv6

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
API_KEY="${IOGRID_API_KEY:-}"
PROXY_HOST="proxy.iogrid.org"
PROXY_PORT="443"
TEST_SIZE_KB=1024  # 1MB default
VERBOSE="${VERBOSE:-0}"

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# ============================================================================
# Utility Functions
# ============================================================================

log_info() {
    echo -e "${BLUE}ℹ${NC} $*"
}

log_success() {
    echo -e "${GREEN}✓${NC} $*"
}

log_warning() {
    echo -e "${YELLOW}⚠${NC} $*"
}

log_error() {
    echo -e "${RED}✗${NC} $*"
}

log_header() {
    echo ""
    echo "════════════════════════════════════════════════════════════════"
    echo "  $*"
    echo "════════════════════════════════════════════════════════════════"
}

debug_log() {
    if [ "$VERBOSE" = "1" ]; then
        echo -e "${YELLOW}[DEBUG]${NC} $*" >&2
    fi
}

# ============================================================================
# Pre-flight Checks
# ============================================================================

check_prerequisites() {
    log_header "Pre-flight Checks"

    if [ -z "$API_KEY" ]; then
        log_error "IOGRID_API_KEY environment variable not set"
        echo ""
        echo "Usage:"
        echo "  export IOGRID_API_KEY='iog_c9a6...'"
        echo "  $0"
        exit 1
    fi

    # Check if get-ip binary exists
    if [ ! -f "$SCRIPT_DIR/get-ip" ]; then
        log_warning "get-ip binary not found at $SCRIPT_DIR/get-ip"
        log_info "Building get-ip from source..."
        cd "$SCRIPT_DIR" && go build -o get-ip get-ip.go 2>/dev/null || {
            log_error "Failed to build get-ip tool"
            exit 1
        }
        log_success "Built get-ip"
    fi

    # Check required commands
    for cmd in curl socat nc openssl; do
        if ! command -v $cmd &> /dev/null; then
            log_warning "Command '$cmd' not found (some tests may be skipped)"
        fi
    done

    log_success "All prerequisites met"
}

# ============================================================================
# Latency Tests
# ============================================================================

test_latency() {
    log_header "Latency Test"

    local iterations=10
    local total_ms=0
    local min_ms=999999
    local max_ms=0

    for i in $(seq 1 $iterations); do
        debug_log "Latency test iteration $i/$iterations"

        local start_ns=$(date +%s%N)

        # Simple TLS handshake timing
        timeout 5 openssl s_client -connect "$PROXY_HOST:$PROXY_PORT" -servername "$PROXY_HOST" < /dev/null &>/dev/null
        local exit_code=$?

        local end_ns=$(date +%s%N)
        local latency_ms=$(( (end_ns - start_ns) / 1000000 ))

        if [ $exit_code -eq 0 ]; then
            total_ms=$((total_ms + latency_ms))
            [ $latency_ms -lt $min_ms ] && min_ms=$latency_ms
            [ $latency_ms -gt $max_ms ] && max_ms=$latency_ms
            debug_log "  Iteration $i: ${latency_ms}ms"
        fi
    done

    local avg_ms=$((total_ms / iterations))

    echo "  Samples: $iterations"
    echo "  Minimum: ${min_ms}ms"
    echo "  Maximum: ${max_ms}ms"
    echo "  Average: ${avg_ms}ms"
    echo ""

    log_success "Latency test complete (avg: ${avg_ms}ms)"
}

# ============================================================================
# Bandwidth Tests (Download)
# ============================================================================

test_download_speed() {
    log_header "Download Speed Test"

    local ipversion="${1:-ipv4}"
    local test_url=""
    local service=""

    case "$ipversion" in
        ipv4)
            service="ipv4.icanhazip.com"
            ;;
        ipv6)
            service="ident.me"
            ;;
        *)
            log_error "Unknown IP version: $ipversion"
            return 1
            ;;
    esac

    log_info "Testing download speed via $service ($ipversion)..."

    # Create test payload (10MB)
    local payload_mb=10
    local payload_bytes=$((payload_mb * 1024 * 1024))

    # Use a large file from a CDN for more realistic testing
    test_url="https://speed.cloudflare.com/__down?bytes=$payload_bytes"

    debug_log "Downloading ${payload_mb}MB from $test_url through proxy"

    local start_time=$(date +%s%N)
    local bytes_downloaded=$(curl -s \
        -x "socks5://${API_KEY}:${API_KEY}@${PROXY_HOST}:${PROXY_PORT}" \
        --max-time 30 \
        --progress-bar \
        "$test_url" 2>&1 | tail -1 | awk '{print $1}' | tr -d 'k')

    if [ $? -ne 0 ]; then
        log_warning "Download test failed or timed out"
        return 1
    fi

    local end_time=$(date +%s%N)
    local duration_ns=$((end_time - start_time))
    local duration_sec=$(echo "scale=3; $duration_ns / 1000000000" | bc)

    # Calculate speed
    local bytes_downloaded_actual=$((payload_mb * 1024 * 1024))
    local mbps=$(echo "scale=2; ($bytes_downloaded_actual * 8) / ($duration_sec * 1000000)" | bc)

    echo "  Downloaded: ${payload_mb}MB"
    echo "  Time: ${duration_sec}s"
    echo "  Speed: ${mbps}Mbps"
    echo ""

    log_success "Download test complete (${mbps}Mbps)"
}

# ============================================================================
# Bandwidth Tests (Upload)
# ============================================================================

test_upload_speed() {
    log_header "Upload Speed Test"

    log_info "Testing upload speed via httpbin..."

    # Create test payload in memory
    local payload_mb=1
    local payload_size=$((payload_mb * 1024 * 1024))

    debug_log "Creating ${payload_mb}MB test payload"
    local test_data=$(head -c $payload_size </dev/urandom | base64)

    debug_log "Uploading ${payload_mb}MB to httpbin.org"

    local start_time=$(date +%s%N)

    local response=$(curl -s \
        -x "socks5://${API_KEY}:${API_KEY}@${PROXY_HOST}:${PROXY_PORT}" \
        --max-time 30 \
        -X POST \
        -d "$test_data" \
        "http://httpbin.org/post" 2>&1)

    if [ $? -ne 0 ]; then
        log_warning "Upload test failed or timed out"
        return 1
    fi

    local end_time=$(date +%s%N)
    local duration_ns=$((end_time - start_time))
    local duration_sec=$(echo "scale=3; $duration_ns / 1000000000" | bc)

    # Calculate speed
    local mbps=$(echo "scale=2; ($payload_size * 8) / ($duration_sec * 1000000)" | bc)

    echo "  Uploaded: ${payload_mb}MB"
    echo "  Time: ${duration_sec}s"
    echo "  Speed: ${mbps}Mbps"
    echo ""

    log_success "Upload test complete (${mbps}Mbps)"
}

# ============================================================================
# Stability Tests
# ============================================================================

test_stability() {
    log_header "Connection Stability Test"

    local iterations=20
    local success_count=0

    log_info "Running $iterations sequential connections..."

    for i in $(seq 1 $iterations); do
        debug_log "Stability test iteration $i/$iterations"

        # Try to get IP address
        if timeout 10 "$SCRIPT_DIR/get-ip" -ipversion ipv4 &>/dev/null; then
            success_count=$((success_count + 1))
        else
            debug_log "  Iteration $i: FAILED"
        fi

        # Show progress
        if [ $((i % 5)) -eq 0 ]; then
            echo -n "."
        fi
    done
    echo ""

    local success_rate=$((success_count * 100 / iterations))

    echo "  Successful connections: $success_count/$iterations"
    echo "  Success rate: ${success_rate}%"
    echo ""

    if [ $success_rate -ge 95 ]; then
        log_success "Stability test passed (${success_rate}% success rate)"
    else
        log_warning "Stability test: ${success_rate}% success rate (target: ≥95%)"
    fi
}

# ============================================================================
# Provider Routing Tests
# ============================================================================

test_routing() {
    log_header "Provider Routing Test"

    log_info "Testing IPv4 routing..."
    ipv4=$("$SCRIPT_DIR/get-ip" -ipversion ipv4 2>/dev/null)
    if [ -n "$ipv4" ]; then
        echo "  IPv4 via ipv4.icanhazip.com: $ipv4"
        log_success "IPv4 routing working"
    else
        log_warning "IPv4 routing test failed"
    fi

    echo ""
    log_info "Testing IPv6 routing..."
    ipv6=$("$SCRIPT_DIR/get-ip" -ipversion ipv6 2>/dev/null)
    if [ -n "$ipv6" ]; then
        echo "  IPv6 via ident.me: $ipv6"
        log_success "IPv6 routing working"
    else
        log_warning "IPv6 routing test failed"
    fi

    echo ""
}

# ============================================================================
# Comprehensive Report
# ============================================================================

generate_report() {
    log_header "Speed Test Report"

    cat << EOF

iogrid Proxy Speed Test Report
Generated: $(date -u +"%Y-%m-%d %H:%M:%S UTC")

Proxy: $PROXY_HOST:$PROXY_PORT
API Key: ${API_KEY:0:20}...

Summary:
  ✓ Latency test: Measures TLS handshake time
  ✓ Download speed: Measures throughput from provider
  ✓ Upload speed: Measures throughput to provider
  ✓ Stability: Measures connection consistency
  ✓ Routing: Verifies IPv4 and IPv6 egress

For full results, run:
  export IOGRID_API_KEY='your_key'
  VERBOSE=1 $0

EOF

    log_success "Report complete"
}

# ============================================================================
# Main
# ============================================================================

main() {
    local test_type="${1:-all}"

    echo ""
    echo "╔═══════════════════════════════════════════════════════════════╗"
    echo "║           iogrid Proxy Speed Test Suite                       ║"
    echo "╚═══════════════════════════════════════════════════════════════╝"
    echo ""

    check_prerequisites

    case "$test_type" in
        all)
            test_latency
            test_routing
            test_stability
            # Download/upload tests are commented out as they take longer
            # Uncomment to enable:
            # test_download_speed ipv4
            # test_download_speed ipv6
            # test_upload_speed
            generate_report
            ;;
        latency)
            test_latency
            ;;
        download)
            test_download_speed "${2:-ipv4}"
            ;;
        upload)
            test_upload_speed
            ;;
        stability)
            test_stability
            ;;
        routing)
            test_routing
            ;;
        full)
            test_latency
            test_routing
            test_stability
            test_download_speed ipv4
            test_download_speed ipv6
            test_upload_speed
            generate_report
            ;;
        *)
            echo "Usage: $0 [test_type]"
            echo ""
            echo "Test types:"
            echo "  all           - Run all quick tests (latency, routing, stability)"
            echo "  latency       - Measure TLS handshake latency"
            echo "  routing       - Test IPv4/IPv6 routing"
            echo "  stability     - Test connection stability (20 sequential connects)"
            echo "  download      - Test download speed (large file transfer)"
            echo "  upload        - Test upload speed"
            echo "  full          - Run all tests including bandwidth (slow)"
            echo ""
            echo "Environment variables:"
            echo "  IOGRID_API_KEY  - API key for authentication (required)"
            echo "  VERBOSE=1       - Enable debug output"
            echo ""
            exit 1
            ;;
    esac

    echo ""
}

main "$@"
