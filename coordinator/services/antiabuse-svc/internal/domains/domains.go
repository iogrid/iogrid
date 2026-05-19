// Package domains encodes the high-risk-target policy from docs/LEGAL.md:
//
//   - Banking domains: customer must explicitly request, KYC verified
//   - Government domains (.gov, .mil): block unconditionally
//   - Adult content domains: provider must explicitly opt-in
//
// The package operates on the eTLD+1 form of any URL or hostname (so
// "https://payments.chase.com/foo" reduces to "chase.com"). For now
// the banking and adult lists are static; in Phase 1 they'll move to
// the database so legal can edit them without redeploying.
package domains

import (
	"net/url"
	"path/filepath"
	"strings"

	"golang.org/x/net/publicsuffix"
)

// Class is the high-level category of a domain.
type Class int

const (
	// ClassNormal is the default — no special handling.
	ClassNormal Class = iota
	// ClassBanking is a financial-institution domain. Customers must
	// pass KYC and explicitly request it; otherwise BLOCK.
	ClassBanking
	// ClassGovernment is a .gov / .mil domain. Always BLOCK.
	ClassGovernment
	// ClassAdult is adult-content. Provider must opt-in via
	// scheduling-config category=ADULT_CONTENT; otherwise BLOCK.
	ClassAdult
	// ClassBlocked is an env-driven static deny-list match. Used by
	// staging / e2e harnesses to pre-seed known-bad fixtures and by
	// the upstream-feed loader to inject reputation-feed hits without
	// the full reputation pipeline. Always BLOCK.
	ClassBlocked
)

func (c Class) String() string {
	switch c {
	case ClassBanking:
		return "banking"
	case ClassGovernment:
		return "government"
	case ClassAdult:
		return "adult"
	case ClassBlocked:
		return "blocked"
	default:
		return "normal"
	}
}

// Policy resolves a hostname or URL to a Class.
type Policy struct {
	banking      map[string]struct{}
	adult        map[string]struct{}
	blockedExact map[string]struct{}
	blockedGlob  []string
}

// NewDefaultPolicy seeds the banking + adult lists with the most-trafficked
// entries. Phase 1 will replace this with a DB-backed loader.
func NewDefaultPolicy() *Policy {
	p := &Policy{
		banking:      map[string]struct{}{},
		adult:        map[string]struct{}{},
		blockedExact: map[string]struct{}{},
	}
	for _, d := range defaultBanking {
		p.banking[d] = struct{}{}
	}
	for _, d := range defaultAdult {
		p.adult[d] = struct{}{}
	}
	return p
}

// AddBlocked appends a domain (or glob pattern) to the env-driven block
// list. Plain domains (no `*` / `?`) are stored exact-match; anything with
// glob metacharacters is matched via filepath.Match against the lowercased
// hostname. Patterns are normalised to lower-case; matching is
// case-insensitive.
func (p *Policy) AddBlocked(pattern string) {
	pat := strings.ToLower(strings.TrimSpace(pattern))
	if pat == "" {
		return
	}
	if strings.ContainsAny(pat, "*?[") {
		p.blockedGlob = append(p.blockedGlob, pat)
		return
	}
	p.blockedExact[pat] = struct{}{}
}

// LoadBlocked appends every pattern in the slice via AddBlocked. Empty
// strings are skipped silently.
func (p *Policy) LoadBlocked(patterns []string) {
	for _, pat := range patterns {
		p.AddBlocked(pat)
	}
}

// BlockedCount reports the size of the deny-list (exact + glob) for
// ListFilters / metrics.
func (p *Policy) BlockedCount() int {
	return len(p.blockedExact) + len(p.blockedGlob)
}

