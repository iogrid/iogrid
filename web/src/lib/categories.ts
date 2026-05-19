/**
 * Canonical workload categories the provider can opt into. The slugs
 * MUST match what providers-svc emits in AuditEvent.category and what
 * the CategoryOptIn proto stores. Keep this list in sync with
 * docs/CATEGORIES.md.
 */
export interface Category {
  slug: string;
  label: string;
  description: string;
  /** Approximate number of active customers in this category. */
  customers: number;
}

export const CATEGORIES: Category[] = [
  {
    slug: "e_commerce",
    label: "E-commerce price intelligence",
    description:
      "Track competitor pricing for clothing, electronics, travel and groceries.",
    customers: 142,
  },
  {
    slug: "seo",
    label: "SEO rank monitoring",
    description:
      "Serp scrapers checking organic + paid result positions per market.",
    customers: 89,
  },
  {
    slug: "ad_verification",
    label: "Ad verification",
    description:
      "Confirm display + video ads render correctly across countries and devices.",
    customers: 67,
  },
  {
    slug: "brand_protection",
    label: "Brand protection",
    description:
      "Detect counterfeit marketplaces, phishing domains, and stolen IP.",
    customers: 41,
  },
  {
    slug: "market_research",
    label: "Market research",
    description: "Survey local listings (rentals, jobs, automotive) at scale.",
    customers: 53,
  },
  {
    slug: "academic",
    label: "Academic research",
    description:
      "University corpus collection — published datasets, news archives.",
    customers: 27,
  },
  {
    slug: "ai_training",
    label: "AI training data",
    description:
      "Crawl public-web pages for ML training. Robots.txt is always honoured.",
    customers: 38,
  },
  {
    slug: "qa_uptime",
    label: "QA + uptime monitoring",
    description:
      "Synthetic checks that customer-facing sites work from real residential IPs.",
    customers: 31,
  },
];

export function findCategory(slug: string): Category | undefined {
  return CATEGORIES.find((c) => c.slug === slug);
}
