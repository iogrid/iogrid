#!/usr/bin/env node
// fetch-openapi.mjs — copy the latest generated OpenAPI spec from
// ../proto/gen/openapi/iogrid.yaml into public/ so it's served at
// docs.iogrid.org/openapi.yaml.
//
// If the spec doesn't exist yet (SDK pipeline hasn't shipped, fresh clone),
// emit a tiny stub spec so the API reference page still renders something
// instead of breaking the build.

import { existsSync, mkdirSync, copyFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(__dirname, "..");
const SRC = path.resolve(ROOT, "..", "proto", "gen", "openapi", "iogrid.yaml");
const PUB = path.join(ROOT, "public");
const DEST = path.join(PUB, "openapi.yaml");

mkdirSync(PUB, { recursive: true });

if (existsSync(SRC)) {
  copyFileSync(SRC, DEST);
  console.log(`[openapi] copied ${SRC} -> ${DEST}`);
} else {
  // Emit a minimal stub so consumers and our own renderer don't choke.
  const stub = `openapi: 3.1.0
info:
  title: iogrid API
  version: 0.0.0-stub
  description: |
    This is a placeholder OpenAPI spec.

    The real spec is generated from protobuf at proto/gen/openapi/iogrid.yaml
    by the SDK pipeline. When that ships, this stub is overwritten on
    every docs-site build via scripts/fetch-openapi.mjs.
servers:
  - url: https://api.iogrid.org/v1
paths:
  /me:
    get:
      summary: Get the current account
      operationId: getMe
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                properties:
                  account_id: { type: string }
                  email:      { type: string }
                  plan:       { type: string }
`;
  writeFileSync(DEST, stub);
  console.log(`[openapi] no spec at ${SRC}; wrote stub to ${DEST}`);
}
