FROM golang:1.24.0-alpine AS builder

# Add build argument with default value
ARG GIN_MODE=release
ENV GIN_MODE=${GIN_MODE}

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache gcc musl-dev

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-w -s" -o kv-server

# Final stage
FROM alpine:3.18

WORKDIR /app

# Add SQLite and other required runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    sqlite \
    sqlite-libs \
    && adduser -D -H -h /app appuser \
    && mkdir -p /app/data \
    && chown -R appuser:appuser /app

COPY --from=builder /app/kv-server .
COPY --from=builder /app/static ./static

USER appuser

# Set GIN_MODE from build arg
ARG GIN_MODE
ENV GIN_MODE=${GIN_MODE}

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

CMD ["./kv-server"]
