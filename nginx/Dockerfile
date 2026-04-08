FROM nginx:1.27-alpine

RUN rm /etc/nginx/conf.d/default.conf

COPY nginx.conf /etc/nginx/conf.d/vcs.conf

# Frontend static files — injected by CI (copied from frontend/dist build artefact)
# The html/ directory is populated by the CD pipeline before building this image.
COPY html/ /usr/share/nginx/html/

EXPOSE 80

HEALTHCHECK --interval=10s --timeout=5s --retries=3 \
  CMD wget -qO- http://localhost/nginx-health || exit 1

# Write region.json at startup from REGION env var, then hand off to nginx
CMD ["/bin/sh", "-c", "echo \"{\\\"region\\\":\\\"${REGION:-local}\\\"}\" > /usr/share/nginx/html/region.json && exec nginx -g 'daemon off;'"]
