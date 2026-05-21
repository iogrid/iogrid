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
    <div className="space-y-4 rounded-md border border-border bg-card p-6 dark:border-border">
      <div className="flex items-center gap-4">
        {image ? (
          // eslint-disable-next-line @next/next/no-img-element
          <img
            src={image}
            alt=""
            className="h-14 w-14 rounded-full border border-border dark:border-border"
          />
        ) : (
          <div className="flex h-14 w-14 items-center justify-center rounded-full bg-muted text-xl font-semibold dark:bg-muted">
            {name.slice(0, 1).toUpperCase() || "?"}
          </div>
        )}
        <div className="min-w-0 flex-1">
          <p className="text-lg font-semibold">{name || "Unnamed account"}</p>
          <p className="text-sm text-muted-foreground">{email}</p>
        </div>
        <Button
          variant="outline"
          onClick={() => signOut({ callbackUrl: "/" })}
          aria-label="Sign out"
        >
          Sign out
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">
        Email + Google identifiers are merged server-side — adding a Google
        identity to an account that already has an email-only login keeps
        all your providing/customer history.
      </p>
    </div>
  );
}
