# Dashboard-only: nginx serving React SPA + reverse proxy to host bot API
FROM nginx:alpine

# Copy React build output
COPY web/ /usr/share/nginx/html/

# Copy nginx configuration
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 8082

HEALTHCHECK --interval=30s --timeout=10s --retries=3 \
    CMD curl -sf http://localhost:8082/ || exit 1
