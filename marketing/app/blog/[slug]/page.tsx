import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { posts, getPost } from "@/content/posts";

interface Params {
  slug: string;
}

export function generateStaticParams(): Params[] {
  return posts.map((p) => ({ slug: p.slug }));
}

export async function generateMetadata({
  params,
}: {
  params: Promise<Params>;
}): Promise<Metadata> {
  const { slug } = await params;
  const post = getPost(slug);
  if (!post) return { title: "Not found" };
  return {
    title: post.title,
    description: post.description,
  };
}

export default async function BlogPostPage({
  params,
}: {
  params: Promise<Params>;
}) {
  const { slug } = await params;
  const post = getPost(slug);
  if (!post) notFound();

  return (
    <article className="container-page py-16">
      <header className="mx-auto max-w-3xl">
        <div className="flex flex-wrap items-center gap-2 text-xs text-neutral-500">
          <time dateTime={post.date} className="font-tabular">
            {post.date}
          </time>
          <span>&middot; {post.author}</span>
          {post.tags.map((t) => (
            <span key={t} className="pill">
              {t}
            </span>
          ))}
        </div>
        <h1 className="mt-4 text-4xl font-extrabold tracking-tight text-neutral-900 md:text-5xl">
          {post.title}
        </h1>
        <p className="mt-4 text-lead">{post.description}</p>
      </header>
      <div className="mx-auto mt-12 max-w-3xl whitespace-pre-wrap font-sans text-base leading-7 text-neutral-700">
        {post.body}
      </div>
      <div className="mx-auto mt-12 max-w-3xl border-t border-neutral-200 pt-6">
        <Link href="/blog" className="text-sm font-semibold text-primary-600 hover:underline">
          &larr; All posts
        </Link>
      </div>
    </article>
  );
}
