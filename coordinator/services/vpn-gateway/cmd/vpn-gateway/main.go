// Command vpn-gateway is the consumer-VPN microservice.
//
// It exposes an HTTP control surface on $LISTEN_ADDR (default :8080)
// for admin + service-discovery + DNS-policy queries, and (in a real
// deployment) a WireGuard UDP listener on a SEPARATE LoadBalancer
// service exposed on UDP :51820. The HTTP plane and WG plane share
// process state through the package-level types in internal/{customer,
// blocklist, session, metering, wireguard}.
//
// The WG bring-up is gated by the WG_LISTEN_ENABLE env (defaults off in
// test/dev so the scaffold can run on ports that may already be bound).
// In production it is on.
package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/vpn-gateway/internal/blocklist"
	"github.com/iogrid/iogrid/coordinator/services/vpn-gateway/internal/customer"
	"github.com/iogrid/iogrid/coordinator/services/vpn-gateway/internal/metering"
	"github.com/iogrid/iogrid/coordinator/services/vpn-gateway/internal/server"
	"github.com/iogrid/iogrid/coordinator/services/vpn-gateway/internal/session"
	"github.com/iogrid/iogrid/coordinator/services/vpn-gateway/internal/wireguard"
	"github.com/iogrid/iogrid/coordinator/shared/health"
	"github.com/iogrid/iogrid/coordinator/shared/log"
	"github.com/iogrid/iogrid/coordinator/shared/otel"
	sharedserver "github.com/iogrid/iogrid/coordinator/shared/server"
)

const (
	serviceName    = "vpn-gateway"
	serviceVersion = "0.0.1"
)

func main() {
	logger := log.Setup(serviceName)
	logger.Info("starting vpn-gateway", slog.String("version", serviceVersion))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	otelShutdown, err := otel.Setup(ctx, serviceName, serviceVersion)
	if err != nil {
		logger.Error("otel setup failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = otelShutdown(shutdownCtx)
	}()

	// --- assemble the in-process state. --------------------------------------
	bl := blocklist.New()
	if path := os.Getenv("BLOCKLIST_FILE"); path != "" {
		if n, err := bl.LoadFile(path); err != nil {
			logger.Warn("blocklist file load failed", slog.String("path", path), slog.String("error", err.Error()))
		} else {
			logger.Info("blocklist loaded", slog.String("path", path), slog.Int("hosts", n))
		}
	} else if url := os.Getenv("BLOCKLIST_URL"); url != "" {
		if n, err := bl.LoadURL(ctx, url); err != nil {
			logger.Warn("blocklist URL load failed", slog.String("url", url), slog.String("error", err.Error()))
		} else {
			logger.Info("blocklist loaded", slog.String("url", url), slog.Int("hosts", n))
		}
	} else {
		logger.Info("blocklist not configured — ad-block requests will all pass through")
	}

	gw := &server.Gateway{
		Customers:          customer.New(),
		Blocklist:          bl,
		Meter:              metering.New(nil), // emitter wired up once NATS is mounted
		Sessions:           session.New(0),
		SupportedCountries: parseCountries(os.Getenv("SUPPORTED_COUNTRIES")),
		ServerPublicKeyB64: os.Getenv("SERVER_PUBLIC_KEY_B64"),
		ServerEndpoint:     orDefault(os.Getenv("SERVER_ENDPOINT"), "vpn.iogrid.org:51820"),
		DNSAddress:         orDefault(os.Getenv("DNS_ADDRESS"), "10.99.0.1"),
	}

	// --- optionally start the WG data plane. ---------------------------------
	// Production deployments set WG_LISTEN_ENABLE=true; the WG_LISTEN_PORT
	// is forwarded by a k8s LoadBalancer Service (UDP :51820 publicly).
	if strings.EqualFold(os.Getenv("WG_LISTEN_ENABLE"), "true") {
		port := 51820
		if s := os.Getenv("WG_LISTEN_PORT"); s != "" {
			if v, err := strconv.Atoi(s); err == nil {
				port = v
			}
		}
		// The Mock keeps us compileable + testable; the production
		// build swaps in a wgctrl-go-backed implementation behind the
		// same interface. (Wiring belongs in a follow-up PR — see
		// internal/wireguard/wireguard.go for the contract.)
		wg := wireguard.NewMock()
		var priv [32]byte
		if err := wg.Start(ctx, &net.UDPAddr{Port: port}, priv); err != nil {
			logger.Error("wireguard start failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		logger.Info("wireguard server up (mock backend)", slog.Int("port", port))
	} else {
		logger.Info("wireguard listener disabled — set WG_LISTEN_ENABLE=true to enable")
	}

	// --- weekly blocklist refresh. -------------------------------------------
	if url := os.Getenv("BLOCKLIST_URL"); url != "" {
		ref := &blocklist.Refresher{
			Set:      bl,
			URL:      url,
			Interval: 7 * 24 * time.Hour,
			OnReload: func(n int, err error) {
				if err != nil {
					logger.Warn("blocklist refresh failed", slog.String("error", err.Error()))
					return
				}
				logger.Info("blocklist refreshed", slog.Int("hosts", n))
			},
		}
		ref.Start(ctx)
		defer ref.Stop()
	}

	// --- session sweeper. ----------------------------------------------------
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if n := gw.Sessions.Sweep(); n > 0 {
					logger.Info("session sweep", slog.Int("expired", n))
				}
			}
		}
	}()

	// --- HTTP serve. ---------------------------------------------------------
	hr := health.New()
	hr.MarkReady()

	if err := sharedserver.Run(ctx, sharedserver.Options{
		ServiceName: serviceName,
		Logger:      logger,
		Health:      hr,
		Mount:       server.Mount(gw),
	}); err != nil {
		logger.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func parseCountries(s string) []string {
	if s == "" {
		// 30-country default for Plus/Pro. The list is product-curated
		// and shipped as a constant; ops can override via env.
		return []string{
			"US", "CA", "MX", "BR", "AR",
			"GB", "IE", "DE", "FR", "NL", "BE", "ES", "IT", "PT", "CH",
			"AT", "DK", "SE", "NO", "FI", "PL", "CZ", "RO",
			"JP", "KR", "SG", "HK", "TW", "AU", "NZ",
		}
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.ToUpper(strings.TrimSpace(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
