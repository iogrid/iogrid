/**
 * Helpers for decoding proto3 enums that arrive over the gateway-bff
 * JSON wire.
 *
 * The Go BFF (`coordinator/services/gateway-bff/...`) serialises Connect-Go
 * proto structs with the standard library `json.Marshal`, which emits enum
 * fields as their numeric tag (e.g. `{"state": 1}`) — NOT the canonical
 * proto3-JSON string form (`{"state": "SCHEDULER_STATE_ACTIVE"}`).
 *
 * The TypeScript view code historically branched on the string form, so a
 * numeric `state` always fell through to the default branch ("Unknown" /
 * "Event" / etc.). See iogrid/iogrid#314.
 *
 * The buf-generated TypeScript enums under `web/src/lib/pb/` are the proto
 * source-of-truth but we intentionally do NOT pull them into the browser
 * bundle — they drag in `@bufbuild/protobuf` runtime which doubles the
 * shipped JS (see `src/lib/api.ts` rationale). Instead, this module
 * declares small (number to SCREAMING_SNAKE_CASE name) maps that mirror
 * the proto definitions under `proto/iogrid/` 1:1.
 *
 * When a new enum value is added to a .proto file, the corresponding map
 * here must be extended. The `proto-enum.test.ts` regression suite asserts
 * round-tripping for every value currently in use.
 */

/**
 * `iogrid.providers.v1.SchedulerState` — mirrors
 * `proto/iogrid/providers/v1/scheduling.proto::SchedulerState`.
 */
export const SchedulerStateNames: Record<number, string> = {
  0: "SCHEDULER_STATE_UNSPECIFIED",
  1: "SCHEDULER_STATE_ACTIVE",
  2: "SCHEDULER_STATE_PAUSED_BANDWIDTH_CAP",
  3: "SCHEDULER_STATE_PAUSED_CPU_CAP",
  4: "SCHEDULER_STATE_PAUSED_MEMORY_CAP",
  5: "SCHEDULER_STATE_PAUSED_OUTSIDE_CALENDAR",
  6: "SCHEDULER_STATE_PAUSED_USER_ACTIVE",
  7: "SCHEDULER_STATE_PAUSED_OPERATIONS",
};

/**
 * `iogrid.providers.v1.EventKind` — mirrors
 * `proto/iogrid/providers/v1/dashboard.proto::EventKind`.
 */
export const EventKindNames: Record<number, string> = {
  0: "EVENT_KIND_UNSPECIFIED",
  1: "EVENT_KIND_WORKLOAD_DISPATCHED",
  2: "EVENT_KIND_WORKLOAD_COMPLETED",
  3: "EVENT_KIND_WORKLOAD_BLOCKED",
  4: "EVENT_KIND_SCHEDULER_TRANSITION",
  5: "EVENT_KIND_ABUSE_FLAGGED",
  6: "EVENT_KIND_EARNINGS_CREDITED",
};

/**
 * `iogrid.common.v1.WorkloadType` — mirrors
 * `proto/iogrid/common/v1/types.proto::WorkloadType`.
 */
export const WorkloadTypeNames: Record<number, string> = {
  0: "WORKLOAD_TYPE_UNSPECIFIED",
  1: "WORKLOAD_TYPE_BANDWIDTH",
  2: "WORKLOAD_TYPE_DOCKER",
  3: "WORKLOAD_TYPE_GPU",
  4: "WORKLOAD_TYPE_IOS_BUILD",
};

/**
 * Resolve a proto enum value (either the numeric tag from a `json.Marshal`
 * wire payload or the canonical string from a proto3-JSON wire payload)
 * to its canonical SCREAMING_SNAKE_CASE proto name.
 *
 * Returns `undefined` when the value is `undefined` / `null`. Returns the
 * stringified value unchanged when the numeric tag is unknown — that way
 * downstream defaults (e.g. "Unknown" pill) still fire instead of crashing.
 */
export function protoEnumName(
  value: number | string | null | undefined,
  names: Record<number, string>,
): string | undefined {
  if (value === null || value === undefined) return undefined;
  if (typeof value === "string") return value;
  return names[value] ?? String(value);
}

/**
 * Convenience: the SchedulerState short name (`"ACTIVE"`,
 * `"PAUSED_BANDWIDTH_CAP"`, …) that the `/provide` overview switches on.
 * Strips the `SCHEDULER_STATE_` proto prefix.
 */
export function schedulerStateShortName(
  value: number | string | null | undefined,
): string | undefined {
  const full = protoEnumName(value, SchedulerStateNames);
  if (full === undefined) return undefined;
  return full.startsWith("SCHEDULER_STATE_")
    ? full.slice("SCHEDULER_STATE_".length)
    : full;
}
