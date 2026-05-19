import type { Metadata, Viewport } from "next";
import { Inter, JetBrains_Mono } from "next/font/google";
import { Nav } from "@/components/Nav";
import { Footer } from "@/components/Footer";
import "./globals.css";

const inter = Inter({
  subsets: ["latin"],
  variable: "--font-inter",
  display: "swap",
});

const jetbrainsMono = JetBrains_Mono({
  subsets: ["latin"],
  variable: "--font-jetbrains-mono",
  display: "swap",
});

const SITE_URL = process.env.NEXT_PUBLIC_SITE_URL ?? "https://iogrid.org";

export const metadata: Metadata = {
  metadataBase: new URL(SITE_URL),
  title: {
    default:
      "iogrid — the transparent mesh network for bandwidth, compute, GPU, and iOS builds",
    template: "%s · iogrid",
  },
  description:
    "iogrid is a transparent mesh network. Providers share idle bandwidth and compute and see every byte categorized in real time. Customers buy residential proxy, Docker, GPU, and iOS-build CI at 30–60% below market.",
  keywords: [
    "residential proxy",
    "distributed compute",
    "GPU inference",
    "iOS build CI",
    "mesh VPN",
    "transparency",
  ],
  authors: [{ name: "iogrid" }],
  openGraph: {
    type: "website",
    locale: "en_US",
    url: SITE_URL,
    siteName: "iogrid",
    title: "iogrid — transparent mesh network",
    description:
      "Bandwidth, compute, GPU, iOS builds. Live audit dashboard. Cash, free VPN, or $GRID payout.",
    images: [
      {
        url: "/og-image.png",
        width: 1200,
        height: 630,
        alt: "iogrid",
      },
    ],
  },
  twitter: {
    card: "summary_large_image",
    title: "iogrid — transparent mesh network",
    description:
      "Bandwidth, compute, GPU, iOS builds. Live audit dashboard. Cash, free VPN, or $GRID payout.",
    images: ["/og-image.png"],
  },
  icons: {
    icon: "/favicon.svg",
  },
  robots: {
    index: true,
    follow: true,
    googleBot: {
      index: true,
      follow: true,
    },
  },
};

export const viewport: Viewport = {
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: "#FFFFFF" },
    { media: "(prefers-color-scheme: dark)", color: "#15140F" },
  ],
  width: "device-width",
  initialScale: 1,
};

const organizationJsonLd = {
  "@context": "https://schema.org",
  "@type": "Organization",
  name: "iogrid",
  url: SITE_URL,
  logo: `${SITE_URL}/favicon.svg`,
  description:
    "Transparent mesh network for residential bandwidth, Docker compute, GPU inference, and iOS-build CI. Every byte categorized, every provider in control.",
  sameAs: [
    "https://github.com/iogrid",
    "https://docs.iogrid.org",
    "https://status.iogrid.org",
  ],
  contactPoint: [
    {
      "@type": "ContactPoint",
      contactType: "customer support",
      url: `${SITE_URL}/about`,
      availableLanguage: ["English"],
    },
  ],
};

const breadcrumbJsonLd = {
  "@context": "https://schema.org",
  "@type": "BreadcrumbList",
  itemListElement: [
    {
      "@type": "ListItem",
      position: 1,
      name: "Home",
      item: SITE_URL,
    },
    {
      "@type": "ListItem",
      position: 2,
      name: "Products",
      item: `${SITE_URL}/proxy`,
    },
    {
      "@type": "ListItem",
      position: 3,
      name: "Pricing",
      item: `${SITE_URL}/pricing`,
    },
    {
      "@type": "ListItem",
      position: 4,
      name: "Earn with iogrid",
      item: `${SITE_URL}/providers`,
    },
    {
      "@type": "ListItem",
      position: 5,
      name: "Blog",
      item: `${SITE_URL}/blog`,
    },
  ],
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en" className={`${inter.variable} ${jetbrainsMono.variable}`}>
      <head>
        <script
          type="application/ld+json"
          dangerouslySetInnerHTML={{
            __html: JSON.stringify(organizationJsonLd),
          }}
        />
        <script
          type="application/ld+json"
          dangerouslySetInnerHTML={{
            __html: JSON.stringify(breadcrumbJsonLd),
          }}
        />
      </head>
      <body className="min-h-screen font-sans antialiased">
        <a
          href="#main"
          className="sr-only focus:not-sr-only focus:fixed focus:left-4 focus:top-4 focus:z-50 focus:rounded focus:bg-primary-500 focus:px-4 focus:py-2 focus:text-white"
        >
          Skip to content
        </a>
        <Nav />
        <main id="main" className="flex-1">
          {children}
        </main>
        <Footer />
      </body>
    </html>
  );
}
