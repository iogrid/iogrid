import { defineConfig } from "vitest/config";
import path from "node:path";

export default defineConfig({
  // Vitest transforms TS/TSX via esbuild. Force the automatic JSX
  // runtime so test files don't need an explicit `import React`. This
  // mirrors how Next.js compiles JSX in the app.
  esbuild: {
    jsx: "automatic",
  },
  test: {
    environment: "jsdom",
    globals: true,
    include: ["src/**/*.test.{ts,tsx}"],
    exclude: ["tests/**", "node_modules/**", ".next/**"],
  },
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
});
