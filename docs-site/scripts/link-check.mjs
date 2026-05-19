#!/usr/bin/env node
// link-check.mjs — verify every internal link in the built docs site resolves to
// an actual file in dist/. Runs in CI after `astro build`.
//
// We intentionally do NOT validate external links here — they 503 / 429 / move
// far too often, and a docs build should never fail because GitHub had a
// hiccup. External-link sweeps are a manual quarterly job.

import { readdir, readFile, stat } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(__dirname, "..");
const DIST = path.join(ROOT, "dist");

/** @type {string[]} */
const errors = [];

async function* walk(dir) {
  const entries = await readdir(dir, { withFileTypes: true });
  for (const entry of entries) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) yield* walk(full);
    else yield full;
  }
}

async function pathExists(p) {
  try {
    await stat(p);
    return true;
  } catch {
    return false;
  }
}

const LINK_RE = /href=(?:"|')(\/[^"'#?]*)(?:[?#][^"']*)?(?:"|')/g;

async function main() {
  let scanned = 0;
  let checked = 0;
  for await (const file of walk(DIST)) {
    if (!file.endsWith(".html")) continue;
    scanned++;
    const html = await readFile(file, "utf8");
    let match;
    while ((match = LINK_RE.exec(html)) !== null) {
      const href = match[1];
      // Skip well-known paths that aren't files (sitemap, redirects)
      if (
        href.startsWith("/_astro") ||
        href === "/" ||
        href === "/index.html"
      ) {
        continue;
      }
      checked++;
      // Trailing-slash style — Astro emits /foo/ which maps to /foo/index.html
      let target;
      if (href.endsWith("/")) {
        target = path.join(DIST, href, "index.html");
      } else if (
        href.endsWith(".html") ||
        href.endsWith(".yaml") ||
        href.endsWith(".xml") ||
        href.endsWith(".svg") ||
        href.endsWith(".png") ||
        href.endsWith(".webp") ||
        href.endsWith(".pdf") ||
        href.endsWith(".txt") ||
        href.endsWith(".json")
      ) {
        target = path.join(DIST, href);
      } else {
        // Bare path — try as directory first, then with .html suffix
        const asDir = path.join(DIST, href, "index.html");
        const asHtml = path.join(DIST, href + ".html");
        target = (await pathExists(asDir)) ? asDir : asHtml;
      }
      if (!(await pathExists(target))) {
        errors.push(`${path.relative(DIST, file)} -> ${href} (resolved to ${path.relative(DIST, target)})`);
      }
    }
  }
  console.log(
    `[link-check] scanned ${scanned} HTML files, checked ${checked} internal links`,
  );
  if (errors.length) {
    console.error(`[link-check] FOUND ${errors.length} broken links:`);
    for (const e of errors) console.error(`  - ${e}`);
    process.exit(1);
  }
  console.log("[link-check] OK");
}

await main();
