// Static placeholder stats. Phase 1+ will fetch from coordinator API at build time
// (revalidate=300) or render live via a separate edge endpoint at iogrid.org.

const stats: Array<{ label: string; value: string; note?: string }> = [
  { label: "Active providers", value: "—", note: "live counter Phase 1" },
  { label: "Countries", value: "—", note: "global mesh, Phase 2" },
  { label: "Bytes audited", value: "—", note: "cumulative, on-chain Phase 2" },
  { label: "Bytes blocked", value: "—", note: "by anti-abuse pre-flight" },
];

export function Stats() {
  return (
    <section className="border-y border-neutral-200 bg-neutral-50">
      <div className="container-page py-12">
        <div className="grid gap-6 sm:grid-cols-2 md:grid-cols-4">
          {stats.map((s) => (
            <div key={s.label} className="text-center">
              <div className="font-tabular text-3xl font-bold text-neutral-900 md:text-4xl">
                {s.value}
              </div>
              <div className="mt-1 text-xs uppercase tracking-widest text-neutral-600">
                {s.label}
              </div>
              {s.note && (
                <div className="mt-1 text-xs text-neutral-600">{s.note}</div>
              )}
            </div>
          ))}
        </div>
        <p className="mt-8 text-center text-xs text-neutral-600">
          Stats refresh from the coordinator API once Phase 1 launches.
          We deliberately ship dashes until there&rsquo;s real volume — no vanity metrics.
        </p>
      </div>
    </section>
  );
}
