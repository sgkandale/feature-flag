ARG GO_VERSION=1.25.0
FROM golang:${GO_VERSION}-bookworm as builder

WORKDIR /usr/src/app

# Download dependencies
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source code
COPY . .

# Build the server binary targeting cmd/server/main.go
RUN go build -v -o /run-app ./cmd/server


FROM debian:bookworm-slim

# Install ca-certificates (required for verify-full CockroachDB SSL connections)
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

COPY --from=builder /run-app /usr/local/bin/

EXPOSE 8080

CMD ["run-app"]
