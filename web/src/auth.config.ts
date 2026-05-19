import type { NextAuthConfig } from "next-auth";
import Google from "next-auth/providers/google";

/**
 * Edge-safe NextAuth configuration.
 *
 * This module is imported by `src/middleware.ts`, which Next.js compiles
 * for the **edge runtime**. The edge runtime does not implement Node's
 * `stream` / `setImmediate` APIs, so any transitive pull of `nodemailer`
 * (or anything that pulls `stream`) blows up at module-eval time with
 *   `TypeError: Cannot redefine property: __import_unsupported`
 *
 * Rule of thumb for this file:
 *   - Provider DEFINITIONS that have no node-only side effects (Google,
 *     GitHub, credential providers that don't hit a DB) are fine here.
 *   - The Nodemailer / SMTP provider, the Prisma/Drizzle adapter, and
 *     any other node-only deps live in `src/lib/auth.ts` instead — that
 *     module is only ever evaluated by Server Components, Server Actions,
 *     API routes, and the `/api/auth/[...nextauth]` handler, all of which
 *     run in the Node.js runtime.
 *
 * See: https://authjs.dev/guides/edge-compatibility
 */
export const authConfig = {
  providers: [
    Google({
      clientId: process.env.GOOGLE_CLIENT_ID,
      clientSecret: process.env.GOOGLE_CLIENT_SECRET,
    }),
  ],
  session: { strategy: "jwt" },
  pages: {
    signIn: "/account",
  },
  callbacks: {
    /**
     * Lightweight authorization check used by the middleware. We return
     * `true` if there's a session, otherwise `false` — the middleware
     * itself does the redirect-with-callbackUrl dance because it needs
     * access to the request's pathname.
     */
    authorized({ auth }) {
      return !!auth?.user;
    },
    async jwt({ token, user }) {
      if (user) {
        token.uid = user.id;
      }
      return token;
    },
    async session({ session, token }) {
      if (token.uid && session.user) {
        (session.user as { id?: string }).id = token.uid as string;
      }
      return session;
    },
  },
} satisfies NextAuthConfig;
