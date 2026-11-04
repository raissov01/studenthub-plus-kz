# syntax=docker/dockerfile:1.7
# Student Hub Go API. Build context: backend/
#   docker build -f docker/backend.Dockerfile -t studenthub-api:<sha> backend/
# One static binary for the API, one for migrations, one for the opportunity feed collector, the SQL migrations and the bundled content
# (data/*.json, the API's fallback when Google Sheets are down). Runs as a non-root user.
FROM golang:1.25-alpine AS builder
WORKDIR /src
ENV CGO_ENABLED=0 GOOS=linux GOFLAGS=-trimpath
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "-s -w" -o /out/server ./cmd/server && \
    go build -ldflags "-s -w" -o /out/migrate ./cmd/migrate && \
    go build -ldflags "-s -w" -o /out/feeds-collect ./cmd/feeds-collect

FROM alpine:3.20
# ca-certificates: Telegram, OpenAI, Resend, Google Sheets over https; tzdata: Asia/Almaty reminders;
# busybox wget (already in alpine) serves the healthcheck
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S -g 10001 studenthub && adduser -S -D -H -u 10001 -G studenthub studenthub && \
    mkdir -p /app/uploads/market /app/uploads/quiz && chown -R studenthub:studenthub /app/uploads
WORKDIR /app
COPY --from=builder /out/server /out/migrate /out/feeds-collect ./
COPY migrations ./migrations
COPY data ./data
# opportunity feed registry (docs/feeds.md); feeds-collect runs as a one-off container from the host timer
COPY feeds ./feeds
ENV API_ADDR=:8110 UPLOAD_DIR=/app/uploads MIGRATIONS_DIR=/app/migrations DATA_DIR=/app/data
ARG BUILD_ID=dev
LABEL org.opencontainers.image.title="studenthub-api" org.opencontainers.image.revision=$BUILD_ID
USER studenthub
EXPOSE 8110
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8110/api/health || exit 1
CMD ["/app/server"]
