package server

import (
	"testing"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
)

// The metering consumer persists short TEXT names (DOCKER | GPU |
// IOS_BUILD | BANDWIDTH and variants like BANDWIDTH_VPN — the value
// live prod rows carry). ListUsage must map every stored value onto
// the wire enum, and the request's type_filter must round-trip to a
// prefix that matches those same rows.
func TestWorkloadTypeFromText(t *testing.T) {
	cases := []struct {
		text string
		want commonv1.WorkloadType
	}{
		{"BANDWIDTH", commonv1.WorkloadType_WORKLOAD_TYPE_BANDWIDTH},
		// The variant actually present in prod usage_event rows.
		{"BANDWIDTH_VPN", commonv1.WorkloadType_WORKLOAD_TYPE_BANDWIDTH},
		{"DOCKER", commonv1.WorkloadType_WORKLOAD_TYPE_DOCKER},
		{"GPU", commonv1.WorkloadType_WORKLOAD_TYPE_GPU},
		{"IOS_BUILD", commonv1.WorkloadType_WORKLOAD_TYPE_IOS_BUILD},
		{"", commonv1.WorkloadType_WORKLOAD_TYPE_UNSPECIFIED},
		{"SOMETHING_NEW", commonv1.WorkloadType_WORKLOAD_TYPE_UNSPECIFIED},
	}
	for _, c := range cases {
		if got := workloadTypeFromText(c.text); got != c.want {
			t.Errorf("workloadTypeFromText(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestTypePrefixFromEnum(t *testing.T) {
	cases := []struct {
		enum commonv1.WorkloadType
		want string
	}{
		{commonv1.WorkloadType_WORKLOAD_TYPE_BANDWIDTH, "BANDWIDTH"},
		{commonv1.WorkloadType_WORKLOAD_TYPE_DOCKER, "DOCKER"},
		{commonv1.WorkloadType_WORKLOAD_TYPE_GPU, "GPU"},
		{commonv1.WorkloadType_WORKLOAD_TYPE_IOS_BUILD, "IOS_BUILD"},
		// UNSPECIFIED means "no filter" → empty prefix.
		{commonv1.WorkloadType_WORKLOAD_TYPE_UNSPECIFIED, ""},
	}
	for _, c := range cases {
		if got := typePrefixFromEnum(c.enum); got != c.want {
			t.Errorf("typePrefixFromEnum(%v) = %q, want %q", c.enum, got, c.want)
		}
	}
}

// Every filterable enum must round-trip: the prefix produced for an enum
// must map back to the same enum (so a type_filter never silently
// returns rows of a different type). This also pins the BANDWIDTH
// prefix matching BANDWIDTH_VPN as intentional, not accidental.
func TestTypeFilterRoundTrip(t *testing.T) {
	for _, e := range []commonv1.WorkloadType{
		commonv1.WorkloadType_WORKLOAD_TYPE_BANDWIDTH,
		commonv1.WorkloadType_WORKLOAD_TYPE_DOCKER,
		commonv1.WorkloadType_WORKLOAD_TYPE_GPU,
		commonv1.WorkloadType_WORKLOAD_TYPE_IOS_BUILD,
	} {
		prefix := typePrefixFromEnum(e)
		if prefix == "" {
			t.Fatalf("filterable enum %v produced empty prefix", e)
		}
		if got := workloadTypeFromText(prefix); got != e {
			t.Errorf("round-trip %v → %q → %v", e, prefix, got)
		}
	}
}
