#!/usr/bin/env node
// Convert public/og-default.svg -> public/og-default.png at 1200x630.
// Runs in CI before `astro build` so OG metadata always has a PNG to reference.
// Twitter / Slack / Discord prefer PNG over SVG for OG previews.

import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";
import sharp from "sharp";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(__dirname, "..");
const SRC = path.join(ROOT, "public", "og-default.svg");
const DEST = path.join(ROOT, "public", "og-default.png");

const svg = readFileSync(SRC);
const png = await sharp(svg, { density: 144 })
  .resize(1200, 630, { fit: "fill" })
  .png({ compressionLevel: 9, palette: true })
  .toBuffer();
writeFileSync(DEST, png);
console.log(`[og] wrote ${DEST} (${png.length} bytes)`);
