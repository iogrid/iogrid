import type { Metadata } from "next";
import { posts } from "@/content/posts";
import { BlogList } from "@/components/BlogList";

export const metadata: Metadata = {
  title: "Blog",
  description:
    "iogrid engineering, market analysis, and quarterly transparency reports. Posts on the mesh-vs-datacenter economics, iOS-build CI pricing, the launch playbook, and the Rust/Go/Next.js stack.",
  alternates: {
    canonical: "/blog",
  },
};

export default function BlogIndexPage() {
  const summaries = posts.map((p) => ({
    slug: p.slug,
    title: p.title,
    description: p.description,
    date: p.date,
    tags: p.tags,
  }));

  return (
    <section className="container-page py-16">
      <header className="mx-auto max-w-3xl text-center">
        <h1 className="h-hero text-neutral-900">Blog</h1>
        <p className="mt-4 text-lead">
          Engineering posts, market analysis, and quarterly transparency
          reports. No press releases.
        </p>
      </header>

      <BlogList posts={summaries} />

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
