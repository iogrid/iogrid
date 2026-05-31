# iogrid Proxy Tools

Comprehensive toolkit for testing, monitoring, and benchmarking the iogrid SOCKS5 proxy service.

## Quick Start

```bash
# Set your API key
export IOGRID_API_KEY='iog_c9a6fbb02c9d31efb1039242f11b58af577b0bc71981e85a5d5d44491d10a6c7'

# Build tools
make build

# Run quick speed test
./speed-test.sh all

# Get provider IP address
./get-ip -ipversion ipv4
```

## Tools Overview

### 1. `get-ip` — Retrieve Provider's Public IP

Connects through the SOCKS5 proxy to determine the provider's public IP address.

**Features:**
- IPv4 and IPv6 support
- Configurable IP version preference
- Multiple echo services for redundancy
- Sub-1-second latency

**Usage:**

```bash
# Get IPv4 address
./get-ip -ipversion ipv4
# Output: 5.21.76.248

# Get IPv6 address
./get-ip -ipversion ipv6
# Output: 2a02:2908:4400:cbc:c036:4245:4da8:4dcd

# Auto-detect (prefer IPv4, fall back to IPv6)
./get-ip -ipversion auto
```

**Build from source:**
```bash
go build -o get-ip get-ip.go
```

### 2. `speed-test.sh` — Comprehensive Performance Benchmark

Full-featured speed and stability testing suite for the proxy.

**Available Tests:**

| Test | Description | Time |
|---|---|---|
| `latency` | TLS handshake latency (10 samples) | ~5s |
| `routing` | IPv4/IPv6 egress verification | ~2s |
| `stability` | Connection consistency (20 connects) | ~10s |
| `download` | Download speed through proxy | ~60s |
| `upload` | Upload speed through proxy | ~30s |
| `all` | Quick tests (latency, routing, stability) | ~20s |
| `full` | All tests including bandwidth | ~2min |

**Usage:**

```bash
# Run all quick tests
./speed-test.sh all

# Run specific test
./speed-test.sh latency
./speed-test.sh routing
./speed-test.sh stability

# Run full benchmark (slow)
./speed-test.sh full

# Enable verbose debug output
VERBOSE=1 ./speed-test.sh latency
```

**Output Example:**

```
════════════════════════════════════════════════════════════════
  Latency Test
════════════════════════════════════════════════════════════════
ℹ Testing TLS handshake latency...
  Samples: 10
  Minimum: 45ms
  Maximum: 62ms
  Average: 51ms

✓ Latency test complete (avg: 51ms)

════════════════════════════════════════════════════════════════
  Provider Routing Test
════════════════════════════════════════════════════════════════
ℹ Testing IPv4 routing...
  IPv4 via ipv4.icanhazip.com: 5.21.76.248
✓ IPv4 routing working

ℹ Testing IPv6 routing...
  IPv6 via ident.me: 2a02:2908:4400:cbc:c036:4245:4da8:4dcd
✓ IPv6 routing working
```

### 3. `monitor-provider.sh` — Provider Heartbeat Monitoring

Continuously monitors provider online status and alerts when state changes.

**Usage:**

```bash
# Monitor provider heartbeat (checks every 5 seconds)
./monitor-provider.sh

# Example output:
# [2026-06-01 00:30:15] Provider online (1s stale)
# [2026-06-01 00:30:20] Provider online (6s stale)
# [2026-06-01 00:30:25] Provider OFFLINE - stale 65 seconds
```

### 4. `cleanup-pipeline.sh` — Post-Restart Cleanup

Automated database cleanup and verification pipeline for provider restoration.

**What it does:**
1. Verifies provider heartbeat is fresh
2. Snapshots pre-cleanup database state
3. Consolidates provider IDs (removes orphaned rows)
4. Rebinds audit event references
5. Runs full end-to-end proxy test
6. Generates evidence for issue closure

**Usage:**

```bash
# Execute cleanup pipeline
./cleanup-pipeline.sh

# Outputs:
# ✓ Provider online (last_seen: 2s ago)
# ✓ Snapshot saved: /tmp/hatice-pre-cleanup-1780245372.tsv
# ✓ Cleanup transaction committed
# ✓ TLS connection
# ✓ SOCKS5 greeting
# ✓ API key authentication
# ✓ Provider dispatch and tunnel established
# ✓✓✓ END-TO-END TEST PASSED ✓✓✓
```

## Common Workflows

### Test New Provider Installation

```bash
# 1. Build tools
make build

# 2. Set API key
export IOGRID_API_KEY='iog_...'

# 3. Run quick validation
./speed-test.sh all

# 4. Verify both IPv4 and IPv6
./get-ip -ipversion ipv4
./get-ip -ipversion ipv6

# 5. Run full benchmark if needed
./speed-test.sh full
```

### Monitor Provider Status

```bash
# Terminal 1: Monitor heartbeat
./monitor-provider.sh

# Terminal 2: Run periodic speed tests
watch -n 60 './speed-test.sh latency'
```

### Performance Baseline

```bash
# Get latency baseline
./speed-test.sh latency > latency-baseline.txt

# Full benchmark for reference
./speed-test.sh full > benchmark-full.txt

# Compare future runs against baseline
./speed-test.sh latency > latency-today.txt
diff latency-baseline.txt latency-today.txt
```

