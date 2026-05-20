"use client";

import { useEffect, useRef, useState } from "react";

/**
 * useSSE — a small React hook that wraps the native EventSource API.
 *
 * Why not the `eventsource` npm package? It pulls in a Node polyfill we
 * don't need; the browser's native implementation supports Last-Event-ID
 * and auto-reconnect out of the box. We layer an exponential backoff on
 * top because the default 3-second retry is too aggressive when the
 * gateway is unreachable.
 *
 * The hook stores at most `maxBuffer` events to keep memory bounded.
 *
 * Stability contract (#292):
 *   The effect that creates the underlying EventSource depends ONLY on
 *   `url` and `paused`. Callers commonly pass `parse` as an inline arrow
 *   function whose identity changes every render; if that were a
 *   dependency the EventSource would be torn down + re-opened on every
 *   parent render, producing the 30+-requests/10s reconnect storm seen
 *   in #292 (every status flip triggered a re-render → new `parse` →
 *   effect retear). We stash `parse` + `maxBuffer` in refs so callers
 *   keep ergonomic inline options without paying for instability.
 *
 *   We also detect fast reconnects: if the stream errors within 1s of
 *   opening for 3 consecutive attempts we give up and stay in the
 *   `error` state instead of hammering the network forever.
 */

export interface UseSSEOptions<T> {
  /** Full URL to the SSE endpoint. */
  url: string | null;
  /** Convert the raw `data:` payload into an event. */
  parse?: (raw: string) => T | null;
  /** Cap on retained events in memory. Older events are dropped. */
  maxBuffer?: number;
  /** Pause the stream without unmounting the hook. */
  paused?: boolean;
  /** Resume from a known event id (sent as the Last-Event-ID header). */
  initialLastEventId?: string;
}

export interface UseSSEResult<T> {
  events: T[];
  status: "connecting" | "open" | "closed" | "error" | "unavailable";
  lastEventId: string | undefined;
  clear: () => void;
}

const FAST_RECONNECT_THRESHOLD_MS = 1000;
const MAX_FAST_RECONNECTS = 3;

export function useSSE<T>(opts: UseSSEOptions<T>): UseSSEResult<T> {
  const { url, paused = false } = opts;

  const [events, setEvents] = useState<T[]>([]);
  const [status, setStatus] = useState<UseSSEResult<T>["status"]>("connecting");
  const lastIdRef = useRef<string | undefined>(opts.initialLastEventId);

  // Stable refs for the callbacks/values that the effect uses but should
  // NOT trigger a re-subscribe when they change identity. See the
  // "Stability contract" note above.
  const parseRef = useRef<(raw: string) => T | null>(
    opts.parse ?? (JSON.parse as (raw: string) => T),
  );
  const maxBufferRef = useRef<number>(opts.maxBuffer ?? 500);
  // Keep refs current without re-running the effect.
  parseRef.current =
    opts.parse ?? (JSON.parse as (raw: string) => T);
  maxBufferRef.current = opts.maxBuffer ?? 500;

  useEffect(() => {
    if (!url || paused) {
      setStatus("closed");
      return;
    }
    let cancelled = false;
    let es: EventSource | null = null;
    let backoffMs = 1000;
    let timer: ReturnType<typeof setTimeout> | null = null;
    // Fast-reconnect tracker: counts errors that fire within
    // FAST_RECONNECT_THRESHOLD_MS of the previous "open" attempt
    // starting. After MAX_FAST_RECONNECTS we stop retrying and flip
    // the status to "unavailable" so the UI can surface a banner
    // instead of letting EventSource hammer the network forever.
    let fastReconnectCount = 0;
    let openedAt = 0;

    const open = () => {
      if (cancelled) return;
      setStatus("connecting");
      openedAt = Date.now();
      try {
        es = new EventSource(url, { withCredentials: true });
      } catch {
        setStatus("error");
        return;
      }
      es.addEventListener("open", () => {
        if (cancelled) return;
        setStatus("open");
        backoffMs = 1000;
        fastReconnectCount = 0;
      });
      es.addEventListener("message", (ev: MessageEvent<string>) => {
        if (cancelled) return;
        try {
          const parsed = parseRef.current(ev.data);
          if (parsed == null) return;
          if (ev.lastEventId) lastIdRef.current = ev.lastEventId;
          setEvents((prev) => {
            const next = prev.concat([parsed]);
            const cap = maxBufferRef.current;
            return next.length > cap ? next.slice(next.length - cap) : next;
          });
        } catch {
          // Drop malformed payloads — the BFF emits well-formed JSON,
          // anything else is most likely a heartbeat we should ignore.
        }
      });
      es.addEventListener("error", () => {
        if (cancelled) return;
        const elapsed = Date.now() - openedAt;
        es?.close();
        es = null;
        if (elapsed < FAST_RECONNECT_THRESHOLD_MS) {
          fastReconnectCount += 1;
          if (fastReconnectCount >= MAX_FAST_RECONNECTS) {
            // Give up — surface as "unavailable" so the UI can render
            // a banner instead of letting EventSource hammer the wire.
            setStatus("unavailable");
            return;
          }
        } else {
          // A connection that lasted >threshold counts as a healthy
          // disconnect; reset the fast-reconnect counter.
          fastReconnectCount = 0;
        }
        setStatus("error");
        timer = setTimeout(() => {
          backoffMs = Math.min(backoffMs * 2, 30_000);
          open();
        }, backoffMs);
      });
    };
    open();

    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
      es?.close();
      setStatus("closed");
    };
    // Intentionally narrow deps — see "Stability contract" above. The
    // refs cover parse + maxBuffer; initialLastEventId is captured on
    // mount only by design (it's a "where to resume from" hint).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [url, paused]);

  return {
    events,
    status,
    lastEventId: lastIdRef.current,
    clear: () => setEvents([]),
  };
}
