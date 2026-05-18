import Link from "next/link";
import type { PricingTier } from "@/content/pricing";

export function PricingTable({
  tiers,
  caption,
}: {
  tiers: PricingTier[];
  caption?: string;
}) {
  return (
    <section className="container-page py-16">
      <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-4">
        {tiers.map((t) => (
          <article
            key={t.id}
            className={`card flex flex-col ${t.highlight ? "ring-2 ring-primary-500" : ""}`}
          >
            {t.highlight && (
              <span className="pill mb-3 self-start">Most popular</span>
            )}
            <h3 className="h-card text-neutral-900">{t.name}</h3>
            <p className="mt-2 text-sm text-neutral-600">{t.description}</p>
            <div className="mt-6">
              <span className="font-tabular text-4xl font-bold text-neutral-900">
                {t.price}
              </span>
              <span className="ml-2 text-sm text-neutral-500">{t.unit}</span>
            </div>
            <ul className="mt-6 flex-1 space-y-2 text-sm text-neutral-700">
              {t.features.map((f) => (
                <li key={f} className="flex items-start gap-2">
                  <Check />
                  <span>{f}</span>
                </li>
              ))}
            </ul>
            <Link href={t.cta.href} className="btn-primary mt-6 w-full">
              {t.cta.label}
            </Link>
          </article>
        ))}
      </div>
      {caption && (
        <p className="mt-8 text-center text-sm text-neutral-500">{caption}</p>
      )}
    </section>
  );
}

function Check() {
  return (
    <svg
      aria-hidden="true"
      width="16"
      height="16"
      viewBox="0 0 16 16"
      fill="none"
      className="mt-0.5 shrink-0 text-accent-500"
    >
      <path
        d="M3 8l3 3 7-7"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}
