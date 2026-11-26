# Dashboard-only: nginx serving React SPA + reverse proxy to host bot API
FROM nginx:alpine

# Copy React build output
COPY web/ /usr/share/nginx/html/

# nginx.conf is a template: the image's entrypoint fills in WEB_API_TOKEN and
# writes the result to /etc/nginx/conf.d/default.conf. Only that one variable
# is substituted, so nginx's own $variables are left alone.
ENV WEB_API_TOKEN="" \
    NGINX_ENVSUBST_FILTER="^WEB_API_TOKEN$"
COPY nginx.conf /etc/nginx/templates/default.conf.template

EXPOSE 8082

HEALTHCHECK --interval=30s --timeout=10s --retries=3 \
    CMD curl -sf http://localhost:8082/ || exit 1
