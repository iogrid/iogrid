import { MarketingShell } from "./marketing-shell";

/**
 * Shared scaffold for the three legal pages (ToS, Privacy, AUP).
 * Folded from marketing/ into web/ in EPIC #422 Phase 3.
 *
 * Each page still reads as a placeholder until counsel drafts final
 * language — the visible "Placeholder" pill makes this honest to any
 * reader who arrives via search / footer link.
 */
export function LegalPage({
  title,
  lastUpdated,
  children,
}: {
  title: string;
  lastUpdated: string;
  children: React.ReactNode;
}) {
  return (
    <MarketingShell>
      <article className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16 md:py-20">
          <span className="inline-flex items-center gap-2 rounded-full border border-border bg-muted px-3 py-1 text-xs font-medium text-muted-foreground">
            <span aria-hidden className="h-1.5 w-1.5 rounded-full bg-primary" />
            Placeholder
          </span>
          <h1 className="mt-6 text-3xl font-semibold tracking-tight text-foreground md:text-4xl lg:text-5xl">
            {title}
          </h1>
          <p className="mt-4 text-sm text-muted-foreground">
            Last updated: {lastUpdated} — final language will be drafted by
            qualified counsel before Phase 1 launch.
          </p>

          <div className="prose prose-sm mt-12 max-w-none space-y-6 text-base leading-relaxed text-muted-foreground [&_h2]:mt-10 [&_h2]:text-xs [&_h2]:font-medium [&_h2]:uppercase [&_h2]:tracking-wider [&_h2]:text-foreground [&_strong]:text-foreground [&_a]:text-foreground [&_a]:underline-offset-2 hover:[&_a]:underline">
            {children}
          </div>

          <p className="mt-12 rounded-lg border border-border bg-muted p-4 text-sm text-muted-foreground">
            This page is a public scaffold so the URL is reachable from
            navigation and the design is in place. The substantive legal text
            will be replaced ahead of paid traffic.
          </p>
        </div>
      </article>
    </MarketingShell>
  );
}
