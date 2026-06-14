# My Personal Cloud

A self-hosted personal cloud server built with Go. Lightweight, dependency-free (no CGO, no containers required), designed to run on **Raspberry Pi**, **a VPS**, or **any Linux server** — serving files, photos, and videos through a modern web interface.

- **Backend:** Go 1.22+ with `chi` router, SQLite (pure-Go, no CGO), JWT authentication, rate limiting, encrypted backups.
- **Frontend:** React 18 + TypeScript + Vite, embedded in the Go binary — zero external runtime dependencies.
- **Networking:** Single binary + Caddy (automatic HTTPS, Let's Encrypt) + Tailscale (secure zero-config VPN).

---

## Features

| Category | Capabilities |
|---|---|
| **File management** | List, upload, download, rename, move files and folders. Drag & drop to upload or move items. Pagination, grid/list toggle. |
| **Search** | Real-time search across files and folders by name. |
| **Trash / soft delete** | Delete moves files to trash. Restore or empty trash permanently. |
| **Photo gallery** | Automatic thumbnail generation (lazy, cached). Infinite scroll gallery view. |
| **Video playback** | Stream videos (MP4, WebM, etc.) directly in the browser. |
| **Share links** | Generate time-limited, view-count-limited share URLs for files or folders. |
| **Encrypted backups** | Full-system backup (DB + files) with AES-256-GCM encryption. Optional passkey. Auto-restart after restore. |
| **Authentication** | JWT-based login, refresh tokens, rate-limited login, configurable expiry. |
| **Multi-user** | Admin user created on first start (configured via env vars). |
| **Dark / Light theme** | Theme toggle persisted in localStorage. |
| **Keyboard shortcuts** | `Delete` to trash, `Ctrl+F` to focus search. |
| **Clipboard upload** | Paste images directly from clipboard. |

---

## Table of Contents

1. [Architecture](#architecture)
2. [Repository layout](#repository-layout)
3. [Quick start (local dev)](#quick-start-local-development)
4. [Environment variables](#environment-variables)
5. [Production deployment guides](#production-deployment-guides)
   - [Raspberry Pi (manual)](#raspberry-pi-manual-deployment)
   - [Linux server / VPS (manual)](#linux-server--vps-manual-deployment)
   - [Docker](#docker-deployment)
   - [One-click deploy script](#one-click-deploy)
6. [Security hardening](#security-hardening)
7. [Backup & disaster recovery](#backup--disaster-recovery)
8. [Monitoring & logging](#monitoring--logging)
9. [Performance tuning](#performance-tuning)
10. [Maintenance & updates](#maintenance--updates)
11. [REST API reference](#rest-api)
12. [Troubleshooting](#troubleshooting)

---

## Architecture

```
                         Internet
                            │
               ┌────────────┴────────────┐
               │    Tailscale VPN         │  (optional — zero-config remote access)
               └────────────┬────────────┘
                            │
Browser ──HTTPS──> Caddy (reverse proxy) ──HTTP──> Go server (:127.0.0.1:8080)
                                                      │
                                           ┌──────────┴──────────┐
                                           │   SQLite database    │
                                           │   (data/cloud.db)   │
                                           └─────────────────────┘
                                           ┌─────────────────────┐
                                           │   File storage      │
                                           │   (data/storage/)   │
                                           └─────────────────────┘
```

- **Caddy** terminates TLS, provides automatic HTTPS via Let's Encrypt, compresses responses, and proxies to the Go server.
- **Go server** handles all business logic, authentication, and file I/O. Listens only on `127.0.0.1:8080` (not exposed directly).
- **SQLite** (WAL mode) stores metadata: users, files, folders, shares, gallery indexes.
- **Disk** stores raw uploaded files in an isolated directory per user. Thumbnails cached to `_thumbs/`.

---

## Repository layout

```
my-personal-cloud/
├── cmd/server/             Server entrypoint (main.go)
├── internal/
│   ├── auth/               JWT login, middleware, refresh tokens
│   ├── backup/             Encrypted backup/restore logic
│   ├── config/             Configuration from environment variables
│   ├── db/                 SQLite connection, schema migrations (v1-v3)
│   ├── files/              File and folder CRUD
│   ├── mime/               MIME type detection from magic bytes
│   ├── photos/             Gallery listing, thumbnail generation
│   ├── ratelimit/          Per-IP sliding-window rate limiter
│   ├── shares/             Share link creation and public access
│   └── storage/            Local filesystem abstraction
├── web/                    React + TypeScript frontend (Vite)
│   ├── src/pages/          FilesPage, GalleryPage, SharedPage, BackupPage, etc.
│   └── dist/               Build output (embedded in Go binary via //go:embed)
├── deploy/                 Systemd service unit, deploy scripts
├── Caddyfile               Reverse proxy configuration
├── Makefile                Build targets
└── README.md               This file
```

---

## Quick start (local development)

**Requirements:** Go 1.22+, Node 20+, npm.

```bash
# 1. Clone
git clone https://github.com/DavdJass/my-personal-cloud.git
cd my-personal-cloud

# 2. Install deps (Go modules + npm packages)
make deps

# 3. Build frontend and start server
export CLOUD_ADMIN_USER=admin
export CLOUD_ADMIN_PASS=your-password
export CLOUD_JWT_SECRET="change-this-in-production"
make run

# Open http://localhost:8080
```

For hot-reload development:

```bash
# Terminal 1: Go server
export CLOUD_ADMIN_USER=admin
export CLOUD_ADMIN_PASS=your-password
go run ./cmd/server

# Terminal 2: Vite dev server (proxies /api to :8080)
cd web && npm run dev
# Open http://localhost:5173
```

**Windows (PowerShell):** Use `$env:CLOUD_ADMIN_USER = "admin"` instead of `export`.

---

## Environment variables

| Variable | Default | Required | Description |
|---|---|---|---|
| `CLOUD_ADDR` | `127.0.0.1:8080` | No | TCP address the server listens on (use `:8080` for all interfaces) |
| `CLOUD_STORAGE_ROOT` | `./data/storage` | No | Directory for uploaded files |
| `CLOUD_DB_PATH` | `./data/cloud.db` | No | Path to the SQLite database file |
| `CLOUD_JWT_SECRET` | (random at startup) | **Recommended** | HMAC key for JWT signing. Set this to persist sessions across restarts |
| `CLOUD_JWT_EXPIRY_HOURS` | `24` | No | JWT token validity in hours |
| `CLOUD_MAX_UPLOAD_MB` | `10240` (10 GiB) | No | Maximum single upload size in MB |
| `CLOUD_CORS_ORIGIN` | `*` | No | Allowed CORS origin (set to your domain in production) |
| `CLOUD_ADMIN_USER` | (empty) | **Yes** | Bootstrap admin username (created on first start) |
| `CLOUD_ADMIN_PASS` | (empty) | **Yes** | Bootstrap admin password (use a strong password) |

> **Critical:** `CLOUD_JWT_SECRET` has a random fallback. If unset, all tokens become invalid on every restart — **always set a fixed secret in production**. Use `openssl rand -hex 32` to generate one.

---

## Production deployment guides

### Raspberry Pi (manual deployment)

#### Prerequisites

- Raspberry Pi (3B+ or newer, Pi 5 ideal) running **Raspberry Pi OS 64-bit**.
- External USB SSD for storage (recommended — SD cards wear out).
- Your development machine with Go 1.22+ and Node 20+.

#### Step 1: Cross-compile on your dev machine

```bash
make build-pi
```

This produces `my-personal-cloud-arm64` — a statically-linked ARM64 binary with the frontend embedded. No CGO, no external dependencies.

#### Step 2: Copy files to the Pi

```bash
scp my-personal-cloud-arm64 pi@raspberrypi.local:/tmp/
scp Caddyfile pi@raspberrypi.local:/tmp/
scp deploy/my-personal-cloud.service pi@raspberrypi.local:/tmp/
```

If hostname resolution doesn't work, use the Pi's IP:

```bash
scp my-personal-cloud-arm64 pi@192.168.1.X:/tmp/
```

#### Step 3: Set up the server on the Pi

SSH into the Pi:

```bash
# Create a dedicated system user (no shell, no login)
sudo useradd -r -s /usr/sbin/nologin cloud

# Create directories
sudo mkdir -p /opt/my-personal-cloud /mnt/cloud/storage

# Place the binary
sudo mv /tmp/my-personal-cloud-arm64 /opt/my-personal-cloud/my-personal-cloud
sudo chmod +x /opt/my-personal-cloud/my-personal-cloud
sudo chown -R cloud:cloud /opt/my-personal-cloud /mnt/cloud

# Create and secure the environment file
sudo tee /etc/my-personal-cloud.env >/dev/null <<'EOF'
CLOUD_JWT_SECRET=replace-with-openssl-rand-hex-32-output
CLOUD_ADMIN_USER=admin
CLOUD_ADMIN_PASS=your-very-strong-password
CLOUD_ADDR=127.0.0.1:8080
CLOUD_STORAGE_ROOT=/mnt/cloud/storage
CLOUD_DB_PATH=/mnt/cloud/cloud.db
CLOUD_CORS_ORIGIN=https://cloud.local
CLOUD_MAX_UPLOAD_MB=500
CLOUD_JWT_EXPIRY_HOURS=24
EOF

sudo chmod 600 /etc/my-personal-cloud.env
```

Generate a proper JWT secret:

```bash
openssl rand -hex 32
# Copy the output, then edit the env file:
sudo nano /etc/my-personal-cloud.env
# Replace the CLOUD_JWT_SECRET value with the generated hex string
```

#### Step 4: Install the systemd service

```bash
sudo mv /tmp/my-personal-cloud.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now my-personal-cloud

# Verify
sudo systemctl status my-personal-cloud

# Watch logs
sudo journalctl -u my-personal-cloud -f
```

#### Step 5: Set up Caddy (reverse proxy + HTTPS)

```bash
sudo apt update && sudo apt install -y caddy
sudo mv /tmp/Caddyfile /etc/caddy/Caddyfile
sudo systemctl restart caddy
```

By default, the provided `Caddyfile` serves on `https://cloud.local` with a **self-signed certificate**. Your browser will show a security warning — this is expected for local-only access. Add `cloud.local` to your devices' `hosts` file or your router's DNS for LAN-wide name resolution.

For a **public domain**: edit `/etc/caddy/Caddyfile`, uncomment the `cloud.example.com` block, replace with your domain, and ensure ports 80/443 are open on your router. Caddy will obtain a Let's Encrypt certificate automatically.

#### Step 6: Mount external storage (strongly recommended)

```bash
# Identify your external drive
sudo lsblk

# Format (REPLACE sda1 with your device!)
sudo mkfs.ext4 /dev/sda1

# Get UUID
sudo blkid /dev/sda1

# Add to fstab for auto-mount on boot
echo 'UUID=your-uuid-here  /mnt/cloud  ext4  defaults,noatime,nofail  0  2' | sudo tee -a /etc/fstab

# Mount
sudo mount /mnt/cloud

# Fix permissions
sudo chown cloud:cloud /mnt/cloud
```

#### Step 7: (Optional) Remote access with Tailscale

Tailscale gives you secure remote access without opening ports or exposing your server to the internet.

```bash
# Install Tailscale
curl -fsSL https://tailscale.com/install.sh | sudo sh

# Authenticate (follow the URL printed)
sudo tailscale up

# (Optional) Get TLS certificate for your Tailscale hostname
sudo tailscale cert
```

Install Tailscale on your phone, laptop, or other devices. Access the cloud at `https://<hostname>.<tailnet>.ts.net`.

To use Tailscale with Caddy, edit your Caddyfile and uncomment the Tailscale block, then:

```bash
sudo systemctl restart caddy
```

---

### Linux server / VPS (manual deployment)

For an AMD64 VPS (DigitalOcean, Hetzner, Linode, etc.):

```bash
# On your dev machine: build for linux/amd64
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o my-personal-cloud-amd64 ./cmd/server

# Copy to VPS
scp my-personal-cloud-amd64 user@your-vps-ip:/tmp/
scp Caddyfile user@your-vps-ip:/tmp/
scp deploy/my-personal-cloud.service user@your-vps-ip:/tmp/
```

Then SSH into your VPS and follow the same setup as the Pi guide (steps 3–5), adjusting paths as needed. On a VPS you'll typically use a public domain with Let's Encrypt.

#### Firewall rules (VPS)

```bash
# Allow SSH, HTTP, and HTTPS only
sudo ufw allow OpenSSH
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw --force enable
sudo ufw status
```

> The Go server listens on `127.0.0.1:8080` — it is **not exposed** to the internet. Only Caddy on ports 80/443 is public. This is a critical security boundary.

---

### Docker deployment

The project also supports Docker for containerized deployment. This is ideal if you already use Docker or want isolated environments.

#### Prerequisites

- Docker and Docker Compose installed on your server.

#### Quick start with Docker Compose

Create a `docker-compose.yml`:

```yaml
version: "3.9"

services:
  cloud:
    build: .
    container_name: my-personal-cloud
    restart: unless-stopped
    ports:
      - "127.0.0.1:8080:8080"
    environment:
      - CLOUD_ADDR=:8080
      - CLOUD_JWT_SECRET=${CLOUD_JWT_SECRET}
      - CLOUD_ADMIN_USER=${CLOUD_ADMIN_USER}
      - CLOUD_ADMIN_PASS=${CLOUD_ADMIN_PASS}
      - CLOUD_CORS_ORIGIN=${CLOUD_CORS_ORIGIN:-https://cloud.example.com}
      - CLOUD_MAX_UPLOAD_MB=500
      - CLOUD_JWT_EXPIRY_HOURS=24
    volumes:
      - cloud-data:/data
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:8080/api/health"]
      interval: 30s
      timeout: 5s
      retries: 3

  caddy:
    image: caddy:2-alpine
    container_name: cloud-caddy
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy-data:/data
      - caddy-config:/config
    depends_on:
      - cloud

volumes:
  cloud-data:
  caddy-data:
  caddy-config:
```

Create a `.env` file alongside `docker-compose.yml`:

```bash
CLOUD_JWT_SECRET=$(openssl rand -hex 32)
CLOUD_ADMIN_USER=admin
CLOUD_ADMIN_PASS=your-strong-password
CLOUD_CORS_ORIGIN=https://cloud.example.com
```

> **Security:** Mount port 8080 on `127.0.0.1` only, so Caddy (running on the host network or another container) reaches it but the internet cannot. Never expose port 8080 directly.

#### Build and run

```bash
docker compose up -d --build
docker compose logs -f cloud
```

#### Building the Docker image manually

```dockerfile
# Dockerfile
FROM node:20-alpine AS frontend
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ .
RUN npm run build

FROM golang:1.22-alpine AS backend
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /web/dist ./web/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o my-personal-cloud ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
RUN adduser -D -H -h /opt/my-personal-cloud cloud
WORKDIR /opt/my-personal-cloud
COPY --from=backend /app/my-personal-cloud .
USER cloud
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
  CMD wget -qO- http://localhost:8080/api/health || exit 1
ENTRYPOINT ["./my-personal-cloud"]
```

---

### One-click deploy

The `deploy/` directory includes automated deployment scripts for Bash and PowerShell.

#### Bash (`deploy.sh`)

Copies the binary, Caddyfile, and systemd unit to a remote server via SSH, then runs the setup commands:

```bash
# On your dev machine
./deploy/deploy.sh pi@raspberrypi.local
```

Behind the scenes, the script:

1. Cross-compiles the binary for `linux/arm64` (or `linux/amd64`).
2. Copies files via `scp`.
3. SSHes into the server and runs the installation commands.
4. Starts the systemd service.

#### PowerShell (`deploy.ps1`)

Same functionality for Windows users:

```powershell
.\deploy\deploy.ps1 -Target pi@raspberrypi.local
```

---

## Security hardening

A checklist for production deployments.

### 1. Strong JWT secret

```bash
# Generate a cryptographically random 256-bit key
openssl rand -hex 32
```

Set it in your environment file and **never commit it to git**.

### 2. CORS origin

Set `CLOUD_CORS_ORIGIN` to your exact domain. Do not leave it as `*` in production:

```bash
CLOUD_CORS_ORIGIN=https://cloud.example.com
```

### 3. Admin password

Use a strong, unique password (20+ characters, mixed case, numbers, symbols). The server stores it as a bcrypt hash.

### 4. Network isolation

- The Go server binds to `127.0.0.1:8080` — only accessible from localhost.
- Caddy (or another reverse proxy) terminates TLS and proxies requests to the Go server.
- Only ports 80 (redirect) and 443 (HTTPS) are open to the internet.

### 5. Systemd hardening (already configured)

The service unit includes:
- `NoNewPrivileges=true` — prevents privilege escalation.
- `ProtectSystem=strict` — read-only filesystem except `ReadWritePaths`.
- `ProtectHome=true` — blocks access to `/home`, `/root`.
- `PrivateTmp=true` — isolated temp directory.

### 6. File upload safety

- MIME type validation from magic bytes (not file extension).
- Configurable max upload size (default 500 MB in production).
- Files stored without their original names (UUID-based).
- Path traversal prevention.

### 7. Rate limiting

- Login endpoint: 5 attempts per minute per IP.
- Upload endpoints: configurable limit.
- General API: configurable limit.

### 8. HTTPS only

Caddy automatically redirects HTTP → HTTPS. The provided `Caddyfile` enforces this.

### 9. Regular updates

Keep your server patched:

```bash
# System
sudo apt update && sudo apt upgrade -y

# My Personal Cloud (rebuild and copy new binary)
# See "Maintenance & updates" below
```

---

## Backup & disaster recovery

The built-in backup feature creates AES-256-GCM encrypted ZIP archives containing the full database + all stored files.

### Creating a backup

Via the web UI: navigate to **Backup** → **Crear Backup**.

- **Default mode:** encrypted with a key derived from your JWT secret. Restorable on any instance with the same JWT secret.
- **Passkey mode:** encrypted with a user-provided passkey. Only restorable with that exact passkey.

### Restoring a backup

Via the web UI: navigate to **Backup** → upload a `.mpcbackup` file.

- The server will ask for a passkey **if** the backup was encrypted with one (auto-detected).
- After a successful restore, the server **automatically restarts** to reload the database and all services.

### Manual backup (command line)

If you prefer scripting your own backup cycle:

```bash
# Backup commands via the API
TOKEN=$(curl -s -X POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"your-password"}' | jq -r '.token')

# Create backup (default encryption)
curl -o /backups/cloud-backup-$(date +%F).mpcbackup \
  -H "Authorization: Bearer $TOKEN" \
  -X POST http://localhost:8080/api/backup/create

# Create backup with passkey
curl -o /backups/cloud-backup-$(date +%F)-key.mpcbackup \
  -H "Authorization: Bearer $TOKEN" \
  -X POST http://localhost:8080/api/backup/create \
  -F "passkey=your-secure-passphrase"
```

### Automated backup schedule (cron)

Add a daily backup to cron:

```bash
sudo crontab -e

# Add this line (runs at 3 AM daily)
0 3 * * * /usr/local/bin/backup-cloud.sh
```

Create `/usr/local/bin/backup-cloud.sh`:

```bash
#!/bin/bash
TOKEN=$(curl -s -X POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$CLOUD_ADMIN_PASS\"}" | jq -r '.token')

curl -o "/backups/cloud-backup-$(date +%F).mpcbackup" \
  -H "Authorization: Bearer $TOKEN" \
  -X POST http://localhost:8080/api/backup/create

# Keep only last 30 backups
find /backups -name 'cloud-backup-*.mpcbackup' -mtime +30 -delete
```

### Disaster recovery plan

1. **Data loss / corruption:** Restore the latest backup via the web UI.
2. **Hardware failure:** Set up a new server (Pi, VPS, etc.), deploy the binary, then restore your latest `.mpcbackup` file. The restore includes both database and file data.
3. **Lost passkey:** If you encrypted a backup with a passkey and lost it, the backup **cannot** be recovered. Always keep passkeys in a password manager.
4. **No passkey, different server:** As long as you use the same `CLOUD_JWT_SECRET`, default-mode backups are portable across instances.

---

## Monitoring & logging

### Viewing logs

```bash
# Systemd journal (live)
sudo journalctl -u my-personal-cloud -f

# Last 50 lines
sudo journalctl -u my-personal-cloud -n 50 --no-pager
```

### Log format

The server uses structured JSON logging via Go's `log/slog`. Example:

```
time=2026-06-12T12:25:52.126-06:00 level=INFO msg="file uploaded" user_id=2 name=document.pdf parent=/
```

### Health check

```bash
curl http://127.0.0.1:8080/api/health
# Returns: {"status":"ok"}
```

### Monitoring with Prometheus (optional)

If you want metrics, you can extend the server or set up a separate monitoring stack. The health endpoint is sufficient for basic uptime monitoring.

### Alerting

Pair with a tool like **Uptime Kuma**, **Healthchecks.io**, or **Grafana OnCall** to monitor the health endpoint and alert you if the server goes down.

---

## Performance tuning

### SQLite

The database runs in WAL (Write-Ahead Logging) mode by default, which provides good concurrent read performance. No tuning is typically needed.

### Caddy compression

The Caddyfile enables `zstd` and `gzip` compression for all responses — most browsers will receive zstd-compressed data (faster than gzip).

### Upload size

`CLOUD_MAX_UPLOAD_MB` controls the per-file upload limit. The Caddyfile also caps uploads at `500 MB` by default. Increase both if you need to handle larger files:

```bash
# Go server
CLOUD_MAX_UPLOAD_MB=2048   # 2 GB

# Caddyfile
request_body {
    max_size 2GB
}
```

### File descriptor limits

The systemd service sets `LimitNOFILE=65536`. This is sufficient for thousands of concurrent connections. If you expect extreme load, increase it.

### Storage

- **Use an SSD**, not an SD card. SD cards have poor random I/O and limited write endurance.
- Mount with `noatime` to reduce write operations (already in the fstab example above).
- Consider a separate disk or partition for the database and file storage if you anticipate heavy usage.

---

## Maintenance & updates

### Updating the binary

```bash
# On your dev machine
git pull
make frontend               # rebuild frontend
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o my-personal-cloud-arm64 ./cmd/server
scp my-personal-cloud-arm64 pi@raspberrypi.local:/tmp/

# On the Pi
sudo systemctl stop my-personal-cloud
sudo cp /tmp/my-personal-cloud-arm64 /opt/my-personal-cloud/my-personal-cloud
sudo chmod +x /opt/my-personal-cloud/my-personal-cloud
sudo systemctl start my-personal-cloud

# Verify
sudo journalctl -u my-personal-cloud -n 10 --no-pager
```

### Database migrations

Migrations run automatically on startup. They are idempotent — safe to re-run. Current schema versions:

| Version | Adds |
|---|---|
| v1 | Initial schema: users, files, folders |
| v2 | Photos/gallery support |
| v3 | Share links table |

No manual migration steps are needed.

### Disk space management

```bash
# Check storage usage
du -sh /mnt/cloud/storage
du -sh /mnt/cloud/cloud.db
du -sh /mnt/cloud

# Empty the trash (via web UI) to reclaim space
# Or via API:
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/trash/empty
```

---

## REST API

All routes live under `/api`. Protected routes require an `Authorization: Bearer <jwt>` header (`?token=<jwt>` query parameter also works for downloads and media tags — logged and warned in server logs).

| Method | Route | Auth | Description |
|---|---|---|---|
| `GET` | `/api/health` | No | Health check |
| `POST` | `/api/auth/login` | No | Login — returns JWT |
| `GET` | `/api/auth/me` | Yes | Current user info |
| `POST` | `/api/auth/refresh` | Yes | Refresh JWT |
| `GET` | `/api/files?path=/&limit=50&offset=0` | Yes | List files/folders (paginated) |
| `GET` | `/api/files/search?q=term` | Yes | Search files by name |
| `POST` | `/api/files/upload` | Yes | Upload file (multipart) |
| `GET` | `/api/files/{id}/download` | Yes | Download file (`?inline=1` for browser view) |
| `PATCH` | `/api/files/{id}` | Yes | Rename or move a file |
| `DELETE` | `/api/files/{id}` | Yes | Soft-delete (to trash) |
| `POST` | `/api/files/{id}/restore` | Yes | Restore from trash |
| `POST` | `/api/folders` | Yes | Create folder |
| `PATCH` | `/api/folders/{id}` | Yes | Rename or move folder (cascades) |
| `DELETE` | `/api/folders/{id}` | Yes | Delete folder + contents |
| `GET` | `/api/photos?limit=50&offset=0` | Yes | List photos (paginated) |
| `GET` | `/api/photos/{id}/thumb?size=256` | Yes | JPEG thumbnail |
| `GET` | `/api/photos/{id}/full` | Yes | Full-resolution image |
| `GET` | `/api/trash` | Yes | List trashed files |
| `POST` | `/api/trash/empty` | Yes | Permanently empty trash |
| `POST` | `/api/shares` | Yes | Create share link |
| `GET` | `/api/shares` | Yes | List share links |
| `DELETE` | `/api/shares/{id}` | Yes | Revoke share link |
| `POST` | `/api/backup/create` | Yes | Create encrypted backup |
| `POST` | `/api/backup/restore` | Yes | Restore from backup |
| `GET` | `/api/public/share/{token}` | No | Access shared file/folder |

### Login example

```bash
curl -s http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"your-password"}'

# Response:
# {"token":"eyJ...","expires_in_seconds":86400,"user":{"id":2,"username":"admin"}}
```

---

## Troubleshooting

| Problem | Likely cause and fix |
|---|---|
| **Server won't start, port in use** | Another process is on port 8080. `sudo ss -tlnp \| grep 8080` and stop it. |
| **"Permission denied" on uploads** | The `cloud` user doesn't own the storage dir. `sudo chown -R cloud:cloud /mnt/cloud`. |
| **Browser shows "Connection refused"** | The Go server isn't running. `sudo systemctl status my-personal-cloud` and `sudo journalctl -u my-personal-cloud -n 20`. |
| **Caddy returns "502 Bad Gateway"** | Caddy can't reach the Go server. Ensure it's listening on `127.0.0.1:8080` (`ss -tlnp`). |
| **SSL certificate warning** | Expected with `tls internal` (self-signed). Add an exception, or set up a real domain + Let's Encrypt. |
| **Login says "too many requests"** | Rate limiter: 5 failed attempts/min/IP. Wait a minute. |
| **Sessions lost after restart** | `CLOUD_JWT_SECRET` not set. Set it in `/etc/my-personal-cloud.env` and restart. |
| **Upload fails with "request too large"** | Hit the Caddy or Go upload limit. Increase `max_size` in `Caddyfile` and/or `CLOUD_MAX_UPLOAD_MB`. |
| **Files appear after restore but show "Esta carpeta está vacía"** | The backup was made before the WAL checkpoint fix. The SQLite WAL file wasn't flushed to the main DB. Create a **new** backup after the latest update — the server now runs `PRAGMA wal_checkpoint(TRUNCATE)` before backing up. |
| **"sql: database is closed" errors after restore** | The old server process held closed DB connections. This is fixed — the server now automatically restarts after a restore, reloading all connections. |

---

## Technical notes

- **Zero CGO:** The entire Go stack compiles with `CGO_ENABLED=0`, thanks to `modernc.org/sqlite` — a pure-Go SQLite implementation. Cross-compilation is seamless.
- **SQLite WAL mode:** Better concurrent read performance. The backup service checkpoints the WAL to ensure backups are complete.
- **Thumbnails:** Generated lazily on first request using `disintegration/imaging`, cached to `_thumbs/` on disk.
- **Backup encryption:** AES-256-GCM. Keys derived either from `SHA-256("my-personal-cloud-backup" + JWT secret)` or PBKDF2(user passkey, random salt, 4096 iterations).
- **Security:** systemd isolation (`NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome=true`, `PrivateTmp=true`), MIME validation by content, path traversal prevention, rate limiting.
- **Frontend embedding:** Vite build output is embedded into the Go binary at compile time via `//go:embed`. The server includes an SPA fallback handler for client-side routing.
- **Backup file extension:** `.mpcbackup` (My Personal Cloud Backup).
- **Backup portability:** Default-mode backups are portable across instances sharing the same `CLOUD_JWT_SECRET`.

---

## License

MIT.
