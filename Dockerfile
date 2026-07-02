# syntax=docker/dockerfile:1
# Small-Server — Fly.io-ready image. Multi-stage Go build.
# Final image is tiny: a static binary on a minimal base.

# ─── Stage 1: build ─────────────────────────────────────────────────────────
FROM golang:1.26-alpine AS builder

WORKDIR /src

# Cache deps first (only re-fetch when go.mod/go.sum change).
COPY go.mod go.sum ./
RUN go mod download

# App source (main.go + embedded index.html + openapi.json).
COPY main.go index.html openapi.json ./

# Static build: CGO off (modernc.org/sqlite is pure Go), stripped, trimmed.
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -ldflags="-s -w" -trimpath -o /out/small-server .

# Pre-create /data owned by nonroot so the volume mount point is writable.
# distroless nonroot runs as uid 65532 — chown the dir to that uid.
RUN mkdir -p /out/data && chown -R 65532:65532 /out/data

# ─── Stage 2: runtime ───────────────────────────────────────────────────────
# Alpine is ~7MB, has a shell for debugging, CA certs included. The Go binary
# is fully static so no libc needed.
FROM alpine:3.20

# CA certs (in case of outbound HTTPS) + timezone data, no shell needed.
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /out/small-server /app/small-server

# Persistent SQLite lives here (mounted as a Fly volume in production).
# /data is created and owned by 65532 in the builder; Fly mounts the volume
# over it, also writable by the container user.
RUN mkdir -p /data && chown -R 65532:65532 /data

ENV DB_PATH=/data/data.db \
    PORT=8080

# Run as a non-root user for least privilege.
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/app/small-server"]
