import { PortalShell } from "@/components/layout/portal-shell";
import { PROVIDE_NAV } from "@/app/provide/nav";
import { ScheduleEditor } from "./editor";

export const metadata = {
  title: "Schedule — iogrid",
};

/**
 * /provide/schedule — caps sliders + calendar + categories + blocklist
 * in one form. The page is a server component that just renders the
 * client island; the island GETs its own initial state so navigating
 * here doesn't block on the request.
 */
export default function ProvideSchedulePage() {
  return (
    <PortalShell
      badge="Provider"
      title="Schedule"
      subtitle="Resource caps, calendar windows, accepted categories and blocked destinations."
      nav={PROVIDE_NAV}
      activeHref="/provide/schedule"
    >
      <ScheduleEditor />
    </PortalShell>
  );
}