### Troubleshooting Connection Issues

```bash
# 1. Check routing
./speed-test.sh routing

# 2. Test stability
./speed-test.sh stability

# 3. Check latency spikes
VERBOSE=1 ./speed-test.sh latency

# 4. Verify provider IP
./get-ip -ipversion auto
```

## Build Instructions

### Prerequisites

```bash
go >= 1.16
bash >= 4.0
curl
openssl
```

### Building All Tools

```bash
cd tools/proxy

# Build get-ip binary
go build -o get-ip get-ip.go

# Make scripts executable
chmod +x speed-test.sh monitor-provider.sh cleanup-pipeline.sh

# Verify builds
ls -lh get-ip speed-test.sh
```

### Installing Globally

```bash
# Copy to standard location
sudo cp get-ip /usr/local/bin/
sudo cp *.sh /usr/local/bin/

# Or create symlinks
ln -s $(pwd)/get-ip ~/.local/bin/iogrid-get-ip
ln -s $(pwd)/speed-test.sh ~/.local/bin/iogrid-speed-test
```

## Environment Variables

| Variable | Purpose | Example |
|---|---|---|
| `IOGRID_API_KEY` | Authentication for proxy | `iog_c9a6fbb02...` |
| `IOGRID_PROXY_HOST` | Proxy hostname | `proxy.iogrid.org` |
| `IOGRID_PROXY_PORT` | Proxy port | `443` |
| `VERBOSE` | Enable debug logging | `1` |

## Performance Targets

### Latency
- **Target:** < 100ms average
- **Critical:** < 200ms 99th percentile

### Bandwidth (Download)
- **Target:** > 10 Mbps
- **Minimum:** > 5 Mbps

### Stability
- **Target:** > 99% success rate (≥ 198/200 connections)
- **Minimum:** > 95% success rate

### IPv4/IPv6
- **Target:** Both versions available
- **OK:** At least one version working

## Troubleshooting

### API Key Not Set

```
ERROR: IOGRID_API_KEY not set
```

**Fix:**
```bash
export IOGRID_API_KEY='iog_c9a6...'
```

### Build Failed: `go: command not found`

**Fix:**
```bash
# Install Go
brew install go          # macOS
apt install golang-go    # Ubuntu/Debian
```

### Connection Timeout

```
ERROR: Could not retrieve IP from any service
```

**Troubleshooting:**
```bash
# Check proxy connectivity
openssl s_client -connect proxy.iogrid.org:443

# Verify API key
echo $IOGRID_API_KEY

# Test with verbose output
VERBOSE=1 ./get-ip -ipversion ipv4
```

### Slow Performance

```bash
# 1. Check latency baseline
./speed-test.sh latency

# 2. Check routing
./speed-test.sh routing

# 3. Check provider status
kubectl get pods -l app=providers-svc

# 4. Check proxy-gateway logs
kubectl logs -l app=proxy-gateway -f
```

## Integration with CI/CD

### GitHub Actions Example

```yaml
- name: Test iogrid proxy
  env:
    IOGRID_API_KEY: ${{ secrets.IOGRID_API_KEY }}
  run: |
    cd tools/proxy
    go build -o get-ip get-ip.go
    chmod +x speed-test.sh
    ./speed-test.sh all
```

### GitLab CI Example

```yaml
proxy-speed-test:
  script:
    - cd tools/proxy
    - go build -o get-ip get-ip.go
    - chmod +x speed-test.sh
    - ./speed-test.sh all
  variables:
    IOGRID_API_KEY: $IOGRID_API_KEY
```

## API Reference

### get-ip Binary

```
Usage: get-ip [options]

Options:
  -ipversion string
    	IP version preference: auto|ipv4|ipv6 (default "auto")

Environment:
  IOGRID_API_KEY  API key for authentication (required)

Examples:
  get-ip -ipversion ipv4
  get-ip -ipversion ipv6
  get-ip -ipversion auto
```

### SOCKS5 Protocol

All tools use RFC 1928 (SOCKS5) with RFC 1929 (username/password) authentication:

```
1. TLS connection to proxy.iogrid.org:443
2. SOCKS5 greeting (0x05 0x01 0x02)
3. Username/password authentication
   - Username: API key
   - Password: API key
4. SOCKS5 CONNECT to target:port
5. HTTP request through tunnel
```

## Maintenance

### Log Rotation

Speed test output can be large. For long-running tests, rotate logs:

```bash
# Run test and save output
./speed-test.sh full > speed-test-$(date +%Y%m%d).log

# Archive old logs
tar czf logs-archive-$(date +%Y%m).tar.gz speed-test-*.log
```

### Updating Tools

```bash
# Pull latest version
git pull origin main

# Rebuild
cd tools/proxy && go build -o get-ip get-ip.go

# Verify version
./get-ip --version  # (if implemented)
```

## Support

For issues or feature requests:

1. Check troubleshooting section above
2. Enable `VERBOSE=1` for detailed output
3. Capture full output: `VERBOSE=1 ./speed-test.sh all 2>&1 | tee output.log`
4. File issue with: `output.log`, API key (last-4 only), and proxy host

## License

MIT License - See repo root LICENSE file
