import Link from "next/link";

const platforms = [
  {
    id: "mac",
    label: "macOS",
    sublabel: "Apple Silicon or Intel",
    href: "/install/mac",
    icon: (
      <svg width="22" height="22" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
        <path d="M16.5 0c.1 1.6-.5 3-1.5 4-1 1-2.5 1.8-4 1.7-.1-1.5.6-3 1.5-4 1-1 2.6-1.8 4-1.7zm4.5 17c-.7 1.6-1 2.3-1.9 3.7-1.3 2-3.1 4.5-5.3 4.5-2 0-2.5-1.3-5.2-1.3-2.7 0-3.3 1.3-5.3 1.3-2.2 0-3.9-2.2-5.2-4.2C-1.5 17 .3 11 4 9c1.6-.9 3-.9 4.4-.9 1.4 0 2.6.9 4.1.9 1.4 0 2.5-.9 4.4-.9 1.7 0 3.6.9 4.9 2.5-4.3 2.4-3.6 8.5-.8 6.4z" />
      </svg>
    ),
  },
  {
    id: "win",
    label: "Windows",
    sublabel: "x64 or ARM64",
    href: "/install/win",
    icon: (
      <svg width="22" height="22" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
        <path d="M0 3.5L9.75 2.2v9.45H0V3.5zm10.95-1.45L24 0v11.65H10.95V2.05zM0 12.85h9.75v9.45L0 21V12.85zm10.95 0H24V24l-13.05-1.85V12.85z" />
      </svg>
    ),
  },
  {
    id: "linux",
    label: "Linux",
    sublabel: "deb · rpm · apk",
    href: "/install/linux",
    icon: (
      <svg width="22" height="22" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
        <path d="M12 0C7.6 0 6 3.6 6 6c0 1.5.4 2.7.8 3.7C5.3 11.6 4 14 4 17c0 3 1 5 2.5 6.2.6.5 1.4.6 2 .2.4-.3.5-.8.4-1.3-.2-.7-.5-1.4-.5-2.3 0-.9.5-1.8 1.5-1.8.3 0 .5.2.6.5.2.5.3 1.2.8 1.8.5.7 1.4 1.2 2.7 1.2s2.2-.5 2.7-1.2c.5-.6.6-1.3.8-1.8.1-.3.3-.5.6-.5 1 0 1.5.9 1.5 1.8 0 .9-.3 1.6-.5 2.3-.1.5 0 1 .4 1.3.6.4 1.4.3 2-.2C19 22 20 20 20 17c0-3-1.3-5.4-2.8-7.3.4-1 .8-2.2.8-3.7 0-2.4-1.6-6-6-6z"/>
      </svg>
    ),
  },
];

export function InstallButtons({ variant = "primary" }: { variant?: "primary" | "secondary" }) {
  const btnClass = variant === "primary" ? "btn-primary" : "btn-secondary";
  return (
    <div className="grid gap-3 sm:grid-cols-3">
      {platforms.map((p) => (
        <Link
          key={p.id}
          href={p.href}
          className={`${btnClass} h-auto flex-col gap-1 py-4`}
        >
          <span className="flex items-center gap-2">
            <span aria-hidden="true">{p.icon}</span>
            <span>{p.label}</span>
          </span>
          <span className="text-xs font-normal opacity-80">{p.sublabel}</span>
        </Link>
      ))}
    </div>
  );
}
