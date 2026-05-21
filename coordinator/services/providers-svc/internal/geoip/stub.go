package geoip

// StubLookuper is a Lookuper for unit tests in adjacent packages
// (handlers, server, integration harnesses). Lives in a non-_test.go
// file so it's reachable from outside the geoip package's own test
// binary. NEVER wire this into production code paths — main is
// responsible for choosing between a real .mmdb-backed reader and the
// NoopLookuper fallback.
type StubLookuper struct {
	// ByIP is the canned lookup table. Empty map => every Lookup
	// returns ErrNotFound (the production "address valid, unmapped"
	// signal).
	ByIP map[string]Result
	// Err short-circuits every Lookup with this error when non-nil.
	// Use it to exercise the handler's error-handling branches without
	// having to mutate ByIP.
	Err error
}

// Lookup returns either the canned Err or the matching ByIP entry. A
// miss surfaces as ErrNotFound to match the production semantics.
func (s StubLookuper) Lookup(ip string) (Result, error) {
	if s.Err != nil {
		return Result{}, s.Err
	}
	if r, ok := s.ByIP[ip]; ok {
		return r, nil
	}
	return Result{}, ErrNotFound
}

var _ Lookuper = StubLookuper{}
