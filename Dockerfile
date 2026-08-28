# ── build ─────────────────────────────────────────────────────
FROM golang:1.26-alpine AS build

WORKDIR /src

# Dependencies first so edits to the source do not re-download the module graph.
COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath -ldflags="-s -w" \
      -o /out/clipboard ./cmd/server

# ── runtime ───────────────────────────────────────────────────
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata wget \
 && adduser -D -u 10001 clipboard

WORKDIR /app
COPY --from=build /out/clipboard /app/clipboard
COPY web /app/web

RUN mkdir -p /data/blobs && chown -R clipboard:clipboard /data

USER clipboard

ENV ADDR=:8080 \
    WEB_DIR=/app/web \
    BLOB_DIR=/data/blobs

# Heroku assigns $PORT at runtime; the server prefers it over ADDR.
EXPOSE 8080
VOLUME ["/data"]

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- "http://127.0.0.1:${PORT:-8080}/api/health" || exit 1

# CMD rather than ENTRYPOINT: Heroku's container runtime supplies its own
# process command, which an ENTRYPOINT would turn into stray arguments.
CMD ["/app/clipboard"]
