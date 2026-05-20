import { describe, expect, it } from "vitest";

import {
  EventKindNames,
  SchedulerStateNames,
  WorkloadTypeNames,
  protoEnumName,
  schedulerStateShortName,
} from "@/lib/proto-enum";

/**
 * Regression coverage for iogrid/iogrid#314.
 *
 * gateway-bff serialises Connect-Go proto structs with `encoding/json`,
 * which emits enum fields as their numeric tag. The TS view code used to
 * switch on the canonical string form and always fell through to the
 * "Unknown" default. `protoEnumName()` canonicalises both wire forms.
 */
describe("protoEnumName", () => {
  it("maps a numeric SchedulerState tag to the full canonical name", () => {
    expect(protoEnumName(1, SchedulerStateNames)).toBe(
      "SCHEDULER_STATE_ACTIVE",
    );
    expect(protoEnumName(7, SchedulerStateNames)).toBe(
      "SCHEDULER_STATE_PAUSED_OPERATIONS",
    );
  });

  it("passes a string value through unchanged", () => {
    expect(protoEnumName("ACTIVE", SchedulerStateNames)).toBe("ACTIVE");
    expect(
      protoEnumName("SCHEDULER_STATE_ACTIVE", SchedulerStateNames),
    ).toBe("SCHEDULER_STATE_ACTIVE");
  });

  it("returns undefined for null / undefined input", () => {
    expect(protoEnumName(undefined, SchedulerStateNames)).toBeUndefined();
    expect(protoEnumName(null, SchedulerStateNames)).toBeUndefined();
  });

  it("stringifies unknown numeric tags (forward-compat with new enum values)", () => {
    expect(protoEnumName(999, SchedulerStateNames)).toBe("999");
  });

  it("maps numeric EventKind tags to canonical names", () => {
    expect(protoEnumName(1, EventKindNames)).toBe(
      "EVENT_KIND_WORKLOAD_DISPATCHED",
    );
    expect(protoEnumName(3, EventKindNames)).toBe("EVENT_KIND_WORKLOAD_BLOCKED");
  });

  it("maps numeric WorkloadType tags to canonical names", () => {
    expect(protoEnumName(1, WorkloadTypeNames)).toBe("WORKLOAD_TYPE_BANDWIDTH");
    expect(protoEnumName(4, WorkloadTypeNames)).toBe("WORKLOAD_TYPE_IOS_BUILD");
  });
});

describe("schedulerStateShortName", () => {
  it("strips the SCHEDULER_STATE_ prefix from a numeric tag", () => {
    expect(schedulerStateShortName(1)).toBe("ACTIVE");
    expect(schedulerStateShortName(2)).toBe("PAUSED_BANDWIDTH_CAP");
    expect(schedulerStateShortName(7)).toBe("PAUSED_OPERATIONS");
  });

  it("strips the prefix even when the wire delivers the long string form", () => {
    expect(schedulerStateShortName("SCHEDULER_STATE_ACTIVE")).toBe("ACTIVE");
  });

  it("returns the value unchanged when the prefix is already stripped", () => {
    expect(schedulerStateShortName("ACTIVE")).toBe("ACTIVE");
  });

  it("returns undefined for nullish input", () => {
    expect(schedulerStateShortName(undefined)).toBeUndefined();
    expect(schedulerStateShortName(null)).toBeUndefined();
  });
});
