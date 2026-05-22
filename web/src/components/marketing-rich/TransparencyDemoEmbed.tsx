// CSS-only animated mock of the audit feed — no JS, no client component.
// Three rows scroll/pulse via Tailwind animations defined inline.

const sampleEvents = [
  {
    category: "E-commerce price monitoring",
    target: "amazon.com",
    customer: "MyShopMonitor",
    size: "4.2 MB",
    ago: "12s",
    color: "primary",
  },
  {
    category: "SEO rank check",
    target: "google.com",
    customer: "SerpTracker",
    size: "0.8 MB",
    ago: "23s",
    color: "primary",
  },
  {
    category: "Ad verification",
    target: "facebook.com",
    customer: "AdAuditPro",
    size: "1.1 MB",
    ago: "47s",
    color: "primary",
  },
  {
    category: "AI training data",
    target: "wikipedia.org",
    customer: "DatasetForge",
    size: "2.6 MB",
    ago: "1m",
    color: "primary",
  },
];

export function TransparencyDemoEmbed() {
  return (
    <div
      className="rounded-xl border border-neutral-200 bg-white p-5 shadow-md"
      aria-label="Mock audit dashboard"
    >
      <div className="mb-3 flex items-center justify-between text-xs">
        <span className="flex items-center gap-2 font-semibold text-neutral-700">
          <span className="relative flex h-2 w-2">
            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-accent-500 opacity-75" />
            <span className="relative inline-flex h-2 w-2 rounded-full bg-accent-500" />
          </span>
          Live audit
        </span>
        <span className="font-tabular text-neutral-500">updated 0.3s ago</span>
      </div>
      <ul className="space-y-2 font-mono text-xs">
        {sampleEvents.map((e) => (
          <li
            key={e.target + e.customer}
            className="rounded-md border border-neutral-100 bg-neutral-50 p-3"
          >
            <div className="flex items-center justify-between">
              <span className="font-semibold text-primary-700">
                {e.category}
              </span>
              <span className="font-tabular text-neutral-500">
                {e.ago} ago
              </span>
            </div>
            <div className="mt-1 flex items-center justify-between text-neutral-700">
              <span>
                &rarr; <span className="text-neutral-900">{e.target}</span>
              </span>
              <span className="font-tabular">{e.size}</span>
            </div>
            <div className="mt-1 text-neutral-500">
              customer: {e.customer}
            </div>
          </li>
        ))}
      </ul>
      <div className="mt-4 flex flex-wrap gap-2 text-xs">
        <button
          type="button"
          className="rounded border border-neutral-200 bg-white px-3 py-1.5 font-medium text-neutral-700 hover:border-primary-500"
        >
          Block category
        </button>
        <button
          type="button"
          className="rounded border border-neutral-200 bg-white px-3 py-1.5 font-medium text-neutral-700 hover:border-primary-500"
        >
          Block customer
        </button>
        <button
          type="button"
          className="rounded border border-neutral-200 bg-white px-3 py-1.5 font-medium text-neutral-700 hover:border-primary-500"
        >
          Block destination
        </button>
      </div>
    </div>
  );
}
