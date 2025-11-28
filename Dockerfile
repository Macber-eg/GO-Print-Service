# Build stage
FROM golang:1.21-alpine AS builder

# Install build dependencies including libwebp for CGO
RUN apk add --no-cache git ca-certificates tzdata gcc musl-dev libwebp-dev

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum* ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application with CGO enabled for WebP support
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-w -s" -o /app/badge-service .

# Final stage
FROM alpine:3.19

# Install runtime dependencies including libwebp
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    fontconfig \
    ttf-dejavu \
    ttf-liberation \
    libwebp \
    && rm -rf /var/cache/apk/*

# Create non-root user
RUN adduser -D -g '' appuser

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/badge-service .

# Copy fonts directory (if exists)
COPY --from=builder /app/fonts ./fonts

# Create cache directory
RUN mkdir -p /tmp/badge-cache && chown -R appuser:appuser /tmp/badge-cache

# Create fonts directory and set permissions
RUN mkdir -p /app/fonts && chown -R appuser:appuser /app/fonts

# Switch to non-root user
USER appuser

# Expose port
EXPOSE 3000

# Set environment variables
ENV PORT=3000
ENV CACHE_DIR=/tmp/badge-cache

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:3000/health || exit 1

# Run the application
CMD ["./badge-service"]