// matchesBlocked returns true if the lowercased host matches the env
// deny-list. The host is checked verbatim, against its eTLD+1 form, and
// against every parent suffix (foo.bar.example.com → bar.example.com →
// example.com) so exact entries also catch arbitrary subdomains.
func (p *Policy) matchesBlocked(host string) bool {
	if host == "" {
		return false
	}
	if _, ok := p.blockedExact[host]; ok {
		return true
	}
	// Walk parent suffixes — `*.malware.test` is preferred but a bare
	// `malware.test` entry should still catch `sub.malware.test`.
	parts := strings.Split(host, ".")
	for i := 1; i < len(parts); i++ {
		suffix := strings.Join(parts[i:], ".")
		if _, ok := p.blockedExact[suffix]; ok {
			return true
		}
	}
	// eTLD+1 fallback for inputs like `https://payments.malware.test/x`.
	if etld := eTLDPlusOne(host); etld != host {
		if _, ok := p.blockedExact[etld]; ok {
			return true
		}
	}
	for _, g := range p.blockedGlob {
		if ok, _ := filepath.Match(g, host); ok {
			return true
		}
		// Also try the eTLD+1 form so `*.malware.test` matches
		// `payments.sub.malware.test` (filepath.Match doesn't recurse
		// through dot segments).
		if etld := eTLDPlusOne(host); etld != host {
			if ok, _ := filepath.Match(g, etld); ok {
				return true
			}
		}
	}
	return false
}

// AddBanking appends a domain to the banking list (case-insensitive,
// eTLD+1 normalised).
func (p *Policy) AddBanking(domain string) {
	if d := normalize(domain); d != "" {
		p.banking[d] = struct{}{}
	}
}

// AddAdult appends a domain to the adult list.
func (p *Policy) AddAdult(domain string) {
	if d := normalize(domain); d != "" {
		p.adult[d] = struct{}{}
	}
}

// Classify returns the Class for a raw hostname or full URL.
func (p *Policy) Classify(target string) Class {
	host := hostOf(target)
	if host == "" {
		return ClassNormal
	}
	lower := strings.ToLower(host)

	// Env-driven deny-list — checked first so operators can pre-empt
	// reputation-feed lookups for known-bad fixtures.
	if p.matchesBlocked(lower) {
		return ClassBlocked
	}

	// .gov and .mil are TLD-level — match on suffix to catch
	// "ftc.gov", "navy.mil", "treasury.gov".
	if hasTLD(lower, "gov") || hasTLD(lower, "mil") {
		return ClassGovernment
	}

	etld := eTLDPlusOne(lower)
	if _, ok := p.banking[etld]; ok {
		return ClassBanking
	}
	if _, ok := p.adult[etld]; ok {
		return ClassAdult
	}
	return ClassNormal
}

// BankingCount and AdultCount let callers (ListFilters) report the
// size of each list without exposing the contents.
func (p *Policy) BankingCount() int { return len(p.banking) }
func (p *Policy) AdultCount() int   { return len(p.adult) }

// hostOf accepts a URL or bare host and returns the hostname.
func hostOf(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	if strings.Contains(target, "://") {
		u, err := url.Parse(target)
		if err != nil {
			return ""
		}
		return u.Hostname()
	}
	// Bare host may include :port — strip it.
	if i := strings.IndexByte(target, ':'); i >= 0 {
		return target[:i]
	}
	return target
}

func normalize(domain string) string {
	d := strings.ToLower(strings.TrimSpace(domain))
	if d == "" {
		return ""
	}
	return eTLDPlusOne(d)
}

func eTLDPlusOne(host string) string {
	if e, err := publicsuffix.EffectiveTLDPlusOne(host); err == nil {
		return e
	}
	return host
}

func hasTLD(host, tld string) bool {
	host = strings.TrimSuffix(host, ".")
	if host == tld {
		return true
	}
	return strings.HasSuffix(host, "."+tld)
}

// Seed lists; small and easy to audit, expanded by configuration.
var (
	defaultBanking = []string{
		"chase.com", "bankofamerica.com", "wellsfargo.com", "citi.com",
		"usbank.com", "pnc.com", "capitalone.com", "tdbank.com",
		"hsbc.com", "barclays.com", "santander.com", "bnpparibas.com",
		"deutsche-bank.de", "ing.com", "rabobank.nl", "credit-suisse.com",
		"ubs.com", "morganstanley.com", "goldmansachs.com",
		"americanexpress.com", "paypal.com", "stripe.com", "wise.com",
		"revolut.com", "n26.com", "monzo.com", "starlingbank.com",
	}
	defaultAdult = []string{
		"pornhub.com", "xvideos.com", "xnxx.com", "redtube.com",
		"youporn.com", "onlyfans.com", "fansly.com", "chaturbate.com",
		"livejasmin.com", "stripchat.com", "cam4.com", "myfreecams.com",
		"adultfriendfinder.com", "ashleymadison.com",
	}
)
