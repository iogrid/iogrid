import type { NextAuthConfig } from "next-auth";
import Google from "next-auth/providers/google";

/**
 * Edge-safe NextAuth configuration for the admin/ app.
 *
 * Imported by `src/middleware.ts` (edge runtime). The edge runtime
 * does not implement Node's `stream` / `setImmediate` APIs, so any
 * transitive pull of `nodemailer` (or anything that pulls `stream`)
 * blows up at module-eval time. Provider DEFINITIONS that have no
 * node-only side effects (Google) are fine; the Nodemailer provider
 * + Drizzle adapter live in `src/lib/auth.ts`.
 *
 * Cookie scope (EPIC #422 Phase 1)
 * --------------------------------
 * The session cookie is host-scoped to `admin.iogrid.org` (NOT
 * `.iogrid.org`). This is the founder's strict-separation invariant:
 * an admin session must NEVER be sent to the user-facing app on
 * `iogrid.org` / `app.iogrid.org` — different surfaces, different
 * sessions, different RBAC, different blast radius.
 *
 * Default NextAuth cookie names get an `__Secure-` prefix in
 * production. We pin the name explicitly so the cookie is
 * unmistakably the admin one in browser DevTools + access logs.
 */

const adminHost = (process.env.IOGRID_ADMIN_HOST ?? "admin.iogrid.org").toLowerCase();
const isProduction = process.env.NODE_ENV === "production";
// localhost / 127.0.0.1 / *.svc.cluster.local don't get the cookie
// domain pin — pinning to a non-current host stops the browser from
// ever sending the cookie back.
const useDomain = isProduction && adminHost.includes(".") && !adminHost.endsWith(".local");

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
  cookies: {
    sessionToken: {
      name: isProduction
        ? "__Secure-iogrid-admin.session-token"
        : "iogrid-admin.session-token",
      options: {
        httpOnly: true,
        sameSite: "lax",
        path: "/",
        secure: isProduction,
        // Pin to admin host so the cookie can NEVER be sent to
        // iogrid.org / app.iogrid.org. Leaving `domain` undefined
        // makes the cookie host-only (browser default) which is
        // equivalent for security, but pinning is explicit and
        // shows up clearly in browser DevTools.
        ...(useDomain ? { domain: adminHost } : {}),
      },
    },
    callbackUrl: {
      name: isProduction
        ? "__Secure-iogrid-admin.callback-url"
        : "iogrid-admin.callback-url",
      options: {
        httpOnly: true,
        sameSite: "lax",
        path: "/",
        secure: isProduction,
      },
    },
    csrfToken: {
      // Intentionally NOT `__Host-` prefixed (that would forbid a
      // domain attribute) — host-only is fine for CSRF.
      name: isProduction
        ? "__Secure-iogrid-admin.csrf-token"
        : "iogrid-admin.csrf-token",
      options: {
        httpOnly: true,
        sameSite: "lax",
        path: "/",
        secure: isProduction,
      },
    },
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
