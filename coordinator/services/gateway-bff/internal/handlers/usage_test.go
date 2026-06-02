package handlers

import (
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	billingv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1"
	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
)

// TestUsageRecordsToRows is the #635 regression guard: the BFF must remap
// billing-svc's proto UsageRecord ({type, quantity, cost, recorded_at}) into
// the web's UsageRow shape ({workloadType, bytes, computeMillicpuSeconds,
// costMicros, bucketStart}), splitting quantity into bytes (BANDWIDTH) vs
// computeMillicpuSeconds (compute workloads) per the proto's documented unit.
func TestUsageRecordsToRows(t *testing.T) {
	at := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	records := []*billingv1.UsageRecord{
		{
			Type:       commonv1.WorkloadType_WORKLOAD_TYPE_BANDWIDTH,
			Quantity:   2048,
			Cost:       &commonv1.Money{Currency: "GRID", Micros: 1_500_000},
			RecordedAt: timestamppb.New(at),
		},
		{
			Type:       commonv1.WorkloadType_WORKLOAD_TYPE_GPU,
			Quantity:   600,
			Cost:       &commonv1.Money{Currency: "GRID", Micros: 9_000_000},
			RecordedAt: timestamppb.New(at),
		},
	}

	rows := usageRecordsToRows(records)
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rows))
	}

	// BANDWIDTH → quantity lands in bytes, compute stays 0.
	if rows[0].Bytes != "2048" || rows[0].ComputeMillicpuSeconds != "0" {
		t.Fatalf("bandwidth row split wrong: bytes=%q compute=%q", rows[0].Bytes, rows[0].ComputeMillicpuSeconds)
	}
	if rows[0].CostMicros != "1500000" {
		t.Fatalf("bandwidth costMicros: got %q want 1500000", rows[0].CostMicros)
	}
	if rows[0].BucketStart != "2026-06-03T12:00:00Z" {
		t.Fatalf("bucketStart: got %q", rows[0].BucketStart)
	}

	// GPU → quantity lands in computeMillicpuSeconds, bytes stays 0.
	if rows[1].ComputeMillicpuSeconds != "600" || rows[1].Bytes != "0" {
		t.Fatalf("gpu row split wrong: bytes=%q compute=%q", rows[1].Bytes, rows[1].ComputeMillicpuSeconds)
	}
}
