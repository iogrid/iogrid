// Package handlers wires the Connect-Go server handlers for the three
// providers-svc gRPC services (Registration, Scheduling, Dashboard) on top
// of the store interface.
//
// This file holds the proto ⇆ store-struct conversion helpers. They are
// kept side-effect-free so they can be unit-tested without spinning up a
// full server.
package handlers

import (
	"time"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	providersv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1"
	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/store"
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

func platformToStore(p providersv1.Platform) store.Platform {
	switch p {
	case providersv1.Platform_PLATFORM_MACOS:
		return store.PlatformMacOS
	case providersv1.Platform_PLATFORM_LINUX:
		return store.PlatformLinux
	case providersv1.Platform_PLATFORM_WINDOWS:
		return store.PlatformWindows
	default:
		return store.PlatformUnspecified
	}
}

func platformToProto(p store.Platform) providersv1.Platform {
	switch p {
	case store.PlatformMacOS:
		return providersv1.Platform_PLATFORM_MACOS
	case store.PlatformLinux:
		return providersv1.Platform_PLATFORM_LINUX
	case store.PlatformWindows:
		return providersv1.Platform_PLATFORM_WINDOWS
	default:
		return providersv1.Platform_PLATFORM_UNSPECIFIED
	}
}

func archToProto(a string) providersv1.Architecture {
	switch a {
	case "amd64":
		return providersv1.Architecture_ARCHITECTURE_AMD64
	case "arm64":
		return providersv1.Architecture_ARCHITECTURE_ARM64
	default:
		return providersv1.Architecture_ARCHITECTURE_UNSPECIFIED
	}
}

func archToStore(a providersv1.Architecture) string {
	switch a {
	case providersv1.Architecture_ARCHITECTURE_AMD64:
		return "amd64"
	case providersv1.Architecture_ARCHITECTURE_ARM64:
		return "arm64"
	default:
		return ""
	}
}

func statusToStore(s providersv1.ProviderStatus) store.Status {
	switch s {
	case providersv1.ProviderStatus_PROVIDER_STATUS_ACTIVE:
		return store.StatusActive
	case providersv1.ProviderStatus_PROVIDER_STATUS_OFFLINE:
		return store.StatusOffline
	case providersv1.ProviderStatus_PROVIDER_STATUS_SUSPENDED:
		return store.StatusSuspended
	case providersv1.ProviderStatus_PROVIDER_STATUS_DEACTIVATED:
		return store.StatusDeactivated
	default:
		return store.StatusUnspecified
	}
}

func statusToProto(s store.Status) providersv1.ProviderStatus {
	switch s {
	case store.StatusActive:
		return providersv1.ProviderStatus_PROVIDER_STATUS_ACTIVE
	case store.StatusOffline:
		return providersv1.ProviderStatus_PROVIDER_STATUS_OFFLINE
	case store.StatusSuspended:
		return providersv1.ProviderStatus_PROVIDER_STATUS_SUSPENDED
	case store.StatusDeactivated:
		return providersv1.ProviderStatus_PROVIDER_STATUS_DEACTIVATED
	default:
		return providersv1.ProviderStatus_PROVIDER_STATUS_UNSPECIFIED
	}
}

// workloadTypeSlug maps the common WorkloadType enum to the lower-snake
// slug stored on the provider capability list and accepted by /provide
// settings forms.
func workloadTypeSlug(t commonv1.WorkloadType) string {
	switch t {
	case commonv1.WorkloadType_WORKLOAD_TYPE_BANDWIDTH:
		return "bandwidth"
	case commonv1.WorkloadType_WORKLOAD_TYPE_DOCKER:
		return "docker"
	case commonv1.WorkloadType_WORKLOAD_TYPE_GPU:
		return "gpu"
	case commonv1.WorkloadType_WORKLOAD_TYPE_IOS_BUILD:
		return "ios_build"
	default:
		return ""
	}
}

func slugToWorkloadType(s string) commonv1.WorkloadType {
	switch s {
	case "bandwidth":
		return commonv1.WorkloadType_WORKLOAD_TYPE_BANDWIDTH
	case "docker":
		return commonv1.WorkloadType_WORKLOAD_TYPE_DOCKER
	case "gpu":
		return commonv1.WorkloadType_WORKLOAD_TYPE_GPU
	case "ios_build":
		return commonv1.WorkloadType_WORKLOAD_TYPE_IOS_BUILD
	default:
		return commonv1.WorkloadType_WORKLOAD_TYPE_UNSPECIFIED
	}
}

func hostInfoFromProto(h *providersv1.HostInfo) store.HostInfo {
	if h == nil {
		return store.HostInfo{}
	}
	return store.HostInfo{
		Platform:        platformToStore(h.GetPlatform()),
		Architecture:    archToStore(h.GetArchitecture()),
		OSVersion:       h.GetOsVersion(),
		DaemonVersion:   h.GetDaemonVersion(),
		TotalMemoryMiB:  h.GetTotalMemoryMib(),
		CPUModel:        h.GetCpuModel(),
		CPULogicalCores: h.GetCpuLogicalCores(),
		GPUModels:       append([]string(nil), h.GetGpuModels()...),
		DockerAvailable: h.GetDockerAvailable(),
		TartAvailable:   h.GetTartAvailable(),
	}
}

func hostInfoToProto(h store.HostInfo) *providersv1.HostInfo {
	return &providersv1.HostInfo{
		Platform:        platformToProto(h.Platform),
		Architecture:    archToProto(h.Architecture),
		OsVersion:       h.OSVersion,
		DaemonVersion:   h.DaemonVersion,
		TotalMemoryMib:  h.TotalMemoryMiB,
		CpuModel:        h.CPUModel,
		CpuLogicalCores: h.CPULogicalCores,
		GpuModels:       append([]string(nil), h.GPUModels...),
		DockerAvailable: h.DockerAvailable,
		TartAvailable:   h.TartAvailable,
	}
}

func networkFromProto(n *providersv1.NetworkInfo) store.NetworkInfo {
	if n == nil {
		return store.NetworkInfo{}
	}
	region := n.GetInferredRegion()
	out := store.NetworkInfo{
		PublicIP:       n.GetPublicIp(),
		ASN:            n.GetAsn(),
		ISP:            n.GetIsp(),
		ThroughputMbps: n.GetThroughputMbps(),
		LatencyMs:      n.GetLatencyMs(),
	}
	if region != nil {
		out.RegionSlug = region.GetSlug()
		out.RegionName = region.GetDisplayName()
		out.CountryCode = region.GetCountryCode()
	}
	return out
}

func networkToProto(n store.NetworkInfo) *providersv1.NetworkInfo {
	return &providersv1.NetworkInfo{
		PublicIp:       n.PublicIP,
		Asn:            n.ASN,
		Isp:            n.ISP,
		ThroughputMbps: n.ThroughputMbps,
		LatencyMs:      n.LatencyMs,
		InferredRegion: &commonv1.Region{
			Slug:        n.RegionSlug,
			DisplayName: n.RegionName,
			CountryCode: n.CountryCode,
		},
	}
}

func capabilityFromProto(c *providersv1.CapabilityInventory) store.Capability {
	if c == nil {
		return store.Capability{}
	}
	slugs := make([]string, 0, len(c.GetSupportedWorkloadTypes()))
	for _, t := range c.GetSupportedWorkloadTypes() {
		if s := workloadTypeSlug(t); s != "" {
			slugs = append(slugs, s)
		}
	}
	return store.Capability{
		SupportedTypes:  slugs,
		GPUEnabled:      c.GetGpuEnabled(),
		IOSBuildEnabled: c.GetIosBuildEnabled(),
	}
}

func capabilityToProto(c store.Capability) *providersv1.CapabilityInventory {
	types := make([]commonv1.WorkloadType, 0, len(c.SupportedTypes))
	for _, s := range c.SupportedTypes {
		types = append(types, slugToWorkloadType(s))
	}
	return &providersv1.CapabilityInventory{
		SupportedWorkloadTypes: types,
		GpuEnabled:             c.GPUEnabled,
		IosBuildEnabled:        c.IOSBuildEnabled,
	}
}

func providerToProto(p *store.Provider) *providersv1.Provider {
	return &providersv1.Provider{
		Id:           uuidProto(p.ID),
		OwnerUserId:  uuidProto(p.OwnerUserID),
		DisplayName:  p.DisplayName,
		Status:       statusToProto(p.Status),
		HostInfo:     hostInfoToProto(p.HostInfo),
		NetworkInfo:  networkToProto(p.NetworkInfo),
		Capabilities: capabilityToProto(p.Capabilities),
		RegisteredAt: timestamppb.New(p.RegisteredAt),
		LastSeenAt:   timestamppb.New(p.LastSeenAt),
		IsPrimary:    p.IsPrimary,
	}
}

func tsOrZero(t *timestamppb.Timestamp) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.AsTime()
}
