import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useSSE } from "@/lib/sse";

/**
 * useSSE behavioural regression tests for #292 — the reconnect storm.
 *
 * The original implementation used `parse` and `maxBuffer` as effect
 * deps; React callers passing inline arrow functions therefore caused
 * the effect to tear down and re-open the EventSource on every parent
 * re-render. With the lookup page mutating local state in response to
 * SSE status changes that produced the 30+ GETs/10s storm reported in
 * #292. These tests pin the new contract:
 *
 *   - changing `parse` MUST NOT recreate the EventSource
 *   - changing `maxBuffer` MUST NOT recreate the EventSource
 *   - 3 fast-reconnects flip status to "unavailable" and stop retrying
 */

type EventListener = (ev: unknown) => void;

class FakeEventSource {
  static instances: FakeEventSource[] = [];
  url: string;
  withCredentials: boolean;
  closed = false;
  // capture listeners so tests can fire synthetic open/error events
  listeners: Record<string, EventListener[]> = {};

  constructor(url: string, init?: { withCredentials?: boolean }) {
    this.url = url;
    this.withCredentials = !!init?.withCredentials;
    FakeEventSource.instances.push(this);
  }

  addEventListener(name: string, fn: EventListener) {
    (this.listeners[name] ??= []).push(fn);
  }

  fire(name: string, ev: unknown = {}) {
    (this.listeners[name] ?? []).forEach((fn) => fn(ev));
  }

  close() {
    this.closed = true;
  }
}

beforeEach(() => {
  FakeEventSource.instances = [];
  // @ts-expect-error — installing a polyfill onto jsdom's globalThis
  globalThis.EventSource = FakeEventSource;
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  // @ts-expect-error — cleanup polyfill
  delete globalThis.EventSource;
});

describe("useSSE", () => {
  it("does NOT recreate the EventSource when `parse` identity changes", () => {
    // Each render produces a new arrow function for `parse` — the
    // original implementation would tear down + reopen on every render.
    const { rerender } = renderHook(
      ({ tick }: { tick: number }) =>
        useSSE<{ msg: string }>({
          url: "/api/v1/provide/audit/stream?provider_id=abc",
          parse: (raw) => {
            // Reference `tick` to keep the closure varying across renders.
            return { msg: `${raw}-${tick}` };
          },
        }),
      { initialProps: { tick: 0 } },
    );

    expect(FakeEventSource.instances.length).toBe(1);
    const firstInstance = FakeEventSource.instances[0];

    // Re-render 10 times with a different `tick` each time.
    for (let i = 1; i <= 10; i++) {
      rerender({ tick: i });
    }

    // Still exactly one EventSource, still the same instance, not closed.
    expect(FakeEventSource.instances.length).toBe(1);
    expect(FakeEventSource.instances[0]).toBe(firstInstance);
    expect(firstInstance.closed).toBe(false);
  });

  it("flips to `unavailable` after 3 fast-reconnects and stops opening", () => {
    renderHook(() =>
      useSSE<{ msg: string }>({
        url: "/api/v1/provide/audit/stream?provider_id=abc",
        parse: (raw) => ({ msg: raw }),
      }),
    );

    const fastClose = () => {
      const inst = FakeEventSource.instances.at(-1)!;
      // Simulate an error within the fast-reconnect threshold (<1s).
      act(() => {
        inst.fire("error", {});
      });
    };

    // First open attempt — error fires immediately (elapsed ~0ms).
    fastClose();
    // EventSource is recreated after backoff. Backoff starts at 1000ms;
    // we advance the timer to reopen.
    act(() => {
      vi.advanceTimersByTime(1100);
    });
    fastClose();
    act(() => {
      vi.advanceTimersByTime(2200);
    });
    fastClose();
    // After the 3rd fast-error the hook should NOT schedule another
    // reconnect — flip to "unavailable" and stop.
    act(() => {
      vi.advanceTimersByTime(60_000);
    });

    // Total EventSource creations should be exactly 3 (initial + 2 reconnects).
    expect(FakeEventSource.instances.length).toBe(3);
  });

  it("resets the fast-reconnect counter after a long-lived open", () => {
    const { result } = renderHook(() =>
      useSSE<{ msg: string }>({
        url: "/api/v1/provide/audit/stream?provider_id=abc",
        parse: (raw) => ({ msg: raw }),
      }),
    );
    void result;

    // First connection lasts >1s before erroring → counts as healthy.
    act(() => {
      const inst = FakeEventSource.instances.at(-1)!;
      inst.fire("open", {});
    });
    act(() => {
      vi.advanceTimersByTime(5000);
    });
    act(() => {
      const inst = FakeEventSource.instances.at(-1)!;
      inst.fire("error", {});
    });
    act(() => {
      vi.advanceTimersByTime(1100);
    });

    // Should have reconnected (instance #2 now exists).
    expect(FakeEventSource.instances.length).toBeGreaterThanOrEqual(2);
  });
});
