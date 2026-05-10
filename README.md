# My Personal Cloud

Tu propia nube personal en Go, ligera y sin dependencias externas, diseñada
para correr en una **Raspberry Pi 5** y servir tus archivos y fotos a través
de una interfaz web moderna.

- Backend: **Go** con `chi`, SQLite (sin CGO), JWT y thumbnails en Go puro.
- Frontend: **React + Vite + TypeScript**, embebido en el binario Go.
- Despliegue: un solo binario + `Caddy` (HTTPS automático) + `Tailscale`
  (acceso remoto sin abrir puertos).

---

## Arquitectura

```
Navegador  --HTTPS-->  Caddy  --HTTP-->  Go server (:8080)  -->  SQLite + disco
                          ^
                          |
                       Tailscale (acceso remoto desde fuera de la LAN)
```

---

## Estructura del repositorio

```
my-personal-cloud/
  cmd/server/           Entrypoint del servidor
  internal/
    auth/               JWT + login + middleware
    config/             Carga de configuración
    db/                 SQLite y migraciones
    files/              Endpoints de archivos
    photos/             Galería + thumbnails
    storage/            Acceso al sistema de archivos
  web/                  Frontend React (build embebido en el binario)
  deploy/               Unit file de systemd
  Caddyfile             Reverse proxy de ejemplo
  Makefile              Atajos de build
```

---

## Desarrollo local

Requisitos: **Go 1.22+** y **Node 20+**.

```bash
# 1. Instalar dependencias
go mod tidy
cd web && npm install && cd ..

# 2. Crear un usuario admin de prueba
export CLOUD_ADMIN_USER=admin
export CLOUD_ADMIN_PASS=admin

# 3. (Opcional) Fijar un secreto JWT estable
export CLOUD_JWT_SECRET="cambia-esto-en-produccion"

# 4. Arrancar el backend (puerto 8080)
go run ./cmd/server
```

En otra terminal, arranca el frontend con hot-reload:

```bash
cd web
m   # http://localhost:5173 (proxy de /api a :8080)
```

Para construir un binario "todo en uno" con el frontend embebido:

```bash
make build    # produce ./my-personal-cloud
./my-personal-cloud
# abre http://localhost:8080
```

---

## Variables de entorno

| Variable                | Default                  | Descripción                                |
| ----------------------- | ------------------------ | ------------------------------------------ |
| `CLOUD_ADDR`            | `:8080`                  | Dirección/puerto del servidor              |
| `CLOUD_STORAGE_ROOT`    | `./data/storage`         | Directorio donde se guardan los archivos   |
| `CLOUD_DB_PATH`         | `./data/cloud.db`        | Ruta del archivo SQLite                    |
| `CLOUD_JWT_SECRET`      | (aleatorio al arrancar)  | Clave HMAC para firmar JWT (256 bits+)     |
| `CLOUD_JWT_EXPIRY_HOURS`| `24`                     | Horas de validez del token                 |
| `CLOUD_MAX_UPLOAD_MB`   | `10240` (10 GiB)         | Tamaño máximo de subida                    |
| `CLOUD_CORS_ORIGIN`     | `*`                      | Origen permitido por CORS                  |
| `CLOUD_ADMIN_USER`      | (vacío)                  | Si se define junto con `_PASS`, crea el usuario al arrancar |
| `CLOUD_ADMIN_PASS`      | (vacío)                  | Contraseña del admin de bootstrap          |

---

## Despliegue en Raspberry Pi 5

### 1. Compilación cruzada (desde tu máquina)

```bash
make build-pi
# Genera ./my-personal-cloud-arm64
```

### 2. Copia al RPi

```bash
scp my-personal-cloud-arm64 pi@raspberrypi.local:/tmp/
scp Caddyfile deploy/my-personal-cloud.service pi@raspberrypi.local:/tmp/
```

### 3. Instalación en el RPi (SSH)

```bash
sudo useradd -r -s /usr/sbin/nologin cloud
sudo mkdir -p /opt/my-personal-cloud /mnt/cloud/storage
sudo mv /tmp/my-personal-cloud-arm64 /opt/my-personal-cloud/my-personal-cloud
sudo chown -R cloud:cloud /opt/my-personal-cloud /mnt/cloud
sudo chmod +x /opt/my-personal-cloud/my-personal-cloud

# Variables de entorno (secretos)
sudo tee /etc/my-personal-cloud.env >/dev/null <<EOF
CLOUD_JWT_SECRET=$(openssl rand -hex 32)
CLOUD_ADMIN_USER=tu_usuario
CLOUD_ADMIN_PASS=una_password_fuerte
EOF
sudo chmod 600 /etc/my-personal-cloud.env

# Servicio systemd
sudo mv /tmp/my-personal-cloud.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now my-personal-cloud
sudo systemctl status my-personal-cloud
```

### 4. Caddy (HTTPS automático)

```bash
sudo apt install -y caddy
sudo mv /tmp/Caddyfile /etc/caddy/Caddyfile
sudo systemctl restart caddy
```

Por defecto el `Caddyfile` sirve en `https://cloud.local` con un certificado
auto-firmado. Para un dominio público edita el bloque `cloud.example.com` y
asegúrate de tener los puertos 80/443 abiertos.

### 5. Acceso remoto con Tailscale (recomendado)

```bash
curl -fsSL https://tailscale.com/install.sh | sh
sudo tailscale up
sudo tailscale cert  # opcional: emite cert para tu hostname.tailnet.ts.net
```

Después instala Tailscale en tu teléfono o portátil y accede al RPi por su
IP `100.x.y.z` o por su nombre `<host>.<tailnet>.ts.net`. **Sin abrir puertos
en el router.**

---

## Almacenamiento

Recomendado: monta un disco USB externo en `/mnt/cloud` y deja la SD card
solo para el sistema. Ejemplo de `/etc/fstab`:

```
UUID=xxxx-xxxx  /mnt/cloud  ext4  defaults,noatime,nofail  0  2
```

---

## API REST

Todas las rutas viven bajo `/api`. Las rutas protegidas requieren un header
`Authorization: Bearer <jwt>` (o `?token=<jwt>` para descargas e `<img>`).

| Método | Ruta                          | Descripción                  |
|--------|-------------------------------|------------------------------|
| GET    | `/api/health`                 | Health check                 |
| POST   | `/api/auth/login`             | Login → JWT                  |
| GET    | `/api/auth/me`                | Usuario actual               |
| GET    | `/api/files?path=/`           | Listar archivos              |
| POST   | `/api/files/upload`           | Subir (multipart `file`)     |
| GET    | `/api/files/{id}/download`    | Descargar                    |
| DELETE | `/api/files/{id}`             | Eliminar                     |
| GET    | `/api/photos`                 | Listar fotos                 |
| GET    | `/api/photos/{id}/thumb`      | Thumbnail JPEG (param `size`)|
| GET    | `/api/photos/{id}/full`       | Imagen original              |

Ejemplo de login:

```bash
curl -s http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}'
```

---

## Roadmap (ideas para extender)

- Soporte multi-usuario completo desde la UI (no solo bootstrap por env).
- Carpetas anidadas y mover/renombrar archivos.
- Compartir archivos vía link público temporal.
- Sincronización tipo Dropbox con un cliente CLI.
- Etiquetas y búsqueda full-text en SQLite (FTS5).

---

## Licencia

MIT.
