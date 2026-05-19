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
});
