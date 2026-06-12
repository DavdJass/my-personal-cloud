# My Personal Cloud

A self-hosted personal cloud server built with Go. Lightweight, dependency-free (no CGO), designed to run on a **Raspberry Pi** and serve files, photos, and videos through a modern web interface.

- **Backend:** Go 1.22+ with `chi` router, SQLite (pure-Go, no CGO), JWT authentication, rate limiting, structured logging.
- **Frontend:** React 18 + TypeScript + Vite, embedded in the Go binary — zero external runtime dependencies.
- **Deployment:** Single binary + Caddy (automatic HTTPS) + Tailscale (secure remote access without opening ports).

---

## Features

| Category | Capabilities |
|---|---|
| **File management** | List, upload, download, rename, move files and folders. Drag & drop files to upload or move to a folder. Pagination, grid/list view toggle, sorting by name/type/size/date. |
| **Search** | Real-time search across files and folders by name. |
| **Trash / soft delete** | Delete sends files to trash. Restore from trash or permanently empty it. |
| **Photo gallery** | Automatic thumbnail generation for images. Infinite scroll gallery view. |
| **Video playback** | Stream videos directly in the browser (MP4, WebM, etc.). |
| **Authentication** | JWT-based login with configurable expiry. Refresh tokens. Rate-limited login endpoint. |
| **Clipboard upload** | Paste images directly from the clipboard. |
| **Keyboard shortcuts** | `Delete` to trash selected items, `Ctrl+F` to focus search. |
| **Dark / Light theme** | Theme toggle with persistence in localStorage. |
| **Multi-user** | Admin user bootstrap via environment variables. |

---

## Architecture

```
Browser  ──HTTPS──>  Caddy (reverse proxy)  ──HTTP──>  Go server (:8080)  ──>  SQLite + disk
                        ^
                        │
                     Tailscale (secure remote access from outside the LAN)
```

All uploaded files are stored on disk in an isolated directory per user. The SQLite database holds metadata: users, files, folders, and gallery indexes. Thumbnails are generated lazily and cached.

---

## Repository layout

```
my-personal-cloud/
├── cmd/server/           Server entrypoint (main.go)
├── internal/
│   ├── auth/             JWT login, middleware, refresh tokens
│   ├── config/           Configuration loading from environment variables
│   ├── db/               SQLite connection, schema migrations
│   ├── files/            File and folder CRUD endpoints
│   ├── mime/             MIME type detection from magic bytes
│   ├── photos/           Gallery listing, thumbnail generation
│   ├── ratelimit/        Per-IP sliding-window rate limiter
│   └── storage/          Local filesystem abstraction (isolated per user)
├── web/                  React + TypeScript frontend (Vite)
│   ├── src/
│   │   ├── pages/        FilesPage, GalleryPage, LoginPage
│   │   ├── api.ts        API client with JWT handling
│   │   ├── toast.tsx     Toast notification system
│   │   └── theme.tsx     Dark/light theme provider
│   └── dist/             Build output (embedded in Go binary)
├── deploy/               Systemd service unit file
├── Caddyfile             Example reverse proxy configuration
├── Makefile              Build shortcuts (local + cross-compile)
└── README.md             This file
```

---

## Quick start (local development)

Requirements: **Go 1.22+** and **Node 20+**.

```bash
# 1. Clone the repository
git clone https://github.com/DavdJass/my-personal-cloud.git
cd my-personal-cloud

# 2. Install dependencies
make deps

# 3. Set admin credentials and start
export CLOUD_ADMIN_USER=admin
export CLOUD_ADMIN_PASS=admin
export CLOUD_JWT_SECRET="change-this-in-production"
go run ./cmd/server

# Open http://localhost:8080
```

For hot-reload development (two terminals):

```bash
# Terminal 1: Go server
export CLOUD_ADMIN_USER=admin
export CLOUD_ADMIN_PASS=admin
go run ./cmd/server

# Terminal 2: Vite dev server (proxies /api to :8080)
cd web && npm run dev
# Open http://localhost:5173
```

To build a single self-contained binary:

```bash
make build      # Produces ./my-personal-cloud
./my-personal-cloud
```

> **Windows (PowerShell):** Use `$env:CLOUD_ADMIN_USER = "admin"` instead of `export`.

---

## Environment variables

| Variable | Default | Required | Description |
|---|---|---|---|
| `CLOUD_ADDR` | `:8080` | No | TCP address the server listens on |
| `CLOUD_STORAGE_ROOT` | `./data/storage` | No | Directory where uploaded files are stored |
| `CLOUD_DB_PATH` | `./data/cloud.db` | No | Path to the SQLite database file |
| `CLOUD_JWT_SECRET` | (random at startup) | **Recommended** | HMAC key for JWT signing. Set this to persist sessions across restarts |
| `CLOUD_JWT_EXPIRY_HOURS` | `24` | No | JWT token validity in hours |
| `CLOUD_MAX_UPLOAD_MB` | `500` | No | Maximum single upload size in MB |
| `CLOUD_CORS_ORIGIN` | `*` | No | Allowed CORS origin. Set to your domain in production (e.g. `https://cloud.example.com`) |
| `CLOUD_ADMIN_USER` | (empty) | **Yes** | Bootstrap admin username (created on first start) |
| `CLOUD_ADMIN_PASS` | (empty) | **Yes** | Bootstrap admin password |

