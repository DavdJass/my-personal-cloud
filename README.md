# My Personal Cloud

A self-hosted personal cloud built in Go, lightweight and dependency-free,
designed to run on a **Raspberry Pi 5** and serve your files and photos through
a modern web interface.

- **Backend:** Go with `chi`, SQLite (no CGO), JWT auth and pure-Go thumbnails.
- **Frontend:** React + Vite + TypeScript, embedded in the Go binary.
- **Deploy:** single binary + `Caddy` (automatic HTTPS) + `Tailscale` (remote access without opening ports).

---

## Architecture

```
Browser  --HTTPS-->  Caddy  --HTTP-->  Go server (:8080)  -->  SQLite + disk
                       ^
                       |
                    Tailscale (remote access from outside the LAN)
```

---

## Repository layout

```
my-personal-cloud/
  cmd/server/           Server entrypoint
  internal/
    auth/               JWT login + middleware
    config/             Config loading from env vars
    db/                 SQLite and migrations
    files/              File and folder endpoints
    photos/             Gallery + thumbnail generation
    storage/            Local filesystem abstraction
  web/                  React frontend (build embedded in binary)
  deploy/               systemd unit file
  Caddyfile             Example reverse proxy config
  Makefile              Build shortcuts
```

---

## Local development

Requirements: **Go 1.22+** and **Node 20+**.

```bash
# 1. Install dependencies
go mod tidy
cd web && npm install && cd ..

# 2. Bootstrap an admin user
export CLOUD_ADMIN_USER=admin
export CLOUD_ADMIN_PASS=admin

# 3. (Optional) Set a stable JWT secret
export CLOUD_JWT_SECRET="change-this-in-production"

# 4. Start the backend (port 8080)
go run ./cmd/server
```

In a second terminal, start the frontend with hot-reload:

```bash
cd web
npm run dev   # http://localhost:5173 (proxies /api to :8080)
```

To build a single self-contained binary with the frontend embedded:

```bash
make build    # produces ./my-personal-cloud
./my-personal-cloud
# open http://localhost:8080
```

> **Windows note:** use `$env:CLOUD_ADMIN_USER = "admin"` instead of `export`.

---

## Environment variables

| Variable                | Default                   | Description                                          |
| ----------------------- | ------------------------- | ---------------------------------------------------- |
| `CLOUD_ADDR`            | `:8080`                   | TCP address the server listens on                    |
| `CLOUD_STORAGE_ROOT`    | `./data/storage`          | Directory where uploaded files are stored            |
| `CLOUD_DB_PATH`         | `./data/cloud.db`         | Path to the SQLite database file                     |
| `CLOUD_JWT_SECRET`      | (random on each startup)  | HMAC key used to sign JWTs — set this in production  |
| `CLOUD_JWT_EXPIRY_HOURS`| `24`                      | Token validity in hours                              |
| `CLOUD_MAX_UPLOAD_MB`   | `10240` (10 GiB)          | Maximum size accepted for a single upload            |
| `CLOUD_CORS_ORIGIN`     | `*`                       | Allowed CORS origin                                  |
| `CLOUD_ADMIN_USER`      | (empty)                   | If set together with `_PASS`, creates the user on startup |
| `CLOUD_ADMIN_PASS`      | (empty)                   | Password for the bootstrap admin account             |

---

## Deploying on Raspberry Pi 5

### 1. Cross-compile from your machine

```bash
make build-pi
# produces ./my-personal-cloud-arm64
```

### 2. Copy to the Pi

```bash
scp my-personal-cloud-arm64 pi@raspberrypi.local:/tmp/
scp Caddyfile deploy/my-personal-cloud.service pi@raspberrypi.local:/tmp/
```

### 3. Install on the Pi (via SSH)

```bash
sudo useradd -r -s /usr/sbin/nologin cloud
sudo mkdir -p /opt/my-personal-cloud /mnt/cloud/storage
sudo mv /tmp/my-personal-cloud-arm64 /opt/my-personal-cloud/my-personal-cloud
sudo chown -R cloud:cloud /opt/my-personal-cloud /mnt/cloud
sudo chmod +x /opt/my-personal-cloud/my-personal-cloud

# Secrets file
sudo tee /etc/my-personal-cloud.env >/dev/null <<EOF
CLOUD_JWT_SECRET=$(openssl rand -hex 32)
CLOUD_ADMIN_USER=your_username
CLOUD_ADMIN_PASS=a_strong_password
EOF
sudo chmod 600 /etc/my-personal-cloud.env

# Enable systemd service
sudo mv /tmp/my-personal-cloud.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now my-personal-cloud
sudo systemctl status my-personal-cloud
```

### 4. Caddy (automatic HTTPS)

```bash
sudo apt install -y caddy
sudo mv /tmp/Caddyfile /etc/caddy/Caddyfile
sudo systemctl restart caddy
```

By default the `Caddyfile` serves on `https://cloud.local` with a self-signed
certificate. For a public domain, uncomment the `cloud.example.com` block and
ensure ports 80/443 are forwarded on your router.

### 5. Remote access with Tailscale (recommended)

```bash
curl -fsSL https://tailscale.com/install.sh | sh
sudo tailscale up
sudo tailscale cert  # optional: issues a cert for <host>.<tailnet>.ts.net
```

Install Tailscale on your phone or laptop and access the Pi by its Tailscale IP
`100.x.y.z` or hostname `<host>.<tailnet>.ts.net`. **No router port-forwarding needed.**

---

## Storage

Recommended: mount a USB external drive at `/mnt/cloud` and leave the SD card
for the OS only. Example `/etc/fstab` entry:

```
UUID=xxxx-xxxx  /mnt/cloud  ext4  defaults,noatime,nofail  0  2
```

---

## REST API

All routes live under `/api`. Protected routes require an
`Authorization: Bearer <jwt>` header (or `?token=<jwt>` as a query parameter
for downloads and `<img>` tags).

| Method | Route                         | Description                       |
|--------|-------------------------------|-----------------------------------|
| GET    | `/api/health`                 | Health check                      |
| POST   | `/api/auth/login`             | Login — returns a JWT             |
| GET    | `/api/auth/me`                | Current authenticated user        |
| GET    | `/api/files?path=/`           | List files and folders            |
| POST   | `/api/files/upload`           | Upload file (multipart `file`)    |
| GET    | `/api/files/{id}/download`    | Download file                     |
| PATCH  | `/api/files/{id}`             | Rename / move file                |
| DELETE | `/api/files/{id}`             | Delete file                       |
| POST   | `/api/folders`                | Create folder                     |
| PATCH  | `/api/folders/{id}`           | Rename / move folder (cascades)   |
| DELETE | `/api/folders/{id}`           | Delete folder and all contents    |
| GET    | `/api/photos`                 | List photos                       |
| GET    | `/api/photos/{id}/thumb`      | JPEG thumbnail (param `size`)     |
| GET    | `/api/photos/{id}/full`       | Full-resolution image             |

Login example:

```bash
curl -s http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}'
```

---

## Roadmap

- Full multi-user management from the UI (not just env-var bootstrap).
- Public share links with optional expiration.
- Dropbox-style sync with a CLI client.
- Full-text search using SQLite FTS5.
- Login rate limiting to block brute-force attempts.

---

## License

MIT.
