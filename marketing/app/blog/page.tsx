import type { Metadata } from "next";
import Link from "next/link";
import { posts } from "@/content/posts";

export const metadata: Metadata = {
  title: "Blog",
  description:
    "iogrid engineering, market analysis, and quarterly transparency reports. Posts on the mesh-vs-datacenter economics, iOS-build CI pricing, the launch playbook, and the Rust/Go/Next.js stack.",
  alternates: {
    canonical: "/blog",
  },
};

interface BlogIndexProps {
  searchParams?: Promise<{ tag?: string }>;
}

export default async function BlogIndexPage({ searchParams }: BlogIndexProps) {
  const params = (await searchParams) ?? {};
  const activeTag = params.tag?.toLowerCase();

  const allTags = Array.from(
    new Set(posts.flatMap((p) => p.tags.map((t) => t.toLowerCase())))
  ).sort();

  const visiblePosts = activeTag
    ? posts.filter((p) => p.tags.map((t) => t.toLowerCase()).includes(activeTag))
    : posts;

  return (
    <section className="container-page py-16">
      <header className="mx-auto max-w-3xl text-center">
        <h1 className="h-hero text-neutral-900">Blog</h1>
        <p className="mt-4 text-lead">
          Engineering posts, market analysis, and quarterly transparency
          reports. No press releases.
        </p>
      </header>

      <nav
        aria-label="Filter posts by tag"
        className="mx-auto mt-10 flex max-w-4xl flex-wrap justify-center gap-2"
      >
        <Link
          href="/blog"
          className={`rounded-full px-4 py-1.5 text-sm font-semibold transition ${
            !activeTag
              ? "bg-primary-500 text-white"
              : "border border-neutral-200 bg-white text-neutral-700 hover:border-primary-500 hover:text-primary-600"
          }`}
        >
          All ({posts.length})
        </Link>
        {allTags.map((tag) => {
          const count = posts.filter((p) =>
            p.tags.map((t) => t.toLowerCase()).includes(tag)
          ).length;
          const active = activeTag === tag;
          return (
            <Link
              key={tag}
              href={`/blog?tag=${encodeURIComponent(tag)}`}
              className={`rounded-full px-4 py-1.5 text-sm font-semibold transition ${
                active
                  ? "bg-primary-500 text-white"
                  : "border border-neutral-200 bg-white text-neutral-700 hover:border-primary-500 hover:text-primary-600"
              }`}
              aria-current={active ? "page" : undefined}
            >
              {tag} ({count})
            </Link>
          );
        })}
      </nav>

      <div className="mx-auto mt-12 grid max-w-4xl gap-6">
        {visiblePosts.map((post) => (
          <article key={post.slug} className="card">
            <div className="flex flex-wrap items-center gap-2 text-xs text-neutral-500">
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
          <p className="text-center text-sm text-neutral-500">
            No posts in this tag yet. <Link href="/blog" className="underline">See all</Link>.
          </p>
        ) : null}
      </div>

      <aside
        aria-labelledby="newsletter-heading"
        className="mx-auto mt-16 max-w-3xl rounded-2xl border border-neutral-200 bg-neutral-50 p-8 md:p-10"
      >
        <h2
          id="newsletter-heading"
          className="h-section text-center text-neutral-900"
        >
          Subscribe to the newsletter
        </h2>
        <p className="mx-auto mt-3 max-w-xl text-center text-neutral-600">
          New engineering posts and quarterly transparency reports, delivered
          via email when they publish. One message per post. No marketing
          retargeting, no third-party tracking pixel.
        </p>
        <form
          aria-label="Newsletter signup"
          action="/api/newsletter/subscribe"
          method="post"
          className="mx-auto mt-6 flex max-w-md flex-col gap-3 sm:flex-row"
        >
          <label htmlFor="newsletter-email" className="sr-only">
            Email address
          </label>
          <input
            id="newsletter-email"
            type="email"
            name="email"
            required
            placeholder="you@example.com"
            className="flex-1 rounded-md border border-neutral-200 bg-white px-4 py-3 text-sm text-neutral-900 placeholder:text-neutral-400 focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500"
          />
          <button type="submit" className="btn-primary">
            Subscribe
          </button>
        </form>
        <p className="mx-auto mt-3 max-w-xl text-center text-xs text-neutral-500">
          Placeholder during Phase 0 &mdash; submissions are stored locally
          until the broadcast list is wired up to the email service in Phase 1.
        </p>
      </aside>
    </section>
  );
}
