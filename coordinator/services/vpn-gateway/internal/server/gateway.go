// Package server hosts the HTTP control + admin surface for the
// vpn-gateway microservice.
//
// The data plane (UDP :51820 → WireGuard → routing decision) is wired
// up in cmd/vpn-gateway/main.go from the wireguard, session, customer,
// blocklist and metering packages. This file owns the *HTTP* router,
// which serves:
//
//   - /healthz, /readyz, /metrics  (shared bootstrap)
//   - /v1/                          (service identity envelope)
//   - /v1/dns/resolve?host=...      (DNS filter probe — Pro-tier path
//                                    used by the in-tunnel resolver)
//   - /v1/admit                     (admit-or-reject decision endpoint
//                                    the WG frontend calls on first
//                                    handshake from a peer)
//   - /v1/peers/{pubkey}/stats      (current byte counters, used by
//                                    the metering flusher and the BFF)
//   - /v1/config/render             (renders a wgconfig artefact for a
//                                    specific customer + platform —
//                                    delegated from gateway-bff)
//
// All routes are JSON. The /admit endpoint is HOT-PATH; we keep it
// allocation-light and only emits one structured-log line per decision.
package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/iogrid/iogrid/coordinator/services/vpn-gateway/internal/blocklist"
	"github.com/iogrid/iogrid/coordinator/services/vpn-gateway/internal/customer"
	"github.com/iogrid/iogrid/coordinator/services/vpn-gateway/internal/metering"
	"github.com/iogrid/iogrid/coordinator/services/vpn-gateway/internal/session"
	"github.com/iogrid/iogrid/coordinator/services/vpn-gateway/internal/tier"
	"github.com/iogrid/iogrid/coordinator/services/vpn-gateway/internal/wgconfig"
)

// Gateway bundles the in-process state needed by every HTTP handler.
// A single Gateway value is constructed in main() and passed to Mount.
type Gateway struct {
	Customers          *customer.Registry
	Blocklist          *blocklist.Set
	Meter              *metering.Meter
	Sessions           *session.Store
	SupportedCountries []string

	ServerPublicKeyB64 string // for /v1/config/render
	ServerEndpoint     string // host:port broadcast to clients
	DNSAddress         string // in-tunnel DNS resolver address
}

// Mount returns a chi.Router callback wired with the gateway's routes.
// Returning a closure (instead of a method directly) matches the shape
// expected by sharedserver.Run.
func Mount(g *Gateway) func(chi.Router) {
	return func(r chi.Router) {
		r.Route("/v1", func(r chi.Router) {
			r.Get("/", g.indexHandler)
			r.Get("/dns/resolve", g.resolveDNSHandler)
			r.Post("/admit", g.admitHandler)
			r.Get("/peers/{pubkey}/stats", g.peerStatsHandler)
			r.Post("/config/render", g.renderConfigHandler)
		})
	}
}

func (g *Gateway) indexHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service":    "vpn-gateway",
		"status":     "ok",
		"customers":  g.Customers.Len(),
		"blocklist":  g.Blocklist.Size(),
		"sessions":   g.Sessions.Len(),
		"countries":  g.SupportedCountries,
		"endpoint":   g.ServerEndpoint,
		"public_key": g.ServerPublicKeyB64,
	})
}

// resolveDNSResponse is the body returned by /v1/dns/resolve.
type resolveDNSResponse struct {
	Host    string `json:"host"`
	Blocked bool   `json:"blocked"`
	Tier    string `json:"tier"`
}

// resolveDNSHandler decides whether a DNS query from a customer should
// be answered with NXDOMAIN (Pro tier ad-block hit) or passed through.
//
//	GET /v1/dns/resolve?host=ads.example.com&customer_id=<id>
//
// The actual UDP/53 resolver is a sidecar (CoreDNS) — it forwards the
// host to this endpoint for the policy decision, then synthesises the
// NXDOMAIN reply if Blocked=true.
func (g *Gateway) resolveDNSHandler(w http.ResponseWriter, r *http.Request) {
	host := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("host")))
	customerID := strings.TrimSpace(r.URL.Query().Get("customer_id"))
	if host == "" {
		writeError(w, http.StatusBadRequest, "host required")
		return
	}
	c, ok := g.Customers.ByID(customerID)
	if !ok {
		// Unknown customer → Free-tier semantics: no ad block, allow.
		writeJSON(w, http.StatusOK, resolveDNSResponse{Host: host, Blocked: false, Tier: tier.TierFree.String()})
		return
	}
	limits := tier.LimitsFor(c.Tier)
	blocked := limits.AdBlock && g.Blocklist.Block(host)
	writeJSON(w, http.StatusOK, resolveDNSResponse{
		Host:    host,
		Blocked: blocked,
		Tier:    c.Tier.String(),
	})
}

// admitRequest is the body the WG frontend POSTs on first handshake.
type admitRequest struct {
	PubKey  string `json:"pubkey"`  // base64 or hex
	Country string `json:"country"` // ISO alpha-2, optional
}

// admitResponse tells the WG frontend whether to admit the peer plus
// the operational parameters it needs (assigned IP, sticky provider,
// etc.).
type admitResponse struct {
	Admit           bool   `json:"admit"`
	Reason          string `json:"reason,omitempty"`
	CustomerID      string `json:"customer_id,omitempty"`
	Tier            string `json:"tier,omitempty"`
	AssignedIP      string `json:"assigned_ip,omitempty"`
	Country         string `json:"country,omitempty"`
	ProviderID      string `json:"provider_id,omitempty"`
	OverMonthlyCap  bool   `json:"over_monthly_cap,omitempty"`
	BytesUsedMonth  uint64 `json:"bytes_used_month,omitempty"`
	BytesCapMonth   uint64 `json:"bytes_cap_month,omitempty"`
}

