import { DrizzleAdapter } from "@auth/drizzle-adapter";
import NextAuth from "next-auth";
import Nodemailer from "next-auth/providers/nodemailer";

import { authConfig } from "@/auth.config";
import { db } from "@/db/client";
import { accounts, sessions, users, verificationTokens } from "@/db/schema";

/**
 * iogrid admin identity — NextAuth.js v5 (node-only configuration).
 *
 * Mirrors `web/src/lib/auth.ts`. Imported ONLY by Server Components,
 * Server Actions, API routes, and the `/api/auth/[...nextauth]`
 * handler. `middleware.ts` MUST NOT import from here — it pulls
 * `@/auth.config` instead, so nodemailer + pg never reach the edge
 * bundle.
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