> `CLOUD_JWT_SECRET` has a random fallback. If unset, all tokens become invalid each time the server restarts — always set a fixed secret in production.

---

## Deploying on a Raspberry Pi

### Prerequisites

- Raspberry Pi (3B+ or newer recommended, Pi 5 ideal) running **Raspberry Pi OS** (64-bit).
- External USB drive or SSD for storage (optional but recommended).
- Your development machine with Go 1.22+ and Node 20+.

---

### Step 1: Cross-compile from your machine

On your development machine, run:

```bash
make build-pi
```

This produces a statically-linked ARM64 binary `my-personal-cloud-arm64` with the frontend embedded. No CGO, no external dependencies.

---

### Step 2: Copy files to the Raspberry Pi

```bash
scp my-personal-cloud-arm64 pi@raspberrypi.local:/tmp/
scp Caddyfile pi@raspberrypi.local:/tmp/
scp deploy/my-personal-cloud.service pi@raspberrypi.local:/tmp/
```

If hostname resolution doesn't work, use the Pi's IP address directly:

```bash
scp my-personal-cloud-arm64 pi@192.168.1.X:/tmp/
```

---

### Step 3: Set up the server on the Pi

SSH into the Pi and run the following commands:

```bash
# Create a dedicated system user
sudo useradd -r -s /usr/sbin/nologin cloud

# Create directories
sudo mkdir -p /opt/my-personal-cloud /mnt/cloud/storage

# Place the binary
sudo mv /tmp/my-personal-cloud-arm64 /opt/my-personal-cloud/my-personal-cloud
sudo chmod +x /opt/my-personal-cloud/my-personal-cloud
sudo chown -R cloud:cloud /opt/my-personal-cloud /mnt/cloud

# Create the environment file with secrets
sudo tee /etc/my-personal-cloud.env >/dev/null <<'EOF'
CLOUD_JWT_SECRET=$(openssl rand -hex 32)
CLOUD_ADMIN_USER=admin
CLOUD_ADMIN_PASS=your-strong-password-here
EOF

# Secure the secrets file
sudo chmod 600 /etc/my-personal-cloud.env

# IMPORTANT: Open the env file and replace $(openssl rand -hex 32)
# with an actual generated secret (or delete the $() and it'll expand at runtime)
```

Now open the env file to set a proper JWT secret:

```bash
sudo nano /etc/my-personal-cloud.env
```

Replace `$(openssl rand -hex 32)` with an actual hex string (you can generate one with `openssl rand -hex 32` on your machine). It should look like:

```
CLOUD_JWT_SECRET=a1b2c3d4e5f6...  (64 hex characters)
CLOUD_ADMIN_USER=admin
CLOUD_ADMIN_PASS=your-strong-password-here
```

---

### Step 4: Install the systemd service

```bash
sudo mv /tmp/my-personal-cloud.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now my-personal-cloud

# Verify it's running
sudo systemctl status my-personal-cloud

# Check the logs
sudo journalctl -u my-personal-cloud -f
```

---

### Step 5: Set up Caddy (reverse proxy + HTTPS)

Install Caddy:

```bash
sudo apt update && sudo apt install -y caddy
```

Install the Caddyfile:

```bash
sudo mv /tmp/Caddyfile /etc/caddy/Caddyfile
sudo systemctl restart caddy
```

By default, the Caddyfile serves on `https://cloud.local` with a **self-signed certificate**. Your browser will show a security warning — this is expected for local-only access. You can safely proceed.

For a **public domain**, edit `/etc/caddy/Caddyfile`, uncomment the `cloud.example.com` block, replace it with your domain, and ensure ports 80/443 are forwarded on your router. Caddy will automatically obtain a Let's Encrypt certificate.

> **Tip:** Add `cloud.local` to your router's DNS or your local machine's `hosts` file so all devices on your network can reach the cloud by name.

---

### Step 6: (Optional) Remote access with Tailscale

Tailscale gives you secure remote access without opening any ports on your router.

```bash
# Install Tailscale
curl -fsSL https://tailscale.com/install.sh | sudo sh

# Authenticate (follow the URL printed)
sudo tailscale up

# (Optional) Get a TLS certificate for your Tailscale hostname
sudo tailscale cert
```

Install Tailscale on your phone, laptop, or other devices. You can reach the Pi by its **Tailscale IP** (`100.x.y.z`) or hostname (`<hostname>.<tailnet>.ts.net`).

If using Tailscale, you can enable the Tailscale Caddy config block in `Caddyfile` for automatic HTTPS via Tailscale certificates.

