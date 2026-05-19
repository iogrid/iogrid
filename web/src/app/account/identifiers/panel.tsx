"use client";

import * as React from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { browserApi } from "@/lib/api";
import type { MeResponse, Identifier } from "@/lib/types";

export function IdentifiersPanel() {
  const [me, setMe] = React.useState<MeResponse | null>(null);
  const [loading, setLoading] = React.useState(true);

  React.useEffect(() => {
    browserApi()
      .get<MeResponse>("/api/v1/me")
      .then(setMe)
      .catch((e) => toast.error((e as Error).message))
      .finally(() => setLoading(false));
  }, []);

  if (loading) {
    return (
      <div className="rounded-md border border-zinc-200 p-8 text-center text-sm text-zinc-500 dark:border-zinc-800">
        Loading identifiers…
      </div>
    );
  }
  if (!me) {
    return (
      <div className="rounded-md border border-rose-200 bg-rose-50 p-4 text-sm text-rose-700 dark:border-rose-900 dark:bg-rose-950 dark:text-rose-300">
        Couldn&apos;t load identifiers. Try refreshing or sign back in.
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <ul className="divide-y divide-zinc-200 rounded-md border border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800">
        {(me.identifiers ?? []).map((id) => (
          <IdentifierRow key={id.id?.value ?? id.value} id={id} />
        ))}
        {(me.identifiers ?? []).length === 0 ? (
          <li className="p-4 text-sm text-zinc-500">
            No identifiers bound. Add one to keep the account recoverable.
          </li>
        ) : null}
      </ul>
      <p className="text-xs text-zinc-500">
        Adding a new identifier opens a verification flow in the identity
        service. Removing the last verified identifier is blocked server-side.
      </p>
    </div>
  );
}

function IdentifierRow({ id }: { id: Identifier }) {
  return (
    <li className="flex items-center justify-between p-3 text-sm">
      <div className="min-w-0 flex-1">
        <p className="font-medium">{id.value}</p>
        <p className="text-xs text-zinc-500">
          {id.kind === "EMAIL" ? "Email" : id.kind === "GOOGLE" ? "Google" : id.kind}
          {id.verified ? (
            <span className="ml-2 rounded-full bg-emerald-100 px-1.5 py-0.5 text-[10px] font-medium text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300">
              Verified
            </span>
          ) : (
            <span className="ml-2 rounded-full bg-amber-100 px-1.5 py-0.5 text-[10px] font-medium text-amber-700 dark:bg-amber-950 dark:text-amber-300">
              Pending
            </span>
          )}
        </p>
      </div>
      <Button
        size="sm"
        variant="outline"
        onClick={() =>
          toast.info(
            "Identifier removal isn't wired yet — the identity-svc RPC lands in #37.",
          )
        }
      >
        Remove
      </Button>
    </li>
  );
}
