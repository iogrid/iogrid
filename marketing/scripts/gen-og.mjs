// Generates public/og-image.png (1200x630) from an SVG template.
// Run via `pnpm og:gen` whenever the brand or messaging changes.
//
// Self-contained: no template engine, no external fonts (uses system sans-serif
// in the SVG, which sharp's librsvg renders fine on Linux/macOS).

import sharp from "sharp";
import { writeFile, mkdir } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);
const outPath = join(__dirname, "..", "public", "og-image.png");

const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="630" viewBox="0 0 1200 630">
  <defs>
    <linearGradient id="bg" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0" stop-color="#10174A"/>
      <stop offset="1" stop-color="#22309C"/>
    </linearGradient>
  </defs>
  <rect width="1200" height="630" fill="url(#bg)"/>

  <!-- decorative mesh -->
  <g stroke="#FFFFFF" stroke-opacity="0.15" stroke-width="1.5">
    <line x1="0" y1="120"   x2="1200" y2="240"/>
    <line x1="0" y1="280"   x2="1200" y2="380"/>
    <line x1="0" y1="460"   x2="1200" y2="520"/>
    <line x1="180"  y1="0" x2="220"  y2="630"/>
    <line x1="600"  y1="0" x2="540"  y2="630"/>
    <line x1="980"  y1="0" x2="1060" y2="630"/>
  </g>

  <!-- mark -->
  <g transform="translate(80, 100)">
    <g stroke="#FFFFFF" stroke-width="4" stroke-linecap="round">
      <line x1="60" y1="10"   x2="103" y2="35" />
      <line x1="103" y1="35"  x2="103" y2="85" />
      <line x1="103" y1="85"  x2="60"  y2="110" />
      <line x1="60"  y1="110" x2="17"  y2="85" />
      <line x1="17"  y1="85"  x2="17"  y2="35" />
      <line x1="17"  y1="35"  x2="60"  y2="10" />
      <line x1="60"  y1="10"  x2="60"  y2="110" stroke-opacity="0.55" />
      <line x1="103" y1="35"  x2="17"  y2="85"  stroke-opacity="0.55" />
      <line x1="17"  y1="35"  x2="103" y2="85"  stroke-opacity="0.55" />
    </g>
    <g fill="#FFFFFF">
      <circle cx="60"  cy="10"  r="7" />
      <circle cx="103" cy="35"  r="7" />
      <circle cx="103" cy="85"  r="7" />
      <circle cx="60"  cy="110" r="7" />
      <circle cx="17"  cy="85"  r="7" />
      <circle cx="17"  cy="35"  r="7" />
    </g>
    <circle cx="60" cy="60" r="10" fill="#2EC78B" />
  </g>

  <!-- wordmark + tagline -->
  <text x="80" y="320" font-family="Inter, system-ui, sans-serif" font-size="120" font-weight="800" letter-spacing="-2" fill="#FFFFFF">iogrid</text>
  <text x="80" y="400" font-family="Inter, system-ui, sans-serif" font-size="44" font-weight="600" fill="#FFFFFF" fill-opacity="0.92">The transparent mesh network.</text>
  <text x="80" y="470" font-family="Inter, system-ui, sans-serif" font-size="32" font-weight="400" fill="#FFFFFF" fill-opacity="0.75">Bandwidth. Compute. GPU. iOS builds. With receipts.</text>

  <!-- footer dot -->
  <g transform="translate(80, 560)">
    <circle cx="6" cy="6" r="6" fill="#2EC78B"/>
    <text x="22" y="11" font-family="Inter, system-ui, sans-serif" font-size="22" font-weight="600" fill="#FFFFFF">iogrid.org</text>
  </g>
</svg>`;

await mkdir(dirname(outPath), { recursive: true });
const png = await sharp(Buffer.from(svg)).png({ compressionLevel: 9 }).toBuffer();
await writeFile(outPath, png);
console.log(`wrote ${outPath} (${png.length} bytes)`);
