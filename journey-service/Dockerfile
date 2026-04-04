# Multi-stage Dockerfile for Journey Service

# Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make

# Set working directory
WORKDIR /app

# Copy go mod files and source together so go mod tidy can see all imports
COPY go.mod go.sum ./
COPY . .

# Resolve any missing go.sum entries (e.g. GCS packages not yet in go.sum),
# then download all declared dependencies
RUN go mod tidy && go mod download

# Build the application
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -ldflags="-w -s" -o server ./cmd/server

# Final stage — alpine for wget healthcheck and shell-based secret bridging
FROM alpine:3.20

# Install runtime dependencies: wget (healthcheck), ca-certificates (TLS), tzdata (timezone)
RUN apk add --no-cache wget ca-certificates tzdata && \
    addgroup -S appgroup && adduser -S appuser -G appgroup

# Set working directory as /app
WORKDIR /app

# Copy binary, config, and migrations from builder
COPY --from=builder /app/server ./server
COPY --from=builder /app/config.yaml ./config.yaml
COPY --from=builder /app/migrations ./migrations

# Entrypoint script: bridges Docker secrets to env vars expected by Viper
RUN printf '#!/bin/sh\n\
if [ -f /run/secrets/db_password ]; then\n\
  export VCS_DATABASE_MASTER_PASSWORD="$(cat /run/secrets/db_password)"\n\
fi\n\
if [ -f /run/secrets/jwt_secret ]; then\n\
  export VCS_SERVICES_JWT_SECRET="$(cat /run/secrets/jwt_secret)"\n\
fi\n\
exec ./server "$@"\n' > /app/entrypoint.sh && chmod +x /app/entrypoint.sh

# Expose actual service port
EXPOSE 8083

# Run as non-root user
USER appuser

# Run the application via entrypoint (bridges secrets → env vars)
ENTRYPOINT ["/app/entrypoint.sh"]
