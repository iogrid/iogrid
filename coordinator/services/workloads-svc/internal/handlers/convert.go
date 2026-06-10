// Package handlers wires the Connect-Go server handlers for the two
// workloads-svc gRPC services (WorkloadSubmission, WorkloadDispatch) on
// top of the store + scheduler + dispatcher.
//
// This file holds proto ⇆ store-struct conversion helpers, kept
// side-effect-free so they can be unit-tested without spinning up a
// server.
package handlers

import (
	"time"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	workloadsv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/workloads/v1"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/store"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// uuidString returns the canonical hyphenated string form of a wire UUID,
// or empty when the message is nil.
func uuidString(u *commonv1.UUID) string {
	if u == nil {
		return ""
	}
	return u.GetValue()
}

// uuidProto wraps a string into the canonical UUID proto, never returning
// nil so handlers don't need nil-guards on every response.
func uuidProto(s string) *commonv1.UUID {
	return &commonv1.UUID{Value: s}
}

// workloadTypeSlug maps the common WorkloadType enum to the lower-snake
// slug used by the store discriminator.
func workloadTypeSlug(t commonv1.WorkloadType) string {
	switch t {
	case commonv1.WorkloadType_WORKLOAD_TYPE_BANDWIDTH:
		return store.TypeBandwidth
	case commonv1.WorkloadType_WORKLOAD_TYPE_DOCKER:
		return store.TypeDocker
	case commonv1.WorkloadType_WORKLOAD_TYPE_GPU:
		return store.TypeGPU
	case commonv1.WorkloadType_WORKLOAD_TYPE_IOS_BUILD:
		return store.TypeIOSBuild
	default:
		return ""
	}
}

func slugToWorkloadType(s string) commonv1.WorkloadType {
	switch s {
	case store.TypeBandwidth:
		return commonv1.WorkloadType_WORKLOAD_TYPE_BANDWIDTH
	case store.TypeDocker:
		return commonv1.WorkloadType_WORKLOAD_TYPE_DOCKER
	case store.TypeGPU:
		return commonv1.WorkloadType_WORKLOAD_TYPE_GPU
	case store.TypeIOSBuild:
		return commonv1.WorkloadType_WORKLOAD_TYPE_IOS_BUILD
	default:
		return commonv1.WorkloadType_WORKLOAD_TYPE_UNSPECIFIED
	}
}

func priorityToSlug(p workloadsv1.WorkloadPriority) string {
	switch p {
	case workloadsv1.WorkloadPriority_WORKLOAD_PRIORITY_LOW:
		return "low"
	case workloadsv1.WorkloadPriority_WORKLOAD_PRIORITY_HIGH:
		return "high"
	default:
		return "normal"
	}
}

func priorityFromSlug(s string) workloadsv1.WorkloadPriority {
	switch s {
	case "low":
		return workloadsv1.WorkloadPriority_WORKLOAD_PRIORITY_LOW
	case "high":
		return workloadsv1.WorkloadPriority_WORKLOAD_PRIORITY_HIGH
	case "normal":
		return workloadsv1.WorkloadPriority_WORKLOAD_PRIORITY_NORMAL
	default:
		return workloadsv1.WorkloadPriority_WORKLOAD_PRIORITY_UNSPECIFIED
	}
}

// workloadFromProto projects a wire Workload into the store struct.
// Exactly one of the typed payload fields is consulted (whichever oneof
// the caller set); unrecognised payloads silently leave all spec pointers
// nil — the handler is responsible for rejecting empty submissions.
func workloadFromProto(in *workloadsv1.Workload) *store.Workload {
	if in == nil {
		return nil
	}
	w := &store.Workload{
		ID:                uuidString(in.GetId()),
		WorkspaceID:       uuidString(in.GetWorkspaceId()),
		SubmittedByUserID: uuidString(in.GetSubmittedByUserId()),
		Type:              workloadTypeSlug(in.GetType()),
		Priority:          priorityToSlug(in.GetPriority()),
		Labels:            cloneMap(in.GetLabels()),
	}
	if ts := in.GetSubmittedAt(); ts != nil {
		w.SubmittedAt = ts.AsTime()
	}
	if b := in.GetBandwidth(); b != nil {
		w.Bandwidth = &store.BandwidthSpec{
			TargetURL:       b.GetTargetUrl(),
			Method:          b.GetMethod(),
			SessionID:       b.GetSessionId(),
			PreferredRegion: regionSlug(b.GetPreferredRegion()),
			Category:        b.GetCategory(),
		}
		if m := b.GetMaxSpend(); m != nil {
			w.Bandwidth.MaxSpendCurrency = m.GetCurrency()
			w.Bandwidth.MaxSpendMicros = m.GetMicros()
		}
	}
	if d := in.GetDocker(); d != nil {
		w.Docker = &store.DockerSpec{
			Image:           d.GetImage(),
			Command:         append([]string(nil), d.GetCommand()...),
			Env:             cloneMap(d.GetEnv()),
			Timeout:         durationOrZero(d.GetTimeout()),
			MinCPUCores:     d.GetMinCpuCores(),
			MinMemoryMiB:    d.GetMinMemoryMib(),
			MinGPUMemoryMiB: d.GetMinGpuMemoryMib(),
		}
	}
	if g := in.GetGpu(); g != nil {
		w.GPU = &store.GPUSpec{
			Image:          g.GetImage(),
			Command:        append([]string(nil), g.GetCommand()...),
			Env:            cloneMap(g.GetEnv()),
			Timeout:        durationOrZero(g.GetTimeout()),
			MinVRAMMiB:     g.GetMinVramMib(),
			AllowedVendors: append([]string(nil), g.GetAllowedVendors()...),
		}
	}
	if i := in.GetIosBuild(); i != nil {
		w.IOSBuild = &store.IOSBuildSpec{
			SourceTarballS3Key: i.GetSourceTarballS3Key(),
			TartImage:          i.GetTartImage(),
			BuildCommands:      append([]string(nil), i.GetBuildCommands()...),
			ArtifactBucket:     i.GetArtifactS3Bucket(),
			ArtifactPrefix:     i.GetArtifactS3Prefix(),
			RepoURL:            i.GetRepoUrl(),
			GitRef:             i.GetGitRef(),
			BuildCommand:       i.GetBuildCommand(),
			UploadURL:          i.GetUploadUrl(),
			ArtifactGuestPath:  i.GetArtifactGuestPath(),
			CPU:                i.GetCpu(),
			MemoryMiB:          i.GetMemoryMib(),
			BootTimeoutSecs:    i.GetBootTimeoutSecs(),
		}
	}
	return w
}

// workloadToProto inverts workloadFromProto.
func workloadToProto(w *store.Workload) *workloadsv1.Workload {
	if w == nil {
		return nil
	}
	out := &workloadsv1.Workload{
		Id:                uuidProto(w.ID),
		WorkspaceId:       uuidProto(w.WorkspaceID),
		SubmittedByUserId: uuidProto(w.SubmittedByUserID),
		Type:              slugToWorkloadType(w.Type),
		Priority:          priorityFromSlug(w.Priority),
		Status:            string(w.Status),
		SubmittedAt:       timestamppb.New(w.SubmittedAt),
		Labels:            cloneMap(w.Labels),
	}
	if !w.StartedAt.IsZero() {
		out.StartedAt = timestamppb.New(w.StartedAt)
	}
	if !w.FinishedAt.IsZero() {
		out.FinishedAt = timestamppb.New(w.FinishedAt)
	}
	if w.Bandwidth != nil {
		req := &workloadsv1.BandwidthRequest{
			TargetUrl: w.Bandwidth.TargetURL,
			Method:    w.Bandwidth.Method,
			SessionId: w.Bandwidth.SessionID,
			Category:  w.Bandwidth.Category,
		}
		if w.Bandwidth.PreferredRegion != "" {
			req.PreferredRegion = &commonv1.Region{Slug: w.Bandwidth.PreferredRegion}
		}
		if w.Bandwidth.MaxSpendCurrency != "" || w.Bandwidth.MaxSpendMicros != 0 {
			req.MaxSpend = &commonv1.Money{Currency: w.Bandwidth.MaxSpendCurrency, Micros: w.Bandwidth.MaxSpendMicros}
		}
		out.Payload = &workloadsv1.Workload_Bandwidth{Bandwidth: req}
	}
	if w.Docker != nil {
		out.Payload = &workloadsv1.Workload_Docker{Docker: &workloadsv1.DockerRequest{
			Image:           w.Docker.Image,
			Command:         append([]string(nil), w.Docker.Command...),
			Env:             cloneMap(w.Docker.Env),
			Timeout:         durationProto(w.Docker.Timeout),
			MinCpuCores:     w.Docker.MinCPUCores,
			MinMemoryMib:    w.Docker.MinMemoryMiB,
			MinGpuMemoryMib: w.Docker.MinGPUMemoryMiB,
		}}
	}
	if w.GPU != nil {
		out.Payload = &workloadsv1.Workload_Gpu{Gpu: &workloadsv1.GpuRequest{
			Image:          w.GPU.Image,
			Command:        append([]string(nil), w.GPU.Command...),
			Env:            cloneMap(w.GPU.Env),
			Timeout:        durationProto(w.GPU.Timeout),
			MinVramMib:     w.GPU.MinVRAMMiB,
			AllowedVendors: append([]string(nil), w.GPU.AllowedVendors...),
		}}
	}
	if w.IOSBuild != nil {
		out.Payload = &workloadsv1.Workload_IosBuild{IosBuild: &workloadsv1.IosBuildRequest{
			SourceTarballS3Key: w.IOSBuild.SourceTarballS3Key,
			TartImage:          w.IOSBuild.TartImage,
			BuildCommands:      append([]string(nil), w.IOSBuild.BuildCommands...),
			ArtifactS3Bucket:   w.IOSBuild.ArtifactBucket,
			ArtifactS3Prefix:   w.IOSBuild.ArtifactPrefix,
			RepoUrl:            w.IOSBuild.RepoURL,
			GitRef:             w.IOSBuild.GitRef,
			BuildCommand:       w.IOSBuild.BuildCommand,
			UploadUrl:          w.IOSBuild.UploadURL,
			ArtifactGuestPath:  w.IOSBuild.ArtifactGuestPath,
			Cpu:                w.IOSBuild.CPU,
			MemoryMib:          w.IOSBuild.MemoryMiB,
			BootTimeoutSecs:    w.IOSBuild.BootTimeoutSecs,
		}}
	}
	return out
}

func resultToProto(w *store.Workload) *workloadsv1.WorkloadResult {
	if w == nil || w.Result == nil {
		return nil
	}
	r := w.Result
	out := &workloadsv1.WorkloadResult{
		WorkloadId:     uuidProto(w.ID),
		TerminalStatus: r.TerminalStatus,
		ExitCode:       r.ExitCode,
		LogsS3Key:      r.LogsS3Key,
		BytesIn:        r.BytesIn,
		BytesOut:       r.BytesOut,
		ArtifactS3Keys: append([]string(nil), r.ArtifactS3Keys...),
		CompletedAt:    timestamppb.New(r.CompletedAt),
	}
	if r.Currency != "" || r.CostMicros != 0 {
		out.Cost = &commonv1.Money{Currency: r.Currency, Micros: r.CostMicros}
	}
	return out
}

// statusFromProto converts the daemon-supplied WorkloadStatus enum into
// the store's slug form. Unspecified is mapped to "queued".
func statusFromProto(s workloadsv1.WorkloadStatus) store.Status {
	switch s {
	case workloadsv1.WorkloadStatus_WORKLOAD_STATUS_QUEUED:
		return store.StatusQueued
	case workloadsv1.WorkloadStatus_WORKLOAD_STATUS_DISPATCHED:
		return store.StatusDispatched
	case workloadsv1.WorkloadStatus_WORKLOAD_STATUS_RUNNING:
		return store.StatusRunning
	case workloadsv1.WorkloadStatus_WORKLOAD_STATUS_SUCCEEDED:
		return store.StatusSucceeded
	case workloadsv1.WorkloadStatus_WORKLOAD_STATUS_FAILED:
		return store.StatusFailed
	case workloadsv1.WorkloadStatus_WORKLOAD_STATUS_TIMED_OUT:
		return store.StatusTimedOut
	case workloadsv1.WorkloadStatus_WORKLOAD_STATUS_CANCELLED:
		return store.StatusCancelled
	case workloadsv1.WorkloadStatus_WORKLOAD_STATUS_REJECTED:
		return store.StatusRejected
	default:
		return store.StatusQueued
	}
}

func statusToProto(s store.Status) workloadsv1.WorkloadStatus {
	switch s {
	case store.StatusQueued:
		return workloadsv1.WorkloadStatus_WORKLOAD_STATUS_QUEUED
	case store.StatusDispatched:
		return workloadsv1.WorkloadStatus_WORKLOAD_STATUS_DISPATCHED
	case store.StatusRunning:
		return workloadsv1.WorkloadStatus_WORKLOAD_STATUS_RUNNING
	case store.StatusSucceeded:
		return workloadsv1.WorkloadStatus_WORKLOAD_STATUS_SUCCEEDED
	case store.StatusFailed:
		return workloadsv1.WorkloadStatus_WORKLOAD_STATUS_FAILED
	case store.StatusTimedOut:
		return workloadsv1.WorkloadStatus_WORKLOAD_STATUS_TIMED_OUT
	case store.StatusCancelled:
		return workloadsv1.WorkloadStatus_WORKLOAD_STATUS_CANCELLED
	case store.StatusRejected:
		return workloadsv1.WorkloadStatus_WORKLOAD_STATUS_REJECTED
	default:
		return workloadsv1.WorkloadStatus_WORKLOAD_STATUS_UNSPECIFIED
	}
}

func regionSlug(r *commonv1.Region) string {
	if r == nil {
		return ""
	}
	return r.GetSlug()
}

func durationOrZero(d *durationpb.Duration) time.Duration {
	if d == nil {
		return 0
	}
	return d.AsDuration()
}

func durationProto(d time.Duration) *durationpb.Duration {
	if d == 0 {
		return nil
	}
	return durationpb.New(d)
}

func tsOrZero(t *timestamppb.Timestamp) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.AsTime()
}

func cloneMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
