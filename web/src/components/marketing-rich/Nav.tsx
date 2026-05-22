import Link from "next/link";
import { ThemeToggle } from "@/components/theme-toggle";

const productLinks = [
  { href: "/proxy", label: "Bandwidth proxy" },
  { href: "/compute", label: "Docker compute" },
  { href: "/gpu", label: "GPU inference" },
  { href: "/ios-build", label: "iOS build CI" },
  { href: "/vpn", label: "Consumer VPN" },
];

const primaryLinks = [
  { href: "/pricing", label: "Pricing" },
  { href: "/providers", label: "Earn with iogrid" },
  { href: "/token", label: "$GRID" },
  { href: "/blog", label: "Blog" },
];

export function Nav() {
  return (
    <header className="sticky top-0 z-40 border-b border-neutral-200 bg-white/85 backdrop-blur supports-[backdrop-filter]:bg-white/70">
      <div className="container-page flex h-16 items-center justify-between">
        <Link
          href="/"
          aria-label="iogrid home"
          className="flex items-center gap-2 text-neutral-900"
        >
          <Logo />
          <span className="text-lg font-extrabold tracking-tight">iogrid</span>
        </Link>

        <nav
          aria-label="Primary"
          className="hidden items-center gap-1 lg:flex"
        >
          <details className="group relative">
            <summary className="btn-ghost cursor-pointer list-none">
              Products
              <svg
                aria-hidden="true"
                className="ml-1 h-3 w-3 transition group-open:rotate-180"
                viewBox="0 0 12 12"
                fill="none"
              >
                <path d="M3 5l3 3 3-3" stroke="currentColor" strokeWidth="1.5" />
              </svg>
            </summary>
            <div className="absolute left-0 top-full mt-2 w-64 rounded-lg border border-neutral-200 bg-white p-2 shadow-lg">
              {productLinks.map((l) => (
                <Link
                  key={l.href}
                  href={l.href}
                  className="block rounded px-3 py-2 text-sm text-neutral-700 hover:bg-neutral-100 hover:text-primary-600"
                >
                  {l.label}
                </Link>
              ))}
            </div>
          </details>
          {primaryLinks.map((l) => (
            <Link key={l.href} href={l.href} className="btn-ghost">
              {l.label}
            </Link>
          ))}
        </nav>

        <div className="flex items-center gap-2">
          <ThemeToggle />
          <Link
            href="/providers"
            className="hidden btn-secondary md:inline-flex"
          >
            Become a provider
          </Link>
          <Link href="/pricing" className="btn-primary">
            Get started
          </Link>
        </div>
      </div>
    </header>
  );
}

function Logo() {
  return (
    <svg
      width="28"
      height="28"
      viewBox="0 0 64 64"
      fill="none"
      aria-hidden="true"
    >
      <g stroke="#4257F5" strokeWidth="2.5" strokeLinecap="round">
        <line x1="32" y1="10" x2="51.05" y2="21" />
        <line x1="51.05" y1="21" x2="51.05" y2="43" />
        <line x1="51.05" y1="43" x2="32" y2="54" />
        <line x1="32" y1="54" x2="12.95" y2="43" />
        <line x1="12.95" y1="43" x2="12.95" y2="21" />
        <line x1="12.95" y1="21" x2="32" y2="10" />
        <line x1="32" y1="10" x2="32" y2="54" strokeOpacity="0.55" />
        <line x1="51.05" y1="21" x2="12.95" y2="43" strokeOpacity="0.55" />
        <line x1="12.95" y1="21" x2="51.05" y2="43" strokeOpacity="0.55" />
      </g>
      <g fill="#4257F5">
        <circle cx="32" cy="10" r="3.5" />
        <circle cx="51.05" cy="21" r="3.5" />
        <circle cx="51.05" cy="43" r="3.5" />
        <circle cx="32" cy="54" r="3.5" />
        <circle cx="12.95" cy="43" r="3.5" />
        <circle cx="12.95" cy="21" r="3.5" />
      </g>
      <circle cx="32" cy="32" r="4.5" fill="#2EC78B" />
    </svg>
  );
}