// admitHandler is the synchronous admit-or-reject decision called by
// the WG frontend the first time it sees a handshake from a peer. The
// decision flow is:
//
//	1. Look up the customer by pubkey.
//	2. Reject UNKNOWN_PEER if not in registry.
//	3. Reject MONTHLY_CAP if free-tier and over limit.
//	4. Reject UNSUPPORTED_COUNTRY if country choice not allowed.
//	5. Bind a sticky session and return the provider_id.
func (g *Gateway) admitHandler(w http.ResponseWriter, r *http.Request) {
	var req admitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	pk, err := customer.DecodePubKey(req.PubKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pubkey: "+err.Error())
		return
	}
	c, ok := g.Customers.ByPubKey(pk)
	if !ok {
		writeJSON(w, http.StatusOK, admitResponse{
			Admit:  false,
			Reason: "UNKNOWN_PEER",
		})
		return
	}
	limits := tier.LimitsFor(c.Tier)
	used := g.Meter.MonthToDate(c.ID).Total()
	if tier.OverCap(c.Tier, used) {
		writeJSON(w, http.StatusOK, admitResponse{
			Admit:          false,
			Reason:         "MONTHLY_CAP_EXCEEDED",
			CustomerID:     c.ID,
			Tier:           c.Tier.String(),
			OverMonthlyCap: true,
			BytesUsedMonth: used,
			BytesCapMonth:  limits.MonthlyCapBytes,
		})
		return
	}
	country := strings.ToUpper(strings.TrimSpace(req.Country))
	if country == "" {
		country = c.Country
	}
	if !tier.CanSelectCountry(c.Tier, country, g.SupportedCountries) {
		writeJSON(w, http.StatusOK, admitResponse{
			Admit:      false,
			Reason:     "UNSUPPORTED_COUNTRY",
			CustomerID: c.ID,
			Tier:       c.Tier.String(),
			Country:    country,
		})
		return
	}
	binding := g.Sessions.Bind(c.ID, pickProvider(country), country)
	writeJSON(w, http.StatusOK, admitResponse{
		Admit:          true,
		CustomerID:     c.ID,
		Tier:           c.Tier.String(),
		AssignedIP:     c.AssignedIP,
		Country:        binding.Country,
		ProviderID:     binding.ProviderID,
		BytesUsedMonth: used,
		BytesCapMonth:  limits.MonthlyCapBytes,
	})
}

// pickProvider is the placeholder provider selector. In production it
// calls workloads-svc.SubmitWorkload(type=bandwidth, geo_preference=country)
// over Connect-Go and pins the returned provider ID. We keep the
// in-process default deterministic for tests by hashing the country.
func pickProvider(country string) string {
	if country == "" {
		return "provider-default"
	}
	return "provider-" + strings.ToLower(country)
}

// peerStatsHandler exposes the WG byte counters for one peer. The
// gateway runs its own metering loop locally, but this endpoint exists
// for the metering flusher in a sidecar pod or for admin tooling.
func (g *Gateway) peerStatsHandler(w http.ResponseWriter, r *http.Request) {
	pubkey := chi.URLParam(r, "pubkey")
	pk, err := customer.DecodePubKey(pubkey)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pubkey")
		return
	}
	c, ok := g.Customers.ByPubKey(pk)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown peer")
		return
	}
	used := g.Meter.MonthToDate(c.ID)
	limits := tier.LimitsFor(c.Tier)
	writeJSON(w, http.StatusOK, map[string]any{
		"customer_id":     c.ID,
		"tier":            c.Tier.String(),
		"bytes_in":        used.BytesIn,
		"bytes_out":       used.BytesOut,
		"bytes_total":     used.Total(),
		"bytes_cap_month": limits.MonthlyCapBytes,
		"over_cap":        tier.OverCap(c.Tier, used.Total()),
	})
}

// renderConfigRequest is the body POSTed to /v1/config/render by the
// gateway-bff service to fetch a WG config for a customer's chosen
// platform.
type renderConfigRequest struct {
	CustomerID         string `json:"customer_id"`
	Platform           string `json:"platform"`
	CustomerPrivateKey string `json:"customer_private_key,omitempty"`
}

func (g *Gateway) renderConfigHandler(w http.ResponseWriter, r *http.Request) {
	var req renderConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	c, ok := g.Customers.ByID(req.CustomerID)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown customer")
		return
	}
	plat, err := wgconfig.ParsePlatform(req.Platform)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	art, err := wgconfig.Render(wgconfig.Inputs{
		CustomerID:         c.ID,
		CustomerPrivateKey: req.CustomerPrivateKey,
		CustomerAddress:    c.AssignedIP,
		CustomerCountry:    c.Country,
		CustomerTier:       c.Tier,
		ServerPublicKey:    g.ServerPublicKeyB64,
		ServerEndpoint:     g.ServerEndpoint,
		ServerDNSAddress:   g.DNSAddress,
		Platform:           plat,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=\""+art.Filename+"\"")
	w.Header().Set("Content-Type", art.MimeType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(art.Body)
}

// writeJSON writes a JSON envelope. Errors at the encoder layer are
// logged at the caller; we don't want to overwrite headers we may
// have set already.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
