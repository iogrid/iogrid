# Build the marketing site to a static export and serve via nginx.
# Multi-stage to keep the runtime image small (~20 MB).

FROM node:22-alpine AS builder
WORKDIR /app

# Install pnpm
RUN corepack enable && corepack prepare pnpm@9.15.0 --activate

# Brand tokens are imported by tailwind.config.ts via ../brand
# so we copy both the marketing/ source AND ../brand into the image.
COPY brand ./brand
COPY marketing/package.json marketing/pnpm-lock.yaml* ./marketing/
WORKDIR /app/marketing
RUN pnpm install --frozen-lockfile || pnpm install

COPY marketing/ ./
ENV NEXT_TELEMETRY_DISABLED=1
RUN pnpm build

# Runtime stage — pure nginx serving static files
FROM nginx:1.27-alpine AS runtime
COPY --from=builder /app/marketing/out /usr/share/nginx/html
COPY marketing/nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s CMD wget -qO- http://localhost:8080/ >/dev/null 2>&1 || exit 1
