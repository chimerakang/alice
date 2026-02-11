# Multi-stage build for Claude TG Agent
# Stage 1: Build stage
FROM golang:1.21-alpine AS builder

# Install dependencies for building
RUN apk add --no-cache git ca-certificates tzdata

# Create app directory
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY *.go ./

# Build the application with optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags='-w -s -extldflags "-static"' \
    -a -installsuffix cgo \
    -o alice .

# Stage 2: Runtime stage
FROM alpine:3.18

# Install runtime dependencies
RUN apk --no-cache add \
    ca-certificates \
    tzdata \
    curl \
    jq \
    && addgroup -g 1001 alice \
    && adduser -D -s /bin/sh -u 1001 -G alice alice

# Set timezone (can be overridden with TZ env var)
ENV TZ=UTC

# Create necessary directories
RUN mkdir -p /app/data /app/config /app/logs \
    && chown -R alice:alice /app

# Copy binary from builder stage
COPY --from=builder /app/alice /app/alice

# Copy example configuration
COPY config.example.json /app/config/config.example.json

# Create health check script
RUN echo '#!/bin/sh' > /app/healthcheck.sh && \
    echo 'if [ "$ENABLE_WEB_INTERFACE" = "true" ]; then' >> /app/healthcheck.sh && \
    echo '  curl -f http://localhost:${WEB_PORT:-8080}/api/health || exit 1' >> /app/healthcheck.sh && \
    echo 'else' >> /app/healthcheck.sh && \
    echo '  # Check if process is running' >> /app/healthcheck.sh && \
    echo '  pgrep -f alice > /dev/null || exit 1' >> /app/healthcheck.sh && \
    echo 'fi' >> /app/healthcheck.sh && \
    chmod +x /app/healthcheck.sh

# Switch to non-root user
USER alice

# Set working directory
WORKDIR /app

# Expose web interface port (can be overridden)
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=30s --retries=3 \
    CMD /app/healthcheck.sh

# Volume for persistent data
VOLUME ["/app/data", "/app/config", "/app/logs"]

# Environment variables with defaults
ENV CLAUDE_MODEL=sonnet \
    PROJECT_DIR=/app/data \
    WEB_PORT=8080 \
    ENABLE_WEB_INTERFACE=true \
    ENABLE_PERFORMANCE_MONITORING=true \
    ENABLE_RATE_LIMITING=true \
    ENABLE_PII_DETECTION=true \
    ENABLE_AUDIT_LOGGING=true \
    DATA_RETENTION_DAYS=30 \
    RATE_LIMIT_RPM=60

# Default command
CMD ["./alice"]