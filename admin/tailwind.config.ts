import type { Config } from "tailwindcss";

/**
 * admin/ Tailwind config (EPIC #422 Phase 1).
 *
 * Mirrors web/ for now so admin routes look identical to where they
 * came from. The world-class UX revamp for admin surfaces lands in
 * EPIC #422 Phase 2.3 — at that point this file diverges from web/.
 */
const config: Config = {
  content: [
    "./src/app/**/*.{ts,tsx,mdx}",
    "./src/components/**/*.{ts,tsx,mdx}",
    "./src/lib/**/*.{ts,tsx,mdx}",
  ],
  theme: {
    extend: {
      colors: {
        brand: {
          50: "#f0f9ff",
          500: "#0ea5e9",
          900: "#0c4a6e",
        },
      },
    },
  },
  plugins: [],
};

export default config;
