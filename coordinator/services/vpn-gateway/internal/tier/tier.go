// Package tier encodes the consumer VPN tier matrix and the enforcement
// helpers used by the rest of the vpn-gateway.
//
// The matrix is intentionally kept in code (not a config file) because tier
// boundaries are a product surface — every tier change ships through code
// review, not through ops touching a YAML.
//
//	Tier   Monthly cap      Locations   Ad-block   Kill switch (advisory)
//	-----  ---------------  ----------  ---------  -----------------------
//	FREE   2 GB / month     1            no         no
//	PLUS   unlimited        30           no         no
//	PRO    unlimited        30           yes        yes
//
// "Unlimited" is operationally enforced as a very high ceiling (1 PB / month)
// so a misbehaving client cannot accidentally DOS the data plane. The
// ceiling is hit only if a Plus/Pro customer is being abused — in which
// case the abuse pipeline kicks in.
package tier

import (
	"fmt"
	"strings"
)

// Tier is the consumer VPN tier of a single customer.
type Tier int

const (
	// TierUnknown is the zero value; rejects all traffic.
	TierUnknown Tier = iota
	// TierFree is the free tier — 2 GB/month, 1 location, no ad-block.
	TierFree
	// TierPlus is the $2.99/mo unlimited tier with 30 locations.
	TierPlus
	// TierPro is the $4.99/mo tier — Plus + ad/tracker blocking.
	TierPro
)

// String returns the lowercase canonical name; the value used over the
// wire and in NATS events.
func (t Tier) String() string {
	switch t {
	case TierFree:
		return "free"
	case TierPlus:
		return "plus"
	case TierPro:
		return "pro"
	default:
		return "unknown"
	}
}

// Parse maps the case-insensitive public-facing string to a Tier.
// Empty input returns TierFree (consumers default to free tier).
func Parse(s string) (Tier, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "free":
		return TierFree, nil
	case "plus":
		return TierPlus, nil
	case "pro":
		return TierPro, nil
	default:
		return TierUnknown, fmt.Errorf("tier: unknown value %q", s)
	}
}

// Limits enumerates every operational constraint a Tier imposes.
//
// MonthlyCapBytes == 0 means "no cap" (used as a sentinel; the data
// plane treats it as never-exceeded). We do NOT use a math.MaxUint64
// sentinel because callers occasionally compare via subtraction.
type Limits struct {
	// MonthlyCapBytes is the hard transfer cap. 0 = unlimited.
	MonthlyCapBytes uint64
	// AllowedLocations is the count of distinct exit countries the
	// tier can pick from. 1 for FREE (auto-routed), 30 for Plus/Pro.
	AllowedLocations int
	// AdBlock enables the DNS-level blocklist filter on this tier.
	AdBlock bool
	// KillSwitchAdvisory tells the client to enable its local kill switch
	// (drop all traffic if the WG tunnel goes down). Advisory means we
	// embed it as a flag in the WG config the client downloads — the
	// client honours it. We cannot enforce client-side kill switch from
	// the server.
	KillSwitchAdvisory bool
}

// LimitsFor returns the Limits matrix for the supplied Tier.
//
// This is the single source of truth for "what does each tier actually
// allow" — every package referencing tier capabilities reads it from
// here.
func LimitsFor(t Tier) Limits {
	switch t {
	case TierPlus:
		return Limits{
			MonthlyCapBytes:    0, // unlimited
			AllowedLocations:   30,
			AdBlock:            false,
			KillSwitchAdvisory: false,
		}
	case TierPro:
		return Limits{
			MonthlyCapBytes:    0, // unlimited
			AllowedLocations:   30,
			AdBlock:            true,
			KillSwitchAdvisory: true,
		}
	case TierFree:
		return Limits{
			MonthlyCapBytes:    2 * 1024 * 1024 * 1024, // 2 GB
			AllowedLocations:   1,
			AdBlock:            false,
			KillSwitchAdvisory: false,
		}
	default:
		return Limits{
			MonthlyCapBytes:    0,
			AllowedLocations:   0,
			AdBlock:            false,
			KillSwitchAdvisory: false,
		}
	}
}

// OverCap reports true when the supplied month-to-date usage exceeds the
// tier's MonthlyCapBytes. Tiers with cap==0 (Plus, Pro) always return
// false. Tiers with cap>0 (Free) return true exactly when used >= cap.
//
// We compare via >= (not >) so the cap is HARD: a free user who has
// transferred exactly 2 GB is over the limit, not at it.
func OverCap(t Tier, usedBytes uint64) bool {
	lim := LimitsFor(t)
	if lim.MonthlyCapBytes == 0 {
		return false
	}
	return usedBytes >= lim.MonthlyCapBytes
}

// CanSelectCountry reports whether the customer's chosen country is
// reachable from their tier. FREE customers are auto-routed (we ignore
// their country choice and pick the lowest-latency exit). Plus/Pro
// customers pick from 30 countries.
//
// `country` is the ISO-3166-1 alpha-2 code uppercased (e.g. "US", "DE").
// We do not validate the code here — that's the caller's responsibility.
func CanSelectCountry(t Tier, country string, supportedCountries []string) bool {
	lim := LimitsFor(t)
	if lim.AllowedLocations == 0 {
		return false
	}
	if lim.AllowedLocations == 1 {
		// FREE: any country is "allowed" in the sense that we will
		// route somewhere — but we pick, not them.
		return true
	}
	if country == "" {
		return true // server picks default
	}
	for _, c := range supportedCountries {
		if strings.EqualFold(c, country) {
			return true
		}
	}
	return false
}
