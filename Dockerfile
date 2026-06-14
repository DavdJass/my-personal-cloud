# ── Stage 1: Build the React frontend ──
FROM node:20-alpine AS frontend
WORKDIR /build
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ .
RUN npm run build

# ── Stage 2: Build the Go backend ──
FROM golang:1.22-alpine AS backend
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /build/dist ./web/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o my-personal-cloud ./cmd/server

# ── Stage 3: Minimal runtime image ──
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
RUN adduser -D -H -h /data cloud

# Use /data as working directory — it's the cloud user's home and writable.
# Railway / Docker volumes mount here for persistence.
WORKDIR /data

COPY --from=backend /build/my-personal-cloud /usr/local/bin/my-personal-cloud

# Default paths point to /data (the mounted volume).
# Override any of these via environment variables or docker-compose.
ENV CLOUD_STORAGE_ROOT=/data/storage
ENV CLOUD_DB_PATH=/data/cloud.db

# The cloud user can write to /data but not to /
USER cloud

# The server listens on :8080 by default.
# Override with CLOUD_ADDR=:$PORT for Railway or other PaaS.
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
  CMD wget -qO- http://localhost:8080/api/health || exit 1

ENTRYPOINT ["my-personal-cloud"]
