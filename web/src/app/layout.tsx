import type { Metadata } from "next";
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
      <body className="min-h-screen antialiased">{children}</body>
    </html>
  );
}