---

### Step 7: Mount external storage (recommended)

Using the SD card for both the OS and file storage is not ideal. Mount an external USB drive at `/mnt/cloud`:

```bash
# Identify your external drive
sudo lsblk

# Format it (if new) — REPLACE sda1 with your actual device!
sudo mkfs.ext4 /dev/sda1

# Get the UUID
sudo blkid /dev/sda1

# Add to /etc/fstab (replace UUID with yours)
echo 'UUID=xxxx-xxxx  /mnt/cloud  ext4  defaults,noatime,nofail  0  2' | sudo tee -a /etc/fstab

# Mount it
sudo mount /mnt/cloud

# Fix permissions
sudo chown cloud:cloud /mnt/cloud
```

---

## REST API

All routes live under `/api`. Protected routes require an `Authorization: Bearer <jwt>` header (or `?token=<jwt>` query parameter for downloads and media tags).

| Method | Route | Description |
|---|---|---|
| `GET` | `/api/health` | Health check |
| `POST` | `/api/auth/login` | Login — returns a JWT |
| `GET` | `/api/auth/me` | Current authenticated user |
| `POST` | `/api/auth/refresh` | Refresh the current JWT |
| `GET` | `/api/files?path=/&limit=50&offset=0` | List files and folders in a directory (paginated) |
| `GET` | `/api/files/search?q=term` | Search files and folders by name |
| `POST` | `/api/files/upload` | Upload a file (multipart form) |
| `GET` | `/api/files/{id}/download` | Download a file (`?inline=1` to view in browser) |
| `PATCH` | `/api/files/{id}` | Rename or move a file |
| `DELETE` | `/api/files/{id}` | Soft-delete a file (sends to trash) |
| `POST` | `/api/files/{id}/restore` | Restore a file from trash |
| `POST` | `/api/folders` | Create a folder |
| `PATCH` | `/api/folders/{id}` | Rename or move a folder (cascades to children) |
| `DELETE` | `/api/folders/{id}` | Delete a folder and all its contents |
| `GET` | `/api/photos?limit=50&offset=0` | List photos (paginated) |
| `GET` | `/api/photos/{id}/thumb?size=256` | JPEG thumbnail |
| `GET` | `/api/photos/{id}/full` | Full-resolution image |
| `GET` | `/api/trash` | List files in trash |
| `POST` | `/api/trash/empty` | Permanently delete all trashed files |

### Login example

```bash
curl -s http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}'
```

---

## Updating

When you pull new changes and want to update your Raspberry Pi:

```bash
# On your development machine
git pull
make build-pi

# Copy to Pi (add -t for timestamp-based diff)
scp my-personal-cloud-arm64 pi@raspberrypi.local:/tmp/

# On the Pi
sudo systemctl stop my-personal-cloud
sudo cp /tmp/my-personal-cloud-arm64 /opt/my-personal-cloud/my-personal-cloud
sudo chmod +x /opt/my-personal-cloud/my-personal-cloud
sudo systemctl start my-personal-cloud
```

Database migrations run automatically on startup — no manual steps needed.

---

## Troubleshooting

| Problem | Likely cause and fix |
|---|---|
| **Server won't start, port in use** | Another process is on port 8080. Check with `sudo ss -tlnp \| grep 8080` and stop it. |
| **"Permission denied" on uploads** | The `cloud` user doesn't own the storage directory. Run `sudo chown -R cloud:cloud /mnt/cloud`. |
| **Browser shows "Connection refused"** | The Go server isn't running. Check `sudo systemctl status my-personal-cloud` and `sudo journalctl -u my-personal-cloud -n 20`. |
| **Caddy shows "502 Bad Gateway"** | Caddy can't reach the Go server. Make sure the server is listening on `127.0.0.1:8080` (check with `ss -tlnp`). |
| **SSL certificate warning** | Expected with `tls internal` (self-signed). Add an exception in your browser, or set up a real domain + Let's Encrypt. |
| **Login says "too many requests"** | The rate limiter blocks after 5 failed attempts per minute per IP. Wait a minute or restart the server. |
| **Sessions lost after restart** | You forgot to set `CLOUD_JWT_SECRET`. Set it in `/etc/my-personal-cloud.env` and restart. |

---

## Technical notes

- **Zero CGO:** The entire Go stack compiles with `CGO_ENABLED=0`, thanks to `modernc.org/sqlite` — a pure-Go reimplementation of SQLite. Cross-compilation is seamless.
- **SQLite WAL mode:** The database uses Write-Ahead Logging for better concurrent read performance.
- **Thumbnails:** Generated lazily on first request using `disintegration/imaging`, cached to disk.
- **Security:** The systemd service runs with `NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome=true`, and `PrivateTmp=true`. The storage layer prevents path traversal by validating all file paths.
- **Frontend embedding:** The Vite build output is embedded into the Go binary at compile time via `//go:embed`. The server includes a SPA fallback handler for client-side routing.

---

## License

MIT.
