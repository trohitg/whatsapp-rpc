# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY src/go/ ./src/go/

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -o whatsapp-rpc-server ./src/go/cmd/server

# Runtime stage
FROM alpine:latest

WORKDIR /app

# Install ca-certificates for HTTPS
RUN apk add --no-cache ca-certificates tzdata

# Create data directories
RUN mkdir -p /app/data/qr /app/configs

# Copy binary from builder
COPY --from=builder /app/whatsapp-rpc-server .

# Copy config if exists
COPY configs/config.yaml ./configs/

# Expose WebSocket port
EXPOSE 9400

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:9400/health || exit 1

# Run server
CMD ["./whatsapp-rpc-server"]
