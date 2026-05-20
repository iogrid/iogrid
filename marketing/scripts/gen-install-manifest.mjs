// Emits the static install-manifest assets the marketing site serves
// from `iogrid.org/install/`:
//
//   - public/install/manifest.json — the same artefact catalogue the
//     React UI consumes, exposed as a CORS-friendly JSON endpoint for
//     external integrations (eg the daemon-self-update follow-up at
//     issue #348).
//   - public/install/<artifact-id>.sha256 — one file per artefact for
//     the documented `curl … .sha256 | shasum -a 256 -c -` flow.
//   - public/install/sh — convenience curl-pipe-sh installer wrapper.
//
// Run as the `prebuild` step in package.json so a fresh `next build`
// always picks up the current manifest content. The source of truth
// is `content/installer-manifest.json`; this script keeps the two
// callers (React UI + static endpoints) honest by hashing the JSON
// once at the start of build.

import { readFile, writeFile, mkdir, rm } from "node:fs/promises";
import { existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const marketingRoot = join(__dirname, "..");
const sourceFile = join(marketingRoot, "content", "installer-manifest.json");
const outDir = join(marketingRoot, "public", "install");

async function main() {
  const raw = JSON.parse(await readFile(sourceFile, "utf8"));
  const { version, channel, cdn, artifacts } = raw;
  if (!Array.isArray(artifacts) || !version || !cdn) {
    throw new Error(
      "gen-install-manifest: malformed installer-manifest.json (expected version + cdn + artifacts[])",
    );
  }

  if (existsSync(outDir)) await rm(outDir, { recursive: true, force: true });
  await mkdir(outDir, { recursive: true });

  // manifest.json — keys in the same shape as
  // installer/auto-update/manifest.schema.json so a follow-up service
  // can swap it without breaking external consumers.
  const manifest = {
    version: 1,
    channel: channel ?? "stable",
    issued_at: new Date().toISOString(),
    release: {
      version,
      artifacts: artifacts.map((a) => ({
        id: a.id,
        platform: a.platform,
        arch: a.arch,
        linux_format: a.linuxFormat ?? null,
        target: a.target,
        url: `${cdn}/${version}/${a.filename}`,
        sha256: a.sha256,
        size_bytes: a.sizeBytes,
        label: a.label,
      })),
    },
  };
  await writeFile(
    join(outDir, "manifest.json"),
    JSON.stringify(manifest, null, 2) + "\n",
    "utf8",
  );

  // Per-artefact .sha256 files. Format matches `shasum -a 256` output
  // (lowercase hex + two spaces + filename) so a user can compare via:
  //   curl … .sha256 | shasum -a 256 -c -
  for (const a of artifacts) {
    const body = `${a.sha256}  ${a.filename}\n`;
    await writeFile(join(outDir, `${a.id}.sha256`), body, "utf8");
  }

  // Convenience text endpoint advertised in the install-page docs:
  //   curl -fsSL https://iogrid.org/install/sh | sh
  await writeFile(
    join(outDir, "sh"),
    "#!/bin/sh\n" +
      "exec curl -fsSL https://raw.githubusercontent.com/iogrid/iogrid/main/installer/install.sh | sh -\n",
    "utf8",
  );

  console.log(
    `gen-install-manifest: wrote manifest.json + ${artifacts.length} .sha256 files + sh wrapper to ${outDir}`,
  );
}

main().catch((err) => {
  console.error("gen-install-manifest failed:", err);
  process.exit(1);
});
