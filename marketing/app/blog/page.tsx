import type { Metadata } from "next";
import Link from "next/link";
import { posts } from "@/content/posts";

export const metadata: Metadata = {
  title: "Blog",
  description: "iogrid engineering, strategy, and transparency reports.",
};

export default function BlogIndexPage() {
  return (
    <section className="container-page py-16">
      <header className="mx-auto max-w-3xl text-center">
        <h1 className="h-hero text-neutral-900">Blog</h1>
        <p className="mt-4 text-lead">
          Engineering posts, market analysis, and quarterly transparency
          reports. No press releases.
        </p>
      </header>
      <div className="mx-auto mt-12 grid max-w-4xl gap-6">
        {posts.map((post) => (
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
              <Link href={`/blog/${post.slug}`} className="hover:text-primary-600">
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
      </div>
    </section>
  );
}
