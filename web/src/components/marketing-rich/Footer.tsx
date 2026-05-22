import Link from "next/link";

const cols = [
  {
    title: "Products",
    links: [
      { href: "/proxy", label: "Bandwidth proxy" },
      { href: "/compute", label: "Docker compute" },
      { href: "/gpu", label: "GPU inference" },
      { href: "/ios-build", label: "iOS build CI" },
      { href: "/vpn", label: "Consumer VPN" },
    ],
  },
  {
    title: "Network",
    links: [
      { href: "/providers", label: "Earn with iogrid" },
      { href: "/pricing", label: "Pricing" },
      { href: "/token", label: "$GRID token" },
      { href: "https://status.iogrid.org", label: "Status", external: true },
    ],
  },
  {
    title: "Resources",
    links: [
      { href: "/blog", label: "Blog" },
      { href: "/docs", label: "Documentation" },
      { href: "/about", label: "About" },
      {
        href: "https://github.com/iogrid",
        label: "GitHub",
        external: true,
      },
    ],
  },
  {
    title: "Legal",
    links: [
      { href: "/legal/tos", label: "Terms of service" },
      { href: "/legal/privacy", label: "Privacy" },
      { href: "/legal/aup", label: "Acceptable use" },
    ],
  },
];

export function Footer() {
  return (
    <footer className="mt-24 border-t border-neutral-200 bg-neutral-50">
      <div className="container-page py-12">
        <div className="grid grid-cols-2 gap-8 md:grid-cols-4">
          {cols.map((col) => (
            <div key={col.title}>
              <h3 className="text-xs font-semibold uppercase tracking-widest text-neutral-500">
                {col.title}
              </h3>
              <ul className="mt-4 space-y-2">
                {col.links.map((l) => (
                  <li key={l.href}>
                    {"external" in l && l.external ? (
                      <a
                        href={l.href}
                        rel="noopener"
                        className="text-sm text-neutral-600 hover:text-primary-600"
                      >
                        {l.label}
                      </a>
                    ) : (
                      <Link
                        href={l.href}
                        className="text-sm text-neutral-600 hover:text-primary-600"
                      >
                        {l.label}
                      </Link>
                    )}
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>

        <div className="mt-12 flex flex-col items-start justify-between gap-4 border-t border-neutral-200 pt-6 md:flex-row md:items-center">
          <p className="text-sm text-neutral-500">
            &copy; {new Date().getFullYear()} iogrid. The network is yours.
          </p>
          <p className="text-xs text-neutral-400">
            Always-on transparency. No hidden routing. No surprises.
          </p>
        </div>
      </div>
    </footer>
  );
}
