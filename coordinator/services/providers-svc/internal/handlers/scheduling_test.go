package handlers

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	providersv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1"
	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/store"
)

func TestGetSchedulingConfig_Defaults(t *testing.T) {
	h := NewSchedulingHandler(store.NewInMemory(), nil)
	resp, err := h.GetSchedulingConfig(context.Background(), connect.NewRequest(&providersv1.GetSchedulingConfigRequest{
		ProviderId: &commonv1.UUID{Value: "p1"},
	}))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	caps := resp.Msg.Config.GetCaps()
	if caps.GetBandwidthCapGbPerMonth() != 50 {
		t.Fatalf("bandwidth default: got %d want 50", caps.GetBandwidthCapGbPerMonth())
	}
	if caps.GetCpuCapPercent() != 30 || caps.GetMemoryCapPercent() != 25 {
		t.Fatalf("cpu/mem defaults wrong: %+v", caps)
	}
}

func TestUpdateSchedulingConfig_RoundTrip(t *testing.T) {
	h := NewSchedulingHandler(store.NewInMemory(), nil)
	ctx := context.Background()
	cfg := &providersv1.SchedulingConfig{
		ProviderId: &commonv1.UUID{Value: "p1"},
		Caps: &providersv1.ResourceCaps{
			BandwidthCapGbPerMonth: 200,
			CpuCapPercent:          40,
			MemoryCapPercent:       35,
		},
		Idle: &providersv1.IdleDetection{Enabled: true, IdleThresholdSeconds: 600},
		Calendar: &providersv1.CalendarSchedule{
			Windows: []*providersv1.CalendarWindow{{
				DaysOfWeek:     []uint32{1, 2, 3, 4, 5},
				StartLocalTime: "22:00",
				EndLocalTime:   "07:00",
				Timezone:       "America/New_York",
			}},
		},
	}
	if _, err := h.UpdateSchedulingConfig(ctx, connect.NewRequest(&providersv1.UpdateSchedulingConfigRequest{Config: cfg})); err != nil {
		t.Fatalf("update: %v", err)
	}
	resp, err := h.GetSchedulingConfig(ctx, connect.NewRequest(&providersv1.GetSchedulingConfigRequest{
		ProviderId: &commonv1.UUID{Value: "p1"},
	}))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.Msg.Config.Caps.BandwidthCapGbPerMonth != 200 {
		t.Fatalf("did not persist")
	}
}

func TestUpdateSchedulingConfig_RejectsBadCalendar(t *testing.T) {
	h := NewSchedulingHandler(store.NewInMemory(), nil)
	_, err := h.UpdateSchedulingConfig(context.Background(), connect.NewRequest(&providersv1.UpdateSchedulingConfigRequest{
		Config: &providersv1.SchedulingConfig{
			ProviderId: &commonv1.UUID{Value: "p1"},
			Calendar: &providersv1.CalendarSchedule{Windows: []*providersv1.CalendarWindow{{
				StartLocalTime: "25:99",
				EndLocalTime:   "11:11",
				Timezone:       "UTC",
				DaysOfWeek:     []uint32{1},
			}}},
		},
	}))
	if err == nil {
		t.Fatalf("expected validation failure")
	}
}

func TestUpdateSchedulingConfig_RejectsOver100Pct(t *testing.T) {
	h := NewSchedulingHandler(store.NewInMemory(), nil)
	_, err := h.UpdateSchedulingConfig(context.Background(), connect.NewRequest(&providersv1.UpdateSchedulingConfigRequest{
		Config: &providersv1.SchedulingConfig{
			ProviderId: &commonv1.UUID{Value: "p1"},
			Caps:       &providersv1.ResourceCaps{CpuCapPercent: 150},
		},
	}))
	if err == nil {
		t.Fatalf("expected validation failure")
	}
}

func TestGetCurrentState_NoHeartbeatYet(t *testing.T) {
	h := NewSchedulingHandler(store.NewInMemory(), nil)
	resp, err := h.GetCurrentState(context.Background(), connect.NewRequest(&providersv1.GetCurrentStateRequest{
		ProviderId: &commonv1.UUID{Value: "p1"},
	}))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.Msg.State != providersv1.SchedulerState_SCHEDULER_STATE_ACTIVE {
		t.Fatalf("expected ACTIVE default, got %s", resp.Msg.State)
	}
}
