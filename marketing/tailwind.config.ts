import type { Config } from "tailwindcss";
import colorsTokens from "../brand/tokens/colors.json";
import spacingTokens from "../brand/tokens/spacing.json";

type TokenValue = { value: string };
type TokenGroup = Record<string, TokenValue>;

function flatten(group: TokenGroup): Record<string, string> {
  return Object.fromEntries(
    Object.entries(group).map(([k, v]) => [k, v.value]),
  );
}

const config: Config = {
  content: [
    "./app/**/*.{ts,tsx,mdx}",
    "./components/**/*.{ts,tsx}",
    "./content/**/*.{ts,tsx,mdx}",
  ],
  theme: {
    extend: {
      colors: {
        primary: flatten(colorsTokens.primary as TokenGroup),
        accent: flatten(colorsTokens.accent as TokenGroup),
        neutral: flatten(colorsTokens.neutral as TokenGroup),
        success: colorsTokens.semantic.success.value,
        warning: colorsTokens.semantic.warning.value,
        danger: colorsTokens.semantic.danger.value,
        info: colorsTokens.semantic.info.value,
      },
      borderRadius: flatten(spacingTokens.radius as TokenGroup),
      maxWidth: {
        "container-sm": spacingTokens.container.sm.value,
        "container-md": spacingTokens.container.md.value,
        "container-lg": spacingTokens.container.lg.value,
        "container-xl": spacingTokens.container.xl.value,
        "container-2xl": spacingTokens.container["2xl"].value,
      },
      fontFamily: {
        sans: [
          "var(--font-inter)",
          "system-ui",
          "-apple-system",
          "Segoe UI",
          "sans-serif",
        ],
        mono: [
          "var(--font-jetbrains-mono)",
          "ui-monospace",
          "SFMono-Regular",
          "Menlo",
          "monospace",
        ],
      },
      fontFeatureSettings: {
        tabular: '"tnum" 1, "lnum" 1',
      },
    },
  },
  plugins: [
    function ({ addUtilities }: { addUtilities: (u: Record<string, Record<string, string>>) => void }) {
      addUtilities({
        ".font-tabular": {
          "font-feature-settings": '"tnum" 1, "lnum" 1',
        },
      });
    },
  ],
};

export default config;
