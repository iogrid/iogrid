"use client";

import * as React from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { browserApi } from "@/lib/api";

const PLATFORMS = [
  { id: "macos-arm64", os: "macOS", arch: "Apple silicon", filename: "iogrid-darwin-arm64.pkg" },
  { id: "linux", os: "Linux", arch: "x86_64 / arm64", filename: "iogrid-linux.tar.gz" },
  { id: "windows", os: "Windows", arch: "x86_64", filename: "iogrid-windows.msi" },
  { id: "ios", os: "iOS", arch: "ARM64", filename: "iogrid.mobileconfig" },
  { id: "android", os: "Android", arch: "ARM64 / x86_64", filename: "iogrid.apk" },
];

export function Downloads() {
  return (
    <section className="mt-10 grid grid-cols-1 gap-4 md:grid-cols-3">
      {PLATFORMS.map((p) => (
        <DownloadCard key={p.id} {...p} />
      ))}
    </section>
  );
}

function DownloadCard({
  id,
  os,
  arch,
  filename,
}: {
  id: string;
  os: string;
  arch: string;
  filename: string;
}) {
  const [busy, setBusy] = React.useState(false);
  const onDownload = async () => {
    setBusy(true);
    try {
      // The BFF's /vpn/config-for-platform endpoint streams either the
      // VPN config artefact OR redirects to the daemon-binary CDN URL
      // (see coordinator/services/vpn-gateway). We hit it via a hidden
      // anchor so the browser handles streaming + content-disposition.
      const a = document.createElement("a");
      a.href = `${browserApi().baseUrl}/api/v1/vpn/config-for-platform?platform=${id}`;
      a.download = filename;
      document.body.appendChild(a);
      a.click();
      a.remove();
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="rounded-lg border border-zinc-200 bg-white p-5 dark:border-zinc-800 dark:bg-zinc-900">
      <h2 className="font-semibold">{os}</h2>
      <p className="text-xs text-zinc-500">{arch}</p>
      <p className="mt-4 font-mono text-xs text-zinc-600 dark:text-zinc-400">
        {filename}
      </p>
      <Button onClick={onDownload} disabled={busy} className="mt-4 w-full">
        {busy ? "Preparing…" : "Download"}
      </Button>
    </div>
  );
}
