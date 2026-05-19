// Starlight content collection config — Astro 6 loader-based form.
// Required by Astro 6+; the legacy `src/content/config.ts` form is removed.
// See https://docs.astro.build/en/guides/upgrade-to/v6/#removed-legacy-content-collections

import { defineCollection } from "astro:content";
import { docsLoader } from "@astrojs/starlight/loaders";
import { docsSchema } from "@astrojs/starlight/schema";

export const collections = {
  docs: defineCollection({ loader: docsLoader(), schema: docsSchema() }),
};
