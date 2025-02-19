FROM golang:1.24.0 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=linux go build -o kv-server

# Final stage
FROM golang:1.24.0-alpine

WORKDIR /app

COPY --from=builder /app/kv-server .
COPY --from=builder /app/static ./static

EXPOSE 8080

CMD ["./kv-server"]
