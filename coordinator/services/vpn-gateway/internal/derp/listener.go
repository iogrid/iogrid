package derp

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// ListenConfig is the operator-facing knob set for ListenAndServe.
// All fields except Addr have sensible defaults.
type ListenConfig struct {
	// Addr is the TCP listen address (e.g. ":51821"). Required.
	Addr string
	// TLSConfig — when non-nil, the listener terminates TLS using
	// the provided config. Production deploys MUST set this; the
	// nil-TLS path is intended only for the in-process integration
	// tests that use net.Pipe.
	TLSConfig *tls.Config
	// ReadTimeout is the per-frame read deadline. Defaults to
	// `0` (no deadline) so long-lived sessions don't get torn
	// down between WG keepalives (every ~25 s).
	ReadTimeout time.Duration
}

// ListenAndServe spins up a relay listener bound to cfg.Addr and
// hands every incoming TCP connection to relay.AcceptConn. Blocks
// until ctx is cancelled OR the underlying listener returns an
// unrecoverable error.
//
// The Phase-4 deploy posture is to run one Listener per region (same
// regions as vpn-gateway), expose via a regional UDP/TCP LoadBalancer,
// publish the regional URLs via /v1/vpn/regions, and let the
// coordinator hand them out to clients when ICE fails repeatedly.
func (r *Relay) ListenAndServe(ctx context.Context, cfg ListenConfig) error {
	var ln net.Listener
	var err error
	if cfg.TLSConfig != nil {
		ln, err = tls.Listen("tcp", cfg.Addr, cfg.TLSConfig)
	} else {
		ln, err = net.Listen("tcp", cfg.Addr)
	}
	if err != nil {
		return err
	}
	r.logger.Info("derp: relay listening", slog.String("addr", cfg.Addr), slog.Bool("tls", cfg.TLSConfig != nil))

	// Close the listener on ctx cancellation so the Accept loop unwinds.
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		nc, err := ln.Accept()
		if err != nil {
			// Accept errors after ctx cancellation are expected
			// (listener closed); anything else is a startup-time
			// posture problem the operator needs to see.
			select {
			case <-ctx.Done():
				return nil
			default:
				return err
			}
		}
		if cfg.ReadTimeout > 0 {
			_ = nc.SetReadDeadline(time.Now().Add(cfg.ReadTimeout))
		}
		go r.AcceptConn(ctx, nc)
	}
}

// HealthHandler returns an http.HandlerFunc that exposes the relay's
// stats as a tiny JSON blob. Mount on a separate HTTP port from the
// relay's TCP listener so operators can probe it via standard
// liveness/readiness checks without parsing the binary protocol.
func (r *Relay) HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		peers, fwd, dropped, bytes := r.StatsSnapshot()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{`))
		_, _ = w.Write([]byte(`"peers_connected":` + itoa(peers) + `,`))
		_, _ = w.Write([]byte(`"frames_forwarded":` + utoa(fwd) + `,`))
		_, _ = w.Write([]byte(`"frames_dropped":` + utoa(dropped) + `,`))
		_, _ = w.Write([]byte(`"bytes_forwarded":` + utoa(bytes)))
		_, _ = w.Write([]byte(`}`))
	}
}

// itoa / utoa — strconv.Itoa pulls in fmt-style formatting which is
// overkill for the four-field health JSON. Hand-roll the int64/uint64
// → decimal conversion so HealthHandler stays allocation-light under
// scrape load.
func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func utoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
