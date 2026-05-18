import type { ReactNode } from "react";

export interface Feature {
  icon?: ReactNode;
  title: string;
  body: string;
}

export function FeatureGrid({
  title,
  subtitle,
  features,
  columns = 3,
}: {
  title?: string;
  subtitle?: string;
  features: Feature[];
  columns?: 2 | 3 | 4;
}) {
  const gridCols = {
    2: "md:grid-cols-2",
    3: "md:grid-cols-2 lg:grid-cols-3",
    4: "md:grid-cols-2 lg:grid-cols-4",
  }[columns];
  return (
    <section className="container-page py-16">
      {(title || subtitle) && (
        <div className="mx-auto max-w-3xl text-center">
          {title && <h2 className="h-section text-neutral-900">{title}</h2>}
          {subtitle && (
            <p className="mt-4 text-lead">{subtitle}</p>
          )}
        </div>
      )}
      <div className={`mt-12 grid gap-6 ${gridCols}`}>
        {features.map((f) => (
          <article key={f.title} className="card">
            {f.icon && <div className="mb-4 text-primary-500">{f.icon}</div>}
            <h3 className="h-card text-neutral-900">{f.title}</h3>
            <p className="mt-2 text-sm leading-6 text-neutral-600">{f.body}</p>
          </article>
        ))}
      </div>
    </section>
  );
}
