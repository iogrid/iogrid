#!/usr/bin/env node
/**
 * Capture before/after screenshots of the iogrid landing page for the
 * Phase 2.1 design-system PR. Boots Playwright Chromium against an
 * already-running `next start` on http://localhost:3000.
 *
 * Usage:
 *   node scripts/capture-landing.mjs <out-dir> <label>
 *
 * Captures (per label):
 *   <label>-light-desktop.png    (1440x900, light theme)
 *   <label>-dark-desktop.png     (1440x900, dark theme)
 *   <label>-mobile.png           (390x844, light theme — iPhone 14 width)
 */
import { chromium } from "@playwright/test";
import path from "node:path";
import fs from "node:fs";

const [, , outDirArg, labelArg] = process.argv;
if (!outDirArg || !labelArg) {
  console.error("usage: capture-landing.mjs <out-dir> <label>");
  process.exit(2);
}
const outDir = path.resolve(outDirArg);
fs.mkdirSync(outDir, { recursive: true });

const BASE = process.env.PLAYWRIGHT_BASE_URL ?? "http://localhost:3000";

async function shot(page, name) {
  const file = path.join(outDir, `${labelArg}-${name}.png`);
  await page.screenshot({ path: file, fullPage: true });
  console.log("captured", file);
}

const browser = await chromium.launch();
try {
  // Desktop light
  {
    const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, colorScheme: "light" });
    const page = await ctx.newPage();
    await page.goto(BASE + "/", { waitUntil: "networkidle" });
    // Force light by clearing localStorage and reloading.
    await page.evaluate(() => {
      try { localStorage.removeItem("theme"); } catch {}
      document.documentElement.classList.remove("dark");
    });
    await page.waitForTimeout(300);
    await shot(page, "light-desktop");
    await ctx.close();
  }
  // Desktop dark
  {
    const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, colorScheme: "dark" });
    const page = await ctx.newPage();
    await page.goto(BASE + "/", { waitUntil: "networkidle" });
    await page.evaluate(() => {
      document.documentElement.classList.add("dark");
    });
    await page.waitForTimeout(300);
    await shot(page, "dark-desktop");
    await ctx.close();
  }
  // Mobile light
  {
    const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, colorScheme: "light" });
    const page = await ctx.newPage();
    await page.goto(BASE + "/", { waitUntil: "networkidle" });
    await shot(page, "mobile");
    await ctx.close();
  }
} finally {
  await browser.close();
}
