package vpn

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

// RoamingDetector monitors the local machine for network changes
// (default route swap, primary interface IP change) and notifies a
// callback so the active VPN tunnel can re-establish its endpoint
// without dropping the session.
//
// Detection strategy is a portable poll: every PollInterval we resolve
// the local source IP that would be used to reach the current VPN
// endpoint. If that source IP changes, that's a roaming event.
// On Linux a netlink listener would be lower-latency, but poll-based
// detection works on all platforms (Linux/macOS/Windows) without CGO
// and remains under 1s reconnect budget at PollInterval=500ms.
type RoamingDetector struct {
	PollInterval  time.Duration
	OnRoam        func(oldIP, newIP net.IP) error
	probeEndpoint string // e.g. "1.1.1.1:443" — any UDP probe target works

	mu       sync.Mutex
	lastIP   net.IP
	stopCh   chan struct{}
	stoppedCh chan struct{}
}

// NewRoamingDetector creates a roaming detector that probes against
// the provider's endpoint (host:port). The detector won't actually
// send packets — it just opens a UDP socket which forces the kernel
// to resolve the source IP that would be used.
func NewRoamingDetector(probeEndpoint string, pollInterval time.Duration, onRoam func(oldIP, newIP net.IP) error) *RoamingDetector {
	if pollInterval == 0 {
		pollInterval = 500 * time.Millisecond
	}
	return &RoamingDetector{
		PollInterval:  pollInterval,
		OnRoam:        onRoam,
		probeEndpoint: probeEndpoint,
		stopCh:        make(chan struct{}),
		stoppedCh:     make(chan struct{}),
	}
}

// Start begins the roaming detection loop. Returns immediately.
// The detector runs until Stop() is called or ctx is cancelled.
func (r *RoamingDetector) Start(ctx context.Context) error {
	// Establish baseline
	ip, err := r.probeSourceIP()
	if err != nil {
		return fmt.Errorf("baseline probe: %w", err)
	}
	r.mu.Lock()
	r.lastIP = ip
	r.mu.Unlock()

	go r.loop(ctx)
	return nil
}

// Stop terminates the detection loop and waits for it to finish.
func (r *RoamingDetector) Stop() {
	close(r.stopCh)
	<-r.stoppedCh
}

func (r *RoamingDetector) loop(ctx context.Context) {
	defer close(r.stoppedCh)
	ticker := time.NewTicker(r.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.checkOnce()
		}
	}
}

// CheckOnce performs a single roaming check (exposed for testing).
func (r *RoamingDetector) CheckOnce() (changed bool, oldIP, newIP net.IP) {
	return r.checkOnce()
}

func (r *RoamingDetector) checkOnce() (bool, net.IP, net.IP) {
	current, err := r.probeSourceIP()
	if err != nil {
		// Network is down — treat as no-change to avoid spurious events
		return false, nil, nil
	}

	r.mu.Lock()
	old := r.lastIP
	r.mu.Unlock()

	if old.Equal(current) {
		return false, old, current
	}

	// Roaming detected
	r.mu.Lock()
	r.lastIP = current
	r.mu.Unlock()

	if r.OnRoam != nil {
		// Fire callback (caller's responsibility to keep this fast — <1s)
		_ = r.OnRoam(old, current)
	}
	return true, old, current
}

// probeSourceIP opens a UDP "connection" to the probe endpoint and
// reads the local source IP the kernel selected. No packets are sent.
func (r *RoamingDetector) probeSourceIP() (net.IP, error) {
	conn, err := net.Dial("udp", r.probeEndpoint)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return nil, fmt.Errorf("unexpected local addr type %T", conn.LocalAddr())
	}
	return addr.IP, nil
}

// CurrentSourceIP returns the most recently observed source IP for
// the probe endpoint (the IP the kernel routes outbound packets through).
func (r *RoamingDetector) CurrentSourceIP() net.IP {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastIP
}
