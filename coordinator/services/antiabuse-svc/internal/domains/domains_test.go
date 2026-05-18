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
	}
	for c, s := range want {
		if c.String() != s {
			t.Errorf("Class(%d).String() = %q, want %q", c, c.String(), s)
		}
	}
}
