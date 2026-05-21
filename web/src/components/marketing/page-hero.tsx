/**
 * Compact page hero for marketing-folded surfaces — about, pricing,
 * legal/*, status. Mirrors the visual rhythm of the apex landing's
 * <Hero/> but tighter (no large CTAs).
 *
 * Folded from marketing/components/Hero.tsx into web/'s design
 * tokens during EPIC #422 Phase 3.
 */
export function PageHero({
  eyebrow,
  title,
  subtitle,
}: {
  eyebrow?: string;
  title: string;
  subtitle?: React.ReactNode;
}) {
  return (
    <section className="border-b border-border">
      <div className="mx-auto max-w-3xl px-6 py-16 md:py-24">
        {eyebrow ? (
          <div className="mb-6 inline-flex items-center gap-2 rounded-full border border-border px-3 py-1 text-xs text-muted-foreground">
            <span aria-hidden className="h-1.5 w-1.5 rounded-full bg-primary" />
            {eyebrow}
          </div>
        ) : null}
        <h1 className="text-3xl font-semibold tracking-tight text-foreground md:text-4xl lg:text-5xl">
          {title}
        </h1>
        {subtitle ? (
          <div className="mt-6 max-w-2xl text-lg leading-relaxed text-muted-foreground">
            {subtitle}
          </div>
        ) : null}
      </div>
    </section>
  );
}
