package ports

import "testing"

func TestDefaultPolicy_BlocksLegalListed(t *testing.T) {
	p := NewDefaultPolicy()
	for _, port := range []uint32{25, 465, 587, 2525, 6667, 6697, 9001, 9030, 23} {
		d := p.Check(port)
		if d.Allowed {
			t.Errorf("port %d should be blocked by default", port)
		}
		if d.Slug != "destination_port_blocked" {
			t.Errorf("port %d: slug = %q, want destination_port_blocked", port, d.Slug)
		}
	}
}

func TestDefaultPolicy_AllowsCommon(t *testing.T) {
	p := NewDefaultPolicy()
	for _, port := range []uint32{80, 443, 8080, 8443} {
		d := p.Check(port)
		if !d.Allowed {
			t.Errorf("port %d should be allowed by default; reason=%s", port, d.Reason)
		}
	}
}

func TestZeroPortAlwaysAllowed(t *testing.T) {
	p := NewDefaultPolicy()
	d := p.Check(0)
	if !d.Allowed {
		t.Errorf("port 0 should be allowed (caller didn't specify); got reason=%s", d.Reason)
	}
}

func TestAllowOverridesDeny(t *testing.T) {
	p := NewDefaultPolicy()
	p.Allow(25)
	if d := p.Check(25); !d.Allowed {
		t.Errorf("after Allow, port 25 should be allowed; got reason=%s", d.Reason)
	}
}

func TestDenyOverridesAllow(t *testing.T) {
	p := NewDefaultPolicy()
	p.Deny(443, "test")
	if d := p.Check(443); d.Allowed {
		t.Errorf("after Deny, port 443 should be blocked")
	}
}

func TestCheckExplicit_RequiresAllowList(t *testing.T) {
	p := NewDefaultPolicy()
	// 1234 is neither explicitly denied nor allowed → Check ALLOW,
	// CheckExplicit BLOCK.
	if d := p.Check(1234); !d.Allowed {
		t.Errorf("Check(1234) should ALLOW under default-allow policy")
	}
	if d := p.CheckExplicit(1234); d.Allowed {
		t.Errorf("CheckExplicit(1234) should BLOCK (not on allow list)")
	}
}

func TestSnapshot_NonEmpty(t *testing.T) {
	p := NewDefaultPolicy()
	if s := p.Snapshot(); s == "" {
		t.Fatal("Snapshot returned empty string")
	}
}
