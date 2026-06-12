package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Config holds the runtime configuration for the personal cloud server.
// Values are loaded from environment variables with sensible defaults so the
// server can be started with zero configuration on a fresh Raspberry Pi.
type Config struct {
	// Addr is the TCP address the HTTP server listens on (e.g. ":8080").
	Addr string

	// StorageRoot is the directory on disk where user files are persisted.
	// On a Raspberry Pi this typically points to a mounted external drive
	// such as "/mnt/cloud".
	StorageRoot string

	// DatabasePath is the path to the SQLite metadata database file.
	DatabasePath string

	// JWTSecret is the HMAC signing key for JWT tokens. If unset, a random
	// 32-byte secret is generated at startup (tokens won't survive restarts).
	JWTSecret []byte

	// JWTExpiry is how long an issued JWT remains valid.
	JWTExpiry time.Duration

	// MaxUploadBytes is the maximum size in bytes accepted for a single
	// upload. Defaults to 500 MB.
	MaxUploadBytes int64

	// AllowOrigin is the CORS origin allowed by the API.
	// In production, set this to your actual domain (e.g. "https://cloud.example.com").
	// The wildcard "*" is only safe because we do not send credentials (cookies);
	// authentication is done via the Authorization header. However, setting a
	// specific origin is strongly recommended for production.
	AllowOrigin string
}

// Load reads configuration from environment variables and applies defaults.
func Load() (*Config, error) {
	cfg := &Config{
		Addr:           env("CLOUD_ADDR", ":8080"),
		StorageRoot:    env("CLOUD_STORAGE_ROOT", filepath.Join(".", "data", "storage")),
		DatabasePath:   env("CLOUD_DB_PATH", filepath.Join(".", "data", "cloud.db")),
		JWTExpiry:      24 * time.Hour,
		MaxUploadBytes: 500 << 20, // 500 MB
		AllowOrigin:    env("CLOUD_CORS_ORIGIN", "*"),
	}

	if v := os.Getenv("CLOUD_JWT_EXPIRY_HOURS"); v != "" {
		hours, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid CLOUD_JWT_EXPIRY_HOURS: %w", err)
		}
		cfg.JWTExpiry = time.Duration(hours) * time.Hour
	}

	if v := os.Getenv("CLOUD_MAX_UPLOAD_MB"); v != "" {
		mb, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid CLOUD_MAX_UPLOAD_MB: %w", err)
		}
		cfg.MaxUploadBytes = mb * 1024 * 1024
	}

	if secret := os.Getenv("CLOUD_JWT_SECRET"); secret != "" {
		cfg.JWTSecret = []byte(secret)
	} else {
		// Random fallback prevents accidental insecure defaults but invalidates
		// tokens across restarts; operators should set CLOUD_JWT_SECRET in prod.
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			return nil, fmt.Errorf("generate random JWT secret: %w", err)
		}
		cfg.JWTSecret = []byte(hex.EncodeToString(buf))
	}

	if err := os.MkdirAll(cfg.StorageRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create storage root: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DatabasePath), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	return cfg, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
