"use client";

import { useState, FormEvent } from "react";

interface Props {
  apiBase: string;
}

type State =
  | { kind: "idle" }
  | { kind: "submitting" }
  | { kind: "ok"; email: string }
  | { kind: "error"; message: string };

export function SubscribeForm({ apiBase }: Props) {
  const [state, setState] = useState<State>({ kind: "idle" });

  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const form = e.currentTarget;
    const email = (form.elements.namedItem("email") as HTMLInputElement).value;
    setState({ kind: "submitting" });
    try {
      const res = await fetch(`${apiBase}/status/subscribe`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email }),
      });
      if (!res.ok) {
        const text = await res.text();
        throw new Error(text || `status ${res.status}`);
      }
      setState({ kind: "ok", email });
      form.reset();
    } catch (err) {
      setState({
        kind: "error",
        message:
          err instanceof Error && err.message
            ? err.message
            : "Subscription failed. Please try again.",
      });
    }
  }

  return (
    <form onSubmit={onSubmit} className="space-y-3" noValidate>
      <label htmlFor="status-subscribe-email" className="sr-only">
        Email address
      </label>
      <input
        id="status-subscribe-email"
        name="email"
        type="email"
        required
        autoComplete="email"
        placeholder="you@company.com"
        className="block w-full rounded-md border border-neutral-200 bg-white px-3 py-2 text-sm shadow-sm focus:border-primary-500 focus:outline-none focus:ring-1 focus:ring-primary-500"
      />
      <button
        type="submit"
        disabled={state.kind === "submitting"}
        className="btn-primary w-full disabled:opacity-60"
      >
        {state.kind === "submitting" ? "Subscribing…" : "Subscribe"}
      </button>
      {state.kind === "ok" ? (
        <p className="text-sm text-success" role="status">
          Subscribed {state.email}. Check your inbox for a verification
          email.
        </p>
      ) : null}
      {state.kind === "error" ? (
        <p className="text-sm text-danger" role="alert">
          {state.message}
        </p>
      ) : null}
    </form>
  );
}
