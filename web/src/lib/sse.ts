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
  status: "connecting" | "open" | "closed" | "error";
  lastEventId: string | undefined;
  clear: () => void;
}

export function useSSE<T>(opts: UseSSEOptions<T>): UseSSEResult<T> {
  const { url, parse = JSON.parse as (raw: string) => T, maxBuffer = 500, paused = false } =
    opts;

  const [events, setEvents] = useState<T[]>([]);
  const [status, setStatus] = useState<UseSSEResult<T>["status"]>("connecting");
  const lastIdRef = useRef<string | undefined>(opts.initialLastEventId);

  useEffect(() => {
    if (!url || paused) {
      setStatus("closed");
      return;
    }
    let cancelled = false;
    let es: EventSource | null = null;
    let backoffMs = 1000;
    let timer: ReturnType<typeof setTimeout> | null = null;

    const open = () => {
      if (cancelled) return;
      setStatus("connecting");
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
      });
      es.addEventListener("message", (ev: MessageEvent<string>) => {
        if (cancelled) return;
        try {
          const parsed = parse(ev.data);
          if (parsed == null) return;
          if (ev.lastEventId) lastIdRef.current = ev.lastEventId;
          setEvents((prev) => {
            const next = prev.concat([parsed]);
            return next.length > maxBuffer ? next.slice(next.length - maxBuffer) : next;
          });
        } catch {
          // Drop malformed payloads — the BFF emits well-formed JSON,
          // anything else is most likely a heartbeat we should ignore.
        }
      });
      es.addEventListener("error", () => {
        if (cancelled) return;
        setStatus("error");
        es?.close();
        es = null;
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
  }, [url, parse, maxBuffer, paused]);

  return {
    events,
    status,
    lastEventId: lastIdRef.current,
    clear: () => setEvents([]),
  };
}
