package domains

import "testing"

func TestClassify_Banking(t *testing.T) {
	p := NewDefaultPolicy()
	cases := []string{
		"chase.com",
		"www.chase.com",
		"payments.chase.com",
		"https://login.bankofamerica.com/account",
		"wellsfargo.com:443",
	}
	for _, c := range cases {
		if got := p.Classify(c); got != ClassBanking {
			t.Errorf("Classify(%q) = %v, want banking", c, got)
		}
	}
}

func TestClassify_Government(t *testing.T) {
	p := NewDefaultPolicy()
	cases := []string{
		"ftc.gov",
		"www.treasury.gov",
		"navy.mil",
		"https://anything.gov/path",
		"foo.bar.gov",
	}
	for _, c := range cases {
		if got := p.Classify(c); got != ClassGovernment {
			t.Errorf("Classify(%q) = %v, want government", c, got)
		}
	}
}

func TestClassify_Adult(t *testing.T) {
	p := NewDefaultPolicy()
	if got := p.Classify("pornhub.com"); got != ClassAdult {
		t.Errorf("Classify(pornhub.com) = %v, want adult", got)
	}
	if got := p.Classify("sub.onlyfans.com"); got != ClassAdult {
		t.Errorf("Classify(sub.onlyfans.com) = %v, want adult", got)
	}
}

func TestClassify_Normal(t *testing.T) {
	p := NewDefaultPolicy()
	cases := []string{
		"example.com",
		"github.com",
		"https://news.ycombinator.com/",
		"reddit.com",
	}
	for _, c := range cases {
		if got := p.Classify(c); got != ClassNormal {
			t.Errorf("Classify(%q) = %v, want normal", c, got)
		}
	}
}

func TestClassify_EmptyInput(t *testing.T) {
	p := NewDefaultPolicy()
	if got := p.Classify(""); got != ClassNormal {
		t.Errorf("Classify(\"\") = %v, want normal", got)
	}
}

func TestAddBanking_NewEntryClassified(t *testing.T) {
	p := NewDefaultPolicy()
	if got := p.Classify("examplebank.io"); got == ClassBanking {
		t.Fatalf("examplebank.io should NOT yet be banking")
	}
	p.AddBanking("examplebank.io")
	if got := p.Classify("examplebank.io"); got != ClassBanking {
		t.Errorf("after AddBanking, Classify = %v, want banking", got)
	}
}

func TestGovSuffix_NoFalsePositive(t *testing.T) {
	p := NewDefaultPolicy()
	// "governance.example.com" must not trigger .gov
	if got := p.Classify("governance.example.com"); got != ClassNormal {
		t.Errorf("Classify(governance.example.com) = %v, want normal", got)
	}
	// "militaria.com" must not trigger .mil
	if got := p.Classify("militaria.com"); got != ClassNormal {
		t.Errorf("Classify(militaria.com) = %v, want normal", got)
	}
}

func TestClass_String(t *testing.T) {
	want := map[Class]string{
		ClassNormal:     "normal",
		ClassBanking:    "banking",
		ClassGovernment: "government",
		ClassAdult:      "adult",
		ClassBlocked:    "blocked",
	}
	for c, s := range want {
		if c.String() != s {
			t.Errorf("Class(%d).String() = %q, want %q", c, c.String(), s)
		}
	}
}

func TestClassify_BlockedExact(t *testing.T) {
	p := NewDefaultPolicy()
	p.LoadBlocked([]string{"malware.test", "known-bad.test"})

	if p.BlockedCount() != 2 {
		t.Errorf("BlockedCount = %d, want 2", p.BlockedCount())
	}

	cases := []string{
		"malware.test",
		"sub.malware.test",
		"a.b.c.malware.test",
		"https://known-bad.test/path",
		"MALWARE.TEST",
	}
	for _, c := range cases {
		if got := p.Classify(c); got != ClassBlocked {
			t.Errorf("Classify(%q) = %v, want blocked", c, got)
		}
	}
}

func TestClassify_BlockedGlob(t *testing.T) {
	p := NewDefaultPolicy()
	// Glob pattern — filepath.Match treats `*` as "no path separator"
	// but our hostnames are dot-separated so `*.evil.example` matches
	// exactly one label of prefix.
	p.LoadBlocked([]string{"*.evil.example", "evil-*.com"})

	if got := p.Classify("foo.evil.example"); got != ClassBlocked {
		t.Errorf("Classify(foo.evil.example) = %v, want blocked", got)
	}
	if got := p.Classify("evil-host.com"); got != ClassBlocked {
		t.Errorf("Classify(evil-host.com) = %v, want blocked", got)
	}
	// Non-match — bare `evil.example` does NOT match `*.evil.example`
	// (filepath.Match requires the `*` to absorb at least one char).
	if got := p.Classify("evil.example"); got == ClassBlocked {
		t.Errorf("Classify(evil.example) = blocked, glob should not match bare apex")
	}
	if got := p.Classify("benign.com"); got != ClassNormal {
		t.Errorf("Classify(benign.com) = %v, want normal", got)
	}
}

func TestClassify_BlockedPrecedence_OverridesGov(t *testing.T) {
	// Operator deny-list runs before .gov / banking / adult so an env
	// entry produces the dedicated `destination_blocked` explanation
	// for fixtures rather than the .gov reason.
	p := NewDefaultPolicy()
	p.AddBlocked("forbidden.gov")
	if got := p.Classify("forbidden.gov"); got != ClassBlocked {
		t.Errorf("Classify(forbidden.gov) = %v, want blocked (env precedence over gov)", got)
	}
	// Other .gov still classifies as government.
	if got := p.Classify("ftc.gov"); got != ClassGovernment {
		t.Errorf("Classify(ftc.gov) = %v, want government", got)
	}
}

func TestLoadBlocked_SkipsEmpty(t *testing.T) {
	p := NewDefaultPolicy()
	p.LoadBlocked([]string{"", "  ", "ok.test"})
	if p.BlockedCount() != 1 {
		t.Errorf("BlockedCount = %d, want 1 (empty/blank should be skipped)", p.BlockedCount())
	}
}
