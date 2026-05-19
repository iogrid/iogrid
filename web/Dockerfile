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
# NEXT_PUBLIC_* are inlined into the client bundle at `next build` time;
# runtime env injection via the k8s Deployment is too late for the JS
# served to browsers. Default to the production gateway so `pnpm build`
# in CI ships a bundle that talks to api.iogrid.org out of the box.
ARG NEXT_PUBLIC_API_BASE_URL=https://api.iogrid.org
ENV NEXT_PUBLIC_API_BASE_URL=${NEXT_PUBLIC_API_BASE_URL}
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
