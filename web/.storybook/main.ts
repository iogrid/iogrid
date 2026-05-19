import type { StorybookConfig } from "@storybook/nextjs";

/**
 * Storybook config for the iogrid web app.
 *
 * Stories live next to the components under `src/components/**`. Keep
 * the addon list minimal — we don't ship a11y/links/etc. until the
 * design-system PRs land.
 */
const config: StorybookConfig = {
  stories: ["../src/components/**/*.stories.@(ts|tsx)"],
  framework: {
    name: "@storybook/nextjs",
    options: {},
  },
  docs: { autodocs: false },
};

export default config;
