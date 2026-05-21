import type { NextAuthConfig } from "next-auth";
import Google from "next-auth/providers/google";

/**
 * Edge-safe NextAuth configuration for the admin app.
 *
 * Imported by `src/middleware.ts` (edge runtime) — must NOT transitively
 * pull `nodemailer` / `pg` / Drizzle adapter, or module-eval throws
 *   `TypeError: Cannot redefine property: __import_unsupported`
 * on the first protected-route navigation. Node-only providers live in
 * `src/lib/auth.ts` instead.
 *
 * Mirrors `web/src/auth.config.ts` so a single sign-in cookie issued by
 * the web app is recognised here (same AUTH_SECRET, same callbacks).
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
    signIn: "/signin",
  },
  callbacks: {
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
