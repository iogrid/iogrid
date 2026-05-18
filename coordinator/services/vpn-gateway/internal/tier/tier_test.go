package tier

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want Tier
		ok   bool
	}{
		{"", TierFree, true},
		{"free", TierFree, true},
		{"FREE", TierFree, true},
		{"plus", TierPlus, true},
		{"Plus", TierPlus, true},
		{"pro", TierPro, true},
		{"PRO", TierPro, true},
		{"  pro  ", TierPro, true},
		{"premium", TierUnknown, false},
	}
	for _, tc := range cases {
		got, err := Parse(tc.in)
		if tc.ok && err != nil {
			t.Errorf("Parse(%q): unexpected error %v", tc.in, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("Parse(%q): expected error, got %v", tc.in, got)
		}
		if got != tc.want {
			t.Errorf("Parse(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestOverCap(t *testing.T) {
	// FREE: 2GB cap
	if !OverCap(TierFree, 2*1024*1024*1024) {
		t.Error("OverCap(FREE, 2GB) should be true (>= cap)")
	}
	if OverCap(TierFree, 2*1024*1024*1024-1) {
		t.Error("OverCap(FREE, 2GB-1) should be false")
	}
	if !OverCap(TierFree, 10*1024*1024*1024) {
		t.Error("OverCap(FREE, 10GB) should be true")
	}
	// PLUS / PRO: never over
	if OverCap(TierPlus, 1<<60) {
		t.Error("OverCap(PLUS, huge) should be false (unlimited)")
	}
	if OverCap(TierPro, 1<<60) {
		t.Error("OverCap(PRO, huge) should be false (unlimited)")
	}
	// UNKNOWN: not over (no cap), but the gateway gates traffic via
	// CanSelectCountry/AllowedLocations==0 first.
	if OverCap(TierUnknown, 1<<60) {
		t.Error("OverCap(UNKNOWN, huge) should be false (no cap configured)")
	}
}

func TestLimitsFor(t *testing.T) {
	free := LimitsFor(TierFree)
	if free.MonthlyCapBytes != 2*1024*1024*1024 {
		t.Errorf("free cap = %d, want 2GB", free.MonthlyCapBytes)
	}
	if free.AllowedLocations != 1 {
		t.Errorf("free locations = %d, want 1", free.AllowedLocations)
	}
	if free.AdBlock {
		t.Error("free should not have ad-block")
	}
	plus := LimitsFor(TierPlus)
	if plus.MonthlyCapBytes != 0 {
		t.Error("plus must be unlimited")
	}
	if plus.AllowedLocations != 30 {
		t.Errorf("plus locations = %d, want 30", plus.AllowedLocations)
	}
	if plus.AdBlock {
		t.Error("plus should not have ad-block")
	}
	pro := LimitsFor(TierPro)
	if pro.MonthlyCapBytes != 0 {
		t.Error("pro must be unlimited")
	}
	if !pro.AdBlock {
		t.Error("pro must have ad-block")
	}
	if !pro.KillSwitchAdvisory {
		t.Error("pro must signal kill-switch advisory")
	}
}

func TestCanSelectCountry(t *testing.T) {
	supported := []string{"US", "DE", "JP", "GB"}
	// FREE: server-routed; always 'true'
	if !CanSelectCountry(TierFree, "US", supported) {
		t.Error("free should be allowed (server picks)")
	}
	if !CanSelectCountry(TierFree, "ZZ", supported) {
		t.Error("free always routes — country choice is informational")
	}
	// PLUS: allowed country
	if !CanSelectCountry(TierPlus, "DE", supported) {
		t.Error("plus DE should be allowed")
	}
	// PLUS: unsupported country
	if CanSelectCountry(TierPlus, "ZZ", supported) {
		t.Error("plus ZZ (unsupported) should be denied")
	}
	// empty country = server default
	if !CanSelectCountry(TierPlus, "", supported) {
		t.Error("plus empty country should be allowed (server default)")
	}
	// UNKNOWN: denied
	if CanSelectCountry(TierUnknown, "US", supported) {
		t.Error("unknown tier must not route")
	}
}

func TestString(t *testing.T) {
	if TierFree.String() != "free" {
		t.Errorf("free.String() = %s", TierFree.String())
	}
	if TierPlus.String() != "plus" {
		t.Errorf("plus.String() = %s", TierPlus.String())
	}
	if TierPro.String() != "pro" {
		t.Errorf("pro.String() = %s", TierPro.String())
	}
	if TierUnknown.String() != "unknown" {
		t.Errorf("unknown.String() = %s", TierUnknown.String())
	}
}
