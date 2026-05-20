import type { Metadata } from "next";
import { Toaster } from "sonner";
import { ThemeProvider } from "@/components/theme-provider";
import "./globals.css";

export const metadata: Metadata = {
  title: "iogrid admin",
  description:
    "iogrid staff console — abuse review, KYC, provider audits, financial operations.",
  robots: { index: false, follow: false },
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    // `suppressHydrationWarning` is required by next-themes — the
    // provider mutates `<html class="...">` and `style.colorScheme`
    // before React hydrates so the first paint already matches the
    // resolved theme (system preference or persisted choice).
    <html lang="en" suppressHydrationWarning>
      <body className="min-h-screen antialiased">
        {/* ThemeProvider thinly wraps next-themes so we can centralise
            its config (class-based strategy, `system` default,
            enableSystem). The Toaster sits inside so toast surfaces
            pick up the current theme automatically. */}
        <ThemeProvider>
          {children}
          <Toaster
            richColors
            closeButton
            position="top-right"
            theme="system"
          />
        </ThemeProvider>
      </body>
    </html>
  );
}
