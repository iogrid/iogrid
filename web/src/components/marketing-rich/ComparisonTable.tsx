// Pulled from docs/COMPETITORS.md — the master comparison condensed
// to the columns that matter for marketing audiences.

const headers = [
  "",
  "Bandwidth proxy",
  "Docker compute",
  "GPU inference",
  "iOS build CI",
  "Free VPN",
  "Live audit",
  "Multi-currency payout",
];

const rows: Array<{
  player: string;
  cells: Array<"yes" | "no" | "partial" | string>;
}> = [
  {
    player: "iogrid",
    cells: ["yes", "yes", "yes", "yes", "yes", "yes", "yes"],
  },
  {
    player: "Bright Data",
    cells: ["yes", "no", "no", "no", "no", "no", "no"],
  },
  {
    player: "Honeygain",
    cells: ["yes", "no", "no", "no", "no", "no", "no"],
  },
  {
    player: "Salad",
    cells: ["no", "yes", "yes", "no", "no", "partial", "partial"],
  },
  {
    player: "Vast.ai",
    cells: ["no", "partial", "yes", "no", "no", "partial", "no"],
  },
  {
    player: "io.net",
    cells: ["no", "partial", "yes", "no", "no", "no", "no"],
  },
  {
    player: "Mysterium",
    cells: ["partial", "no", "no", "no", "yes", "partial", "no"],
  },
  {
    player: "GitHub Actions Mac",
    cells: ["no", "no", "no", "yes", "no", "yes", "no"],
  },
];

const cellBadge = {
  yes: { label: "Yes", classes: "bg-accent-700 text-white" },
  no: { label: "—", classes: "bg-neutral-200 text-neutral-700" },
  partial: { label: "Partial", classes: "bg-warning-500 text-neutral-900" },
};

export function ComparisonTable() {
  return (
    <section className="container-page py-16">
      <div className="mx-auto max-w-3xl text-center">
        <h2 className="h-section text-neutral-900">
          The only horizontally integrated mesh
        </h2>
        <p className="mt-4 text-lead">
          Every incumbent occupies one quadrant. iogrid covers all of them, plus
          the iOS-build wedge no one else has touched.
        </p>
      </div>
      <div className="mt-12 overflow-x-auto rounded-xl border border-neutral-200 bg-white">
        <table className="w-full text-left text-sm">
          <thead>
            <tr className="border-b border-neutral-200 bg-neutral-50">
              {headers.map((h, i) => (
                <th
                  key={h || `h${i}`}
                  scope="col"
                  className="whitespace-nowrap px-4 py-3 text-xs font-semibold uppercase tracking-wider text-neutral-600"
                >
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((row, i) => {
              const isUs = row.player === "iogrid";
              return (
                <tr
                  key={row.player}
                  className={`${i % 2 === 0 ? "bg-white" : "bg-neutral-50/60"} ${isUs ? "bg-primary-50/70 font-semibold" : ""}`}
                >
                  <th
                    scope="row"
                    className="px-4 py-3 text-left text-neutral-900"
                  >
                    {row.player}
                  </th>
                  {row.cells.map((c, j) => {
                    const badge =
                      cellBadge[c as keyof typeof cellBadge] ??
                      cellBadge.no;
                    return (
                      <td key={j} className="px-4 py-3">
                        <span
                          className={`inline-flex min-w-[3rem] justify-center rounded-full px-2.5 py-0.5 text-xs font-semibold ${badge.classes}`}
                        >
                          {badge.label}
                        </span>
                      </td>
                    );
                  })}
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <p className="mt-6 text-center text-xs text-neutral-600">
        Detailed analysis in our public competitive matrix at /blog and on
        GitHub.
      </p>
    </section>
  );
}
