import * as React from "react";

// Layout passthrough — the actual chrome is rendered by PortalShell at
// the page level so each page can choose its active section tab. We
// keep this layout file in place so future protected nav (e.g. a
// per-provider switcher) lands here without touching every page.

export default function ProvideLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return <>{children}</>;
}
