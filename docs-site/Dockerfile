# Build docs.iogrid.org to a static export and serve via nginx.
# Multi-stage to keep the runtime image small (~20 MB).

FROM node:22-alpine AS builder
WORKDIR /app

RUN corepack enable && corepack prepare pnpm@9.15.0 --activate

# Brand tokens and OpenAPI spec are sibling directories — copy both so the
# astro.config.mjs can read them at build time.
COPY brand           ./brand
COPY proto           ./proto
COPY docs-site/package.json docs-site/pnpm-lock.yaml* ./docs-site/

WORKDIR /app/docs-site
RUN pnpm install --frozen-lockfile || pnpm install

COPY docs-site/ ./
ENV NODE_ENV=production
ENV ASTRO_TELEMETRY_DISABLED=1
RUN node scripts/fetch-openapi.mjs
RUN node scripts/gen-og.mjs
RUN pnpm build
RUN node scripts/link-check.mjs

# Runtime — pure nginx serving static files
FROM nginx:1.27-alpine AS runtime
COPY --from=builder /app/docs-site/dist /usr/share/nginx/html
COPY docs-site/nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s \
  CMD wget -qO- http://localhost:8080/ >/dev/null 2>&1 || exit 1
