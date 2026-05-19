import type { Metadata } from "next";
import { Toaster } from "sonner";
import "./globals.css";

export const metadata: Metadata = {
  title: "iogrid — Distributed compute mesh",
  description:
    "iogrid is a distributed compute mesh that turns idle machines into a shared, schedulable fleet.",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en">
      <body className="min-h-screen antialiased">
        {children}
        {/* Sonner toast container — every mutation (block category, save
           schedule, create API key, ...) routes through `toast.*` so we
           can swap implementations without touching call sites. */}
        <Toaster richColors closeButton position="top-right" />
      </body>
    </html>
  );
}
