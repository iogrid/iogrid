"use client";

import { useMemo, useState } from "react";
import Link from "next/link";

interface PostSummary {
  slug: string;
  title: string;
  description: string;
  date: string;
  tags: string[];
}

export function BlogList({ posts }: { posts: PostSummary[] }) {
  const allTags = useMemo(() => {
    const set = new Set<string>();
    for (const p of posts) {
      for (const t of p.tags) set.add(t.toLowerCase());
    }
    return Array.from(set).sort();
  }, [posts]);

  const [activeTag, setActiveTag] = useState<string | null>(null);

  const visiblePosts = useMemo(() => {
    if (!activeTag) return posts;
    return posts.filter((p) =>
      p.tags.map((t) => t.toLowerCase()).includes(activeTag),
    );
  }, [posts, activeTag]);

  return (
    <>
      <nav
        aria-label="Filter posts by tag"
        className="mx-auto mt-10 flex max-w-4xl flex-wrap justify-center gap-2"
      >
        <button
          type="button"
          onClick={() => setActiveTag(null)}
          className={`rounded-full px-4 py-1.5 text-sm font-semibold transition ${
            !activeTag
              ? "bg-primary-500 text-white"
              : "border border-neutral-200 bg-white text-neutral-700 hover:border-primary-500 hover:text-primary-600"
          }`}
          aria-pressed={!activeTag}
        >
          All ({posts.length})
        </button>
        {allTags.map((tag) => {
          const count = posts.filter((p) =>
            p.tags.map((t) => t.toLowerCase()).includes(tag),
          ).length;
          const active = activeTag === tag;
          return (
            <button
              key={tag}
              type="button"
              onClick={() => setActiveTag(active ? null : tag)}
              className={`rounded-full px-4 py-1.5 text-sm font-semibold transition ${
                active
                  ? "bg-primary-500 text-white"
                  : "border border-neutral-200 bg-white text-neutral-700 hover:border-primary-500 hover:text-primary-600"
              }`}
              aria-pressed={active}
            >
              {tag} ({count})
            </button>
          );
        })}
      </nav>

      <div className="mx-auto mt-12 grid max-w-4xl gap-6">
        {visiblePosts.map((post) => (
          <article key={post.slug} className="card">
            <div className="flex flex-wrap items-center gap-2 text-xs text-neutral-600">
              <time dateTime={post.date} className="font-tabular">
                {post.date}
              </time>
              {post.tags.map((t) => (
                <span key={t} className="pill">
                  {t}
                </span>
              ))}
            </div>
            <h2 className="h-card mt-3 text-neutral-900">
              <Link
                href={`/blog/${post.slug}`}
                className="hover:text-primary-600"
              >
                {post.title}
              </Link>
            </h2>
            <p className="mt-2 text-sm text-neutral-600">{post.description}</p>
            <Link
              href={`/blog/${post.slug}`}
              className="mt-4 inline-block text-sm font-semibold text-primary-600 hover:underline"
            >
              Read post &rarr;
            </Link>
          </article>
        ))}
        {visiblePosts.length === 0 ? (
          <p className="text-center text-sm text-neutral-600">
            No posts in this tag yet.{" "}
            <button
              type="button"
              onClick={() => setActiveTag(null)}
              className="underline"
            >
              See all
            </button>
            .
          </p>
        ) : null}
      </div>
    </>
  );
}
