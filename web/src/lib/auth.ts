import { DrizzleAdapter } from "@auth/drizzle-adapter";
import NextAuth from "next-auth";
import Nodemailer from "next-auth/providers/nodemailer";

import { authConfig } from "@/auth.config";
import { db } from "@/db/client";
import { accounts, sessions, users, verificationTokens } from "@/db/schema";

/**
 * iogrid identity — NextAuth.js v5 (node-only configuration).
 *
 * This module extends the edge-safe `authConfig` with providers + the
 * Postgres-backed Drizzle adapter required by NextAuth's EmailProvider
 * (it stores one-time verification tokens in the DB until the user
 * clicks the magic-link).
 *
 * Server Components, Server Actions, API routes, and the
 * `/api/auth/[...nextauth]` handler import from THIS module. The
 * `middleware.ts` file MUST NOT import from here — it imports from
 * `@/auth.config` instead, so that nodemailer + pg are never pulled
 * into the edge bundle.
 *
 * See `src/auth.config.ts` for the architecture rationale.
 */
export const { handlers, signIn, signOut, auth } = NextAuth({
  ...authConfig,
  adapter: db
    ? DrizzleAdapter(db, {
        usersTable: users,
        accountsTable: accounts,
        sessionsTable: sessions,
        verificationTokensTable: verificationTokens,
      })
    : undefined,
  providers: [
    ...authConfig.providers,
    Nodemailer({
      server: {
        host: process.env.EMAIL_SERVER_HOST,
        port: Number(process.env.EMAIL_SERVER_PORT ?? 587),
        auth: {
          user: process.env.EMAIL_SERVER_USER,
          pass: process.env.EMAIL_SERVER_PASSWORD,
        },
      },
      from: process.env.EMAIL_FROM,
    }),
  ],
  events: {
    /**
     * #685: NextAuth authenticates OUTSIDE identity-svc (this nodemailer
     * magic-link never touches AuthService.CompleteMagicLink), so without
     * this hook a signed-in user has ZERO identifier rows — the
     * /account/identifiers page told them "No identifiers bound" and the
     * keep-one-verified-identifier safety had nothing to protect.
     * EnsureIdentifier is idempotent, so every sign-in re-asserts the row
     * and existing accounts heal on their next login. Best-effort: a
     * registry failure must never block the sign-in itself.
     */
    async signIn({ user, account }) {
      const base = process.env.IOGRID_GATEWAY_BFF_URL;
      const serviceToken = process.env.IOGRID_SERVICE_TOKEN ?? "";
      if (!base || !serviceToken || !user?.id || !user?.email) return;
      const isGoogle = account?.provider === "google";
      try {
        await fetch(`${base.replace(/\/+$/, "")}/api/v1/me/identifiers`, {
          method: "POST",
          headers: {
            authorization: `Bearer ${serviceToken}`,
            "x-iogrid-user-id": user.id,
            "x-iogrid-user-email": user.email,
            "content-type": "application/json",
          },
          body: JSON.stringify({
            kind: isGoogle
              ? "IDENTIFIER_KIND_GOOGLE"
              : "IDENTIFIER_KIND_MAGIC_LINK",
            verified_email: user.email,
            subject: isGoogle ? (account?.providerAccountId ?? "") : "",
          }),
          signal: AbortSignal.timeout(4000),
        });
      } catch {
        // Swallow: next sign-in retries; the identifiers page degrades to
        // its (now-honest) empty state rather than blocking auth.
      }
    },
  },
});
