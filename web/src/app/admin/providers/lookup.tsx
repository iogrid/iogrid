"use client";

import * as React from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { AuditEventCard } from "@/components/dashboard/audit-event-card";
import { browserApi } from "@/lib/api";
import { useSSE } from "@/lib/sse";
import type { AuditEvent } from "@/lib/types";

export function ProviderAuditLookup() {
  const [providerId, setProviderId] = React.useState("");
  const [streamFor, setStreamFor] = React.useState<string | null>(null);

  const url = React.useMemo(() => {
    if (!streamFor) return null;
    return `${browserApi().baseUrl}/api/v1/provide/audit/stream?provider_id=${streamFor}`;
  }, [streamFor]);

  const { events, status } = useSSE<AuditEvent>({
    url,
    parse: (raw) => {
      try {
        return JSON.parse(raw) as AuditEvent;
      } catch {
        return null;
      }
    },
  });

  return (
    <div className="space-y-4">
      <form
        onSubmit={(e) => {
          e.preventDefault();
          setStreamFor(providerId.trim() || null);
        }}
        className="flex gap-2"
      >
        <Input
          type="text"
          value={providerId}
          onChange={(e) => setProviderId(e.target.value)}
          placeholder="Provider UUID"
          aria-label="Provider UUID"
          pattern="[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}"
          className="font-mono"
        />
        <Button type="submit">Audit</Button>
      </form>

      {streamFor ? (
        <p className="text-xs text-zinc-500">
          Streaming events for{" "}
          <code className="font-mono">{streamFor}</code> — status: {status}
        </p>
      ) : (
        <p className="text-xs text-zinc-500">
          Enter a provider UUID and press Audit to open their transparency
          stream.
        </p>
      )}

      <ul className="space-y-2">
        {events.slice().reverse().slice(0, 50).map((ev, i) => (
          <li key={ev.id?.value ?? i}>
            <AuditEventCard event={ev} />
          </li>
        ))}
      </ul>
    </div>
  );
}
