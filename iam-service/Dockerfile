# Multi-stage Dockerfile for IAM Service

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

# Final stage using distroless
FROM gcr.io/distroless/static:nonroot

# Copy binary from builder
COPY --from=builder /app/server /app/server

# Copy config file
COPY --from=builder /app/config.yaml /app/config.yaml

# Set working directory as /app
WORKDIR /app

# Expose port
EXPOSE 8080

# Run as non-root user
USER nonroot:nonroot

# Run the application
ENTRYPOINT ["./server"]