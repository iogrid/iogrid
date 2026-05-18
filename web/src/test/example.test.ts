import { describe, expect, it } from "vitest";
import { cn } from "@/lib/utils";

describe("cn (className merger)", () => {
  it("joins truthy class names", () => {
    expect(cn("a", "b", false && "c", "d")).toBe("a b d");
  });

  it("dedupes conflicting tailwind classes via tailwind-merge", () => {
    expect(cn("px-2", "px-4")).toBe("px-4");
  });
});
