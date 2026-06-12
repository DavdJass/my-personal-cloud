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
├── deploy/
│   ├── deploy.sh         One-click deployment script (Linux/Mac)
│   ├── deploy.ps1        One-click deployment script (Windows)
│   ├── .env.pi.example   Configuration template for the Pi
│   └── my-personal-cloud.service   systemd unit file
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
| `CLOUD_MAX_UPLOAD_MB` | `10240` (10 GiB) | No | Maximum single upload size in MB |
| `CLOUD_CORS_ORIGIN` | `*` | No | Allowed CORS origin |
| `CLOUD_ADMIN_USER` | (empty) | **Yes** | Bootstrap admin username (created on first start) |
| `CLOUD_ADMIN_PASS` | (empty) | **Yes** | Bootstrap admin password |

> `CLOUD_JWT_SECRET` has a random fallback. If unset, all tokens become invalid each time the server restarts — always set a fixed secret in production.

---

## Deploying on a Raspberry Pi (one-click)

No need to type commands on the Pi. Everything is done from your development machine.

### Prerequisites

- Raspberry Pi (3B+ or newer, Pi 5 ideal) running **Raspberry Pi OS** (64-bit) with **SSH enabled**.
- Your Pi must be reachable from your machine (same network, or via Tailscale/ZeroTier).
- Your development machine needs **Go 1.22+**, **Node 20+**, `ssh`, and `scp`.

---

### Quick deploy (recommended)

**Step 1: Configure**

Copy the example config file and edit it with your Pi details:

```bash
cp deploy/.env.pi.example deploy/.env.pi
nano deploy/.env.pi
```

Fill in your Pi's hostname/IP, SSH user, and the admin credentials you want:

```ini
PI_HOST=raspberrypi.local
PI_USER=pi
CLOUD_ADMIN_USER=admin
CLOUD_ADMIN_PASS=your-strong-password-here
# Leave empty to auto-generate
CLOUD_JWT_SECRET=
```

**Step 2: Deploy with one command**

**Linux / Mac:**

```bash
make deploy-pi-run
```

Or directly:

```bash
./deploy/deploy.sh
```

**Windows (PowerShell):**

```powershell
.\deploy\deploy.ps1
```

That's it. The script will:

1. Cross-compile the server for ARM64
2. Copy files to the Pi via SCP
3. SSH into the Pi and automatically:
   - Create the `cloud` system user
   - Set up directories (`/opt/my-personal-cloud`, `/mnt/cloud/storage`)
   - Generate a secure JWT secret (or use yours)
   - Write the secrets file at `/etc/my-personal-cloud.env`
   - Install and start the systemd service
   - Install Caddy and configure the reverse proxy
4. Print the URL to access your cloud: **`https://cloud.local`**

---

### Fully automated (no prompts)

Pass all values as arguments:

**Linux / Mac:**

```bash
./deploy/deploy.sh -h 192.168.1.100 -u admin -p MySecretPass
```

**Windows (PowerShell):**

```powershell
.\deploy\deploy.ps1 -PiHost 192.168.1.100 -AdminPass "MySecretPass"
```

All options:

| Flag (bash) | Parameter (PowerShell) | Description |
|---|---|---|
| `-h <host>` | `-PiHost` | Pi hostname or IP |
| `-u <user>` | `-PiUser` | SSH user (default: pi) |
| `-a <user>` | `-AdminUser` | Cloud admin username (default: admin) |
| `-p <pass>` | `-AdminPass` | Cloud admin password |
| `-j <secret>` | `-JwtSecret` | JWT signing secret (auto-generated if empty) |

---

### Manual deploy (step by step)

If you prefer to do things manually or the automated script doesn't fit your setup:

```bash
# 1. Cross-compile from your machine
make build-pi

# 2. Copy files to the Pi
scp my-personal-cloud-arm64 pi@raspberrypi.local:/tmp/
scp Caddyfile pi@raspberrypi.local:/tmp/
scp deploy/my-personal-cloud.service pi@raspberrypi.local:/tmp/

# 3. SSH into the Pi and run:
ssh pi@raspberrypi.local
```

Then on the Pi:

```bash
# Create the cloud user and directories
sudo useradd -r -s /usr/sbin/nologin cloud
sudo mkdir -p /opt/my-personal-cloud /mnt/cloud/storage

# Place the binary
sudo mv /tmp/my-personal-cloud-arm64 /opt/my-personal-cloud/my-personal-cloud
sudo chmod +x /opt/my-personal-cloud/my-personal-cloud
sudo chown -R cloud:cloud /opt/my-personal-cloud /mnt/cloud

# Create secrets file
sudo tee /etc/my-personal-cloud.env >/dev/null <<EOF
CLOUD_JWT_SECRET=$(openssl rand -hex 32)
CLOUD_ADMIN_USER=admin
CLOUD_ADMIN_PASS=your-strong-password-here
EOF
sudo chmod 600 /etc/my-personal-cloud.env

# Install systemd service
sudo mv /tmp/my-personal-cloud.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now my-personal-cloud

# Install Caddy
sudo apt update && sudo apt install -y caddy
sudo mv /tmp/Caddyfile /etc/caddy/Caddyfile
sudo systemctl restart caddy
```

---

### Post-deploy

After deployment, your cloud is accessible at **`https://cloud.local`** (self-signed certificate — your browser will show a warning, which is expected).

**Tips:**
- Add `cloud.local` to your router's DNS or your machine's `hosts` file so all devices can reach it by name.
- For remote access without opening ports, install **Tailscale** on the Pi and your devices.

**Mount external storage (recommended):**

```bash
# SSH into the Pi
sudo lsblk                          # Find your USB drive (e.g. sda1)
sudo mkfs.ext4 /dev/sda1            # Format it (REPLACE sda1!)
sudo blkid /dev/sda1                # Get the UUID
echo 'UUID=xxxx-xxxx  /mnt/cloud  ext4  defaults,noatime,nofail  0  2' | sudo tee -a /etc/fstab
sudo mount /mnt/cloud
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

**Quick update (via deploy script):**

```bash
git pull
./deploy/deploy.sh -h <pi-ip>     # Linux/Mac
.\deploy\deploy.ps1 -PiHost <ip>  # Windows
```

The script detects the existing installation and only updates the binary and restarts the service.

**Manual update:**

```bash
# On your development machine
git pull
make build-pi

# Copy to Pi
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
