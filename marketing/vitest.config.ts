import { defineConfig } from "vitest/config";
import path from "node:path";

// Marketing vitest config — matches the shape of web/vitest.config.ts
// so contributors only have to learn one. We don't need jsdom yet
// because the only logic-with-tests is the pure detect-os module.
// Add jsdom (and @testing-library/react) here if a future PR wants to
// unit-test the React `InstallButton` component itself.

export default defineConfig({
  esbuild: {
    jsx: "automatic",
  },
  test: {
    environment: "node",
    globals: true,
    include: ["lib/**/*.test.{ts,tsx}", "content/**/*.test.{ts,tsx}"],
    exclude: ["node_modules/**", ".next/**", "out/**"],
  },
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./"),
    },
  },
});
