# Tiny redirect server for releases.iogrid.org.
#
# Image is pure nginx — no static blobs are baked in. The nginx config
# emits 302 redirects pointing at GitHub Releases asset URLs (see
# ./nginx.conf for the routing table).
#
# Refs #392.

FROM nginx:1.27-alpine

# Drop the default site that nginx ships with, then mount ours.
RUN rm -f /etc/nginx/conf.d/default.conf
COPY releases/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 8080

# /healthz is the canonical probe URL — used by both the Deployment's
# liveness/readiness probes and any external uptime monitor.
HEALTHCHECK --interval=30s --timeout=3s \
    CMD wget -qO- http://localhost:8080/healthz >/dev/null 2>&1 || exit 1
