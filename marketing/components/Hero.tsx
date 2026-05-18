import Link from "next/link";
import type { ReactNode } from "react";

export interface HeroProps {
  eyebrow?: string;
  title: ReactNode;
  subtitle: ReactNode;
  primaryCta?: { href: string; label: string };
  secondaryCta?: { href: string; label: string };
  rightSlot?: ReactNode;
}

export function Hero({
  eyebrow,
  title,
  subtitle,
  primaryCta,
  secondaryCta,
  rightSlot,
}: HeroProps) {
  return (
    <section className="container-page py-16 md:py-24">
      <div className="grid items-center gap-12 lg:grid-cols-2">
        <div>
          {eyebrow && (
            <span className="pill mb-4">{eyebrow}</span>
          )}
          <h1 className="h-hero text-neutral-900">{title}</h1>
          <p className="mt-6 text-lead">{subtitle}</p>
          {(primaryCta || secondaryCta) && (
            <div className="mt-8 flex flex-wrap gap-3">
              {primaryCta && (
                <Link href={primaryCta.href} className="btn-primary">
                  {primaryCta.label}
                </Link>
              )}
              {secondaryCta && (
                <Link href={secondaryCta.href} className="btn-secondary">
                  {secondaryCta.label}
                </Link>
              )}
            </div>
          )}
        </div>
        {rightSlot && <div className="lg:justify-self-end">{rightSlot}</div>}
      </div>
    </section>
  );
}
