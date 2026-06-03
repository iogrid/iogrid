# syntax=docker/dockerfile:1.7
# ---------- deps ----------
FROM node:22-alpine AS deps
RUN corepack enable && corepack prepare pnpm@9.15.0 --activate
WORKDIR /app
COPY package.json pnpm-lock.yaml* ./
RUN pnpm install --frozen-lockfile || pnpm install --no-frozen-lockfile

# ---------- build ----------
FROM node:22-alpine AS build
RUN corepack enable && corepack prepare pnpm@9.15.0 --activate
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
ENV NEXT_TELEMETRY_DISABLED=1
# Apple Developer Team ID for the AASA (Universal Links / Ping Direction-B,
# #629). NEXT_PUBLIC_* must be present at build time — next bakes it into the
# AASA route's APP_ID. Defaults to PLACEHOLDER_TEAMID when unset (dev builds);
# web-ci passes the real value from the APPLE_TEAM_ID secret as a build-arg.
ARG NEXT_PUBLIC_APPLE_TEAM_ID=PLACEHOLDER_TEAMID
ENV NEXT_PUBLIC_APPLE_TEAM_ID=$NEXT_PUBLIC_APPLE_TEAM_ID
RUN pnpm build

# ---------- runtime ----------
FROM node:22-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
ENV PORT=3000

RUN addgroup -S iogrid && adduser -S iogrid -G iogrid

COPY --from=build /app/public ./public
COPY --from=build /app/.next/standalone ./
COPY --from=build /app/.next/static ./.next/static

USER iogrid
EXPOSE 3000
CMD ["node", "server.js"]
