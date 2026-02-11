# Multi-stage build for Alice AI Agent
# Stage 1: Build stage
FROM golang:1.24-alpine AS builder

# Install dependencies for building
RUN apk add --no-cache git ca-certificates tzdata

# Create app directory
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code and generated proto code
COPY cmd/ cmd/
COPY internal/ internal/
COPY gen/ gen/

# Build the application with optimizations
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags='-w -s -extldflags "-static"' \
    -a -installsuffix cgo \
    -o alice ./cmd/alice

# Stage 2: Runtime stage
FROM alpine:3.20

# Install runtime dependencies
RUN apk --no-cache add \
    ca-certificates \
    tzdata \
    curl \
    git \
    && addgroup -g 1001 alice \
    && adduser -D -s /bin/sh -u 1001 -G alice alice

# Set timezone (can be overridden with TZ env var)
ENV TZ=Asia/Taipei

# Create necessary directories
RUN mkdir -p /app/data /app/web /app/project \
    && chown -R alice:alice /app

# Copy binary from builder stage
COPY --from=builder /app/alice /app/alice

# Copy web static files
COPY --chown=alice:alice web/ /app/web/

# Create health check script
RUN echo '#!/bin/sh' > /app/healthcheck.sh && \
    echo 'curl -sf http://localhost:${WEB_PORT:-8082}/api/health || exit 1' >> /app/healthcheck.sh && \
    chmod +x /app/healthcheck.sh

# Switch to non-root user
USER alice

# Set working directory
WORKDIR /app

# Expose web interface port
EXPOSE 8082

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
    CMD /app/healthcheck.sh

# Volume for persistent data
VOLUME ["/app/data", "/app/project"]

# Default command
CMD ["./alice"]
