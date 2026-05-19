import * as React from "react";

// Layout passthrough; PortalShell is rendered per-page so each route
// owns its active-tab choice. Same pattern as /provide/layout.tsx.
export default function CustomerLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return <>{children}</>;
}
