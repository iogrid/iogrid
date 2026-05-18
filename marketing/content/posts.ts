// Build-time blog post registry. Reads the MDX files in this directory and
// parses front matter via regex (no extra dependency). Body is rendered as
// preformatted text — full MDX renderer can be wired in Phase 2 with
// @next/mdx without changing the registry shape.

import fs from "node:fs";
import path from "node:path";

export interface Post {
  slug: string;
  title: string;
  description: string;
  date: string;
  author: string;
  tags: string[];
  body: string;
}

const FRONT_MATTER_RE = /^---\n([\s\S]*?)\n---\n([\s\S]*)$/;

function parseFrontMatter(raw: string): { meta: Record<string, string>; body: string } {
  const match = raw.match(FRONT_MATTER_RE);
  if (!match) return { meta: {}, body: raw };
  const meta: Record<string, string> = {};
  for (const line of match[1].split("\n")) {
    const m = line.match(/^([a-zA-Z_]+):\s*(.*)$/);
    if (!m) continue;
    let value = m[2].trim();
    if (value.startsWith('"') && value.endsWith('"')) {
      value = value.slice(1, -1);
    } else if (value.startsWith("[") && value.endsWith("]")) {
      // Array literal, simple parse
    }
    meta[m[1]] = value;
  }
  return { meta, body: match[2] };
}

function parseTags(raw: string | undefined): string[] {
  if (!raw) return [];
  return raw
    .replace(/^\[|\]$/g, "")
    .split(",")
    .map((s) => s.trim().replace(/^"|"$/g, ""))
    .filter(Boolean);
}

function loadPosts(): Post[] {
  const dir = path.join(process.cwd(), "content", "posts");
  if (!fs.existsSync(dir)) return [];
  const files = fs.readdirSync(dir).filter((f) => f.endsWith(".mdx"));
  return files
    .map((file): Post => {
      const slug = file.replace(/\.mdx$/, "");
      const raw = fs.readFileSync(path.join(dir, file), "utf8");
      const { meta, body } = parseFrontMatter(raw);
      return {
        slug,
        title: meta.title ?? slug,
        description: meta.description ?? "",
        date: meta.date ?? "",
        author: meta.author ?? "iogrid team",
        tags: parseTags(meta.tags),
        body,
      };
    })
    .sort((a, b) => (a.date < b.date ? 1 : -1));
}

export const posts: Post[] = loadPosts();

export function getPost(slug: string): Post | undefined {
  return posts.find((p) => p.slug === slug);
}
