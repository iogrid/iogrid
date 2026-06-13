// rest_capability.go implements the REST shim the Rust daemon POSTs to on
// startup to refresh its providers-svc capability record (#746).
//
// ## Why this exists
//
// The daemon's only providers-svc write used to be the one-shot pairing
// handshake (`POST /api/v1/providers/pair`), which carries the CSR +
// display name but NO capabilities — the row is created with
// supported_types={}, ios_build_enabled=false, platform=NULL. Nothing
// refreshed the capabilities afterwards: the dispatch stream advertises
// them to workloads-svc (so dispatch works) and StreamHeartbeats only
// bumps last_seen_at. So a Mac that gains iOS-build capability after first
// pairing (Xcode installed later) stayed `ios_build_enabled=false` in the
// admin / provider dashboard forever.
//
// This shim lets the daemon push its live capability snapshot on every
// startup. Like rest_pair.go it translates the daemon's lean flat-JSON
// shape into the canonical Connect `UpdateCapabilityInventory` RPC and
// invokes the in-process handler, so the RegistrationHandler stays the
// single source of truth.
package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	providersv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1"
)

// capabilityRESTRequest mirrors the daemon's CapabilityReportBody serde
// shape (daemon/crates/core/src/capability_report.rs).
type capabilityRESTRequest struct {
	// SupportedTypes are workload slugs the daemon can run right now,
	// e.g. ["BANDWIDTH", "IOS_BUILD"]. Mapped to the proto WorkloadType
	// enum via slugToWorkloadType (case-insensitive there).
	SupportedTypes []string `json:"supported_types"`
	// GPUEnabled / IOSBuildEnabled are the per-capability booleans the
	// daemon derived from its slug list. Carried explicitly so the server
	// doesn't re-derive them (the daemon is the source of truth for what
	// it will actually accept).
	GPUEnabled      bool `json:"gpu_enabled"`
	IOSBuildEnabled bool `json:"ios_build_enabled"`
	// HostMacosVersion is the host macOS MAJOR version (14 = Sonoma, 15 =
	// Sequoia); 0 = unknown / not macOS.
	HostMacosVersion uint32 `json:"host_macos_version"`
}

// CapabilityReportREST upserts a paired provider's capability record from
// the daemon's flat JSON shape. Mounted at
// `POST /api/v1/providers/{id}/capabilities`. The {id} path param is the
// provider id assigned at pairing. Returns 4xx on validation errors and
// 5xx on internal failures; the body is always a {"error":"..."} envelope
// on failure (matching rest_pair.go).
func (h *RegistrationHandler) CapabilityReportREST(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	providerID := chi.URLParam(r, "id")
	if providerID == "" {
		writeJSONError(w, http.StatusBadRequest, "provider id required in path")
		return
	}

	var in capabilityRESTRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeJSONError(w, http.StatusBadRequest, "malformed json: "+err.Error())
		return
	}

	// Translate the slug list into proto WorkloadType enums. The daemon
	// sends UPPER-case slugs ("BANDWIDTH", "IOS_BUILD") from
	// eligible_workload_types(); slugToWorkloadType keys on lower-case, so
	// normalise. Unknown slugs are dropped (UNSPECIFIED) rather than
	// failing the whole request — forward-compat with a newer daemon that
	// advertises a type this server build doesn't know yet.
	types := make([]commonv1.WorkloadType, 0, len(in.SupportedTypes))
	for _, s := range in.SupportedTypes {
		if t := slugToWorkloadType(strings.ToLower(strings.TrimSpace(s))); t != commonv1.WorkloadType_WORKLOAD_TYPE_UNSPECIFIED {
			types = append(types, t)
		}
	}

	req := &providersv1.UpdateCapabilityInventoryRequest{
		ProviderId: uuidProto(providerID),
		Capabilities: &providersv1.CapabilityInventory{
			SupportedWorkloadTypes: types,
			GpuEnabled:             in.GPUEnabled,
			IosBuildEnabled:        in.IOSBuildEnabled,
			HostMacosVersion:       in.HostMacosVersion,
		},
	}

	if _, err := h.UpdateCapabilityInventory(r.Context(), connect.NewRequest(req)); err != nil {
		var ce *connect.Error
		if errors.As(err, &ce) {
			writeJSONError(w, connectCodeToHTTP(ce.Code()), ce.Message())
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
