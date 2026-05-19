"use client";

import { signOut } from "next-auth/react";
import { Button } from "@/components/ui/button";

export function ProfileCard({
  name,
  email,
  image,
}: {
  name: string;
  email: string;
  image: string | null;
}) {
  return (
    <div className="space-y-4 rounded-md border border-zinc-200 bg-white p-6 dark:border-zinc-800 dark:bg-zinc-900">
      <div className="flex items-center gap-4">
        {image ? (
          // eslint-disable-next-line @next/next/no-img-element
          <img
            src={image}
            alt=""
            className="h-14 w-14 rounded-full border border-zinc-200 dark:border-zinc-800"
          />
        ) : (
          <div className="flex h-14 w-14 items-center justify-center rounded-full bg-zinc-100 text-xl font-semibold dark:bg-zinc-800">
            {name.slice(0, 1).toUpperCase() || "?"}
          </div>
        )}
        <div className="min-w-0 flex-1">
          <p className="text-lg font-semibold">{name || "Unnamed account"}</p>
          <p className="text-sm text-zinc-500">{email}</p>
        </div>
        <Button
          variant="outline"
          onClick={() => signOut({ callbackUrl: "/" })}
          aria-label="Sign out"
        >
          Sign out
        </Button>
      </div>
      <p className="text-xs text-zinc-500">
        Email + Google identifiers are merged server-side — adding a Google
        identity to an account that already has an email-only login keeps
        all your providing/customer history.
      </p>
    </div>
  );
}
