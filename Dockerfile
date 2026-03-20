# Build stage
FROM golang:1.21-alpine AS builder

# Add build argument to specify which proxy to build
# Available variants: deepseek | poe | o2a | o2a-max
ARG PROXY_VARIANT=deepseek

# Install necessary build tools
RUN apk add --no-cache git

# Set working directory
WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source files
COPY . .

# Build the application based on the selected variant
RUN case "$PROXY_VARIANT" in \
        poe)     CGO_ENABLED=0 GOOS=linux go build -tags claude -o proxy proxy-poe.go ;; \
        o2a)     CGO_ENABLED=0 GOOS=linux go build -o proxy proxy-o2a.go ;; \
        o2a-max) CGO_ENABLED=0 GOOS=linux go build -o proxy proxy-o2a-max.go ;; \
        *)       CGO_ENABLED=0 GOOS=linux go build -o proxy proxy.go ;; \
    esac

# Final stage
FROM alpine:latest

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates

# Set working directory
WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/proxy .

# Expose port 9000
EXPOSE 9000

# Run the application
CMD ["./proxy"]
