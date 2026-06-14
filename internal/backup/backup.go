// Package backup implements encrypted full-system backups.
//
// Backup file format:
//
//	[1 byte]   version  (0x01)
//	[1 byte]   key_type (0x00 = derived from JWT secret, 0x01 = user passkey)
//	[32 bytes] salt     (only present when key_type == 0x01; random per backup)
//	[12 bytes] nonce    (AES-GCM nonce)
//	[...]      ciphertext (encrypted ZIP archive)
//
// The plaintext ZIP contains:
//   - database.sqlite  → the full SQLite database
//   - storage/         → all user files (relative to the storage root)
//
// Default key derivation:  SHA-256("my-personal-cloud-backup" + JWT secret)
// Passkey derivation:      PBKDF2(passkey, salt, 4096 iterations, 32 bytes)
package backup

import (
	"archive/zip"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/pbkdf2"

	"github.com/DavdJass/my-personal-cloud/internal/auth"
	"github.com/DavdJass/my-personal-cloud/internal/storage"
)

const (
	backupVersion byte = 0x01
	keyDefault    byte = 0x00
	keyPasskey    byte = 0x01

	nonceSize  = 12
	saltSize   = 32
	aesKeySize = 32 // AES-256
)

var (
	errBadMagic = errors.New("invalid backup file")
	errBadKey   = errors.New("decryption failed — wrong passkey or corrupted backup")
)

// Service creates and restores encrypted backups.
// After a successful restore, the server should restart so all services
// pick up the new database connection. Set RestartCh to receive the signal.
type Service struct {
	dbh        *DBHolder
	store      *storage.LocalStore
	jwtSecret  []byte
	dbPath     string
	RestartCh  chan<- struct{}
}

// NewService creates a backup service.
func NewService(dbh *DBHolder, store *storage.LocalStore, jwtSecret []byte, dbPath string) *Service {
	return &Service{
		dbh:       dbh,
		store:     store,
		jwtSecret: jwtSecret,
		dbPath:    dbPath,
	}
}

// ── Key derivation ───────────────────────────────────────────────────────

// defaultKey derives the encryption key from the JWT secret.
func (s *Service) defaultKey() []byte {
	h := sha256.New()
	h.Write([]byte("my-personal-cloud-backup"))
	h.Write(s.jwtSecret)
	return h.Sum(nil)
}

// passkeyKey derives an encryption key from a user-supplied passkey and salt.
func passkeyKey(passkey string, salt []byte) []byte {
	return pbkdf2.Key([]byte(passkey), salt, 4096, aesKeySize, sha256.New)
}

// ── Backup creation ──────────────────────────────────────────────────────

// CreateBackup writes an encrypted backup ZIP to the given writer.
// If passkey is non-empty, the backup is encrypted with that passkey;
// otherwise it uses the server's default derived key.
func (s *Service) CreateBackup(w io.Writer, passkey string) error {
	// 1. Build the ZIP in memory.
	zipBuf := new(bytes.Buffer)
	if err := s.buildZip(zipBuf); err != nil {
		return fmt.Errorf("build zip: %w", err)
	}

	// 2. Determine key type and derive key.
	var keyType byte
	var key []byte
	var salt []byte

	if passkey != "" {
		keyType = keyPasskey
		salt = make([]byte, saltSize)
		if _, err := rand.Read(salt); err != nil {
			return fmt.Errorf("generate salt: %w", err)
		}
		key = passkeyKey(passkey, salt)
	} else {
		keyType = keyDefault
		key = s.defaultKey()
	}

	// 3. Encrypt with AES-256-GCM.
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("generate nonce: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("create gcm: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, zipBuf.Bytes(), nil)

	// 4. Write output: version + key_type + [salt] + nonce + ciphertext.
	if _, err := w.Write([]byte{backupVersion, keyType}); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if keyType == keyPasskey {
		if _, err := w.Write(salt); err != nil {
			return fmt.Errorf("write salt: %w", err)
		}
	}
	if _, err := w.Write(nonce); err != nil {
		return fmt.Errorf("write nonce: %w", err)
	}
	if _, err := w.Write(ciphertext); err != nil {
		return fmt.Errorf("write ciphertext: %w", err)
	}

	return nil
}

// buildZip creates a ZIP archive containing the database and all stored files.
func (s *Service) buildZip(w io.Writer) error {
	zw := zip.NewWriter(w)

	// Checkpoint WAL so the main db file has all committed data.
	if _, err := s.dbh.DB().Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		slog.Warn("wal checkpoint failed (backup may be incomplete)", "error", err)
	}

	// Add database.
	dbFile, err := os.Open(s.dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer dbFile.Close()

	dbZip, err := zw.Create("database.sqlite")
	if err != nil {
		return fmt.Errorf("create db zip entry: %w", err)
	}
	if _, err := io.Copy(dbZip, dbFile); err != nil {
		return fmt.Errorf("copy database: %w", err)
	}

	// Add storage files.
	root := s.store.Root()
	err = filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil // skip root
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		// Normalise to forward slashes for ZIP portability.
		zipName := filepath.ToSlash(filepath.Join("storage", rel))

		if fi.IsDir() {
			if _, err := zw.Create(zipName + "/"); err != nil {
				return fmt.Errorf("create dir zip entry: %w", err)
			}
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open %s: %w", path, err)
		}

		entry, err := zw.Create(zipName)
		if err != nil {
			f.Close()
			return fmt.Errorf("create file zip entry: %w", err)
		}
		if _, err := io.Copy(entry, f); err != nil {
			f.Close()
			return fmt.Errorf("copy %s: %w", zipName, err)
		}
		return f.Close()
	})
	if err != nil {
		return fmt.Errorf("walk storage: %w", err)
	}

	return zw.Close()
}

// ── Backup restore ───────────────────────────────────────────────────────

// RestoreBackup reads an encrypted backup, decrypts it, validates the
// contents, and extracts the database and storage files in place.
// If passkey is nil, the server attempts decryption with the default key.
// If passkey is non-nil (even empty string), the provided passkey is used.
func (s *Service) RestoreBackup(r io.Reader, passkey *string) error {
	backupData, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("read backup: %w", err)
	}

	// Parse header.
	if len(backupData) < 2 {
		return errBadMagic
	}
	if backupData[0] != backupVersion {
		return fmt.Errorf("unsupported backup version %d", backupData[0])
	}
	keyType := backupData[1]

	offset := 2
	var key []byte

	switch keyType {
	case keyDefault:
		if passkey != nil {
			slog.Warn("backup uses default key but passkey was provided; ignoring passkey")
		}
		slog.Info("backup encrypted with default key")
		key = s.defaultKey()

	case keyPasskey:
		if passkey == nil {
			return errors.New("backup requires a passkey — provide one to restore")
		}
		if len(backupData) < offset+saltSize {
			return errBadMagic
		}
		salt := backupData[offset : offset+saltSize]
		offset += saltSize
		key = passkeyKey(*passkey, salt)
		slog.Info("backup encrypted with passkey")

	default:
		return fmt.Errorf("unknown key type %d", keyType)
	}

	// Read nonce.
	if len(backupData) < offset+nonceSize {
		return errBadMagic
	}
	nonce := backupData[offset : offset+nonceSize]
	offset += nonceSize

	// Decrypt.
	ciphertext := backupData[offset:]
	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("create gcm: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return errBadKey
	}

	// Validate it's a ZIP.
	zipReader, err := zip.NewReader(bytes.NewReader(plaintext), int64(len(plaintext)))
	if err != nil {
		return fmt.Errorf("invalid backup archive: %w", err)
	}

	// Verify essential entries exist.
	hasDB := false
	for _, f := range zipReader.File {
		if f.Name == "database.sqlite" {
			hasDB = true
			break
		}
	}
	if !hasDB {
		return errors.New("backup is missing database.sqlite")
	}

	// Extract to temp directory.
	tmpDir, err := os.MkdirTemp("", "cloud-restore-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	for _, f := range zipReader.File {
		destPath := filepath.Join(tmpDir, f.Name)

		if strings.HasSuffix(f.Name, "/") {
			if err := os.MkdirAll(destPath, 0o755); err != nil {
				return fmt.Errorf("mkdir %s: %w", destPath, err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return fmt.Errorf("mkdir parent %s: %w", destPath, err)
		}

		src, err := f.Open()
		if err != nil {
			return fmt.Errorf("open zip entry %s: %w", f.Name, err)
		}

		dst, err := os.Create(destPath)
		if err != nil {
			src.Close()
			return fmt.Errorf("create %s: %w", destPath, err)
		}

		if _, err := io.Copy(dst, src); err != nil {
			src.Close()
			dst.Close()
			return fmt.Errorf("extract %s: %w", destPath, err)
		}
		src.Close()
		dst.Close()
	}

	// Validate extracted database.
	if err := validateSQLite(filepath.Join(tmpDir, "database.sqlite")); err != nil {
		return fmt.Errorf("database validation failed: %w", err)
	}

	// ── Apply restore ──

	// 1. Copy restored DB file first.
	if err := copyFile(filepath.Join(tmpDir, "database.sqlite"), s.dbPath); err != nil {
		return fmt.Errorf("replace database file: %w", err)
	}

	// 2. Close current DB and reopen with the restored file.
	if _, err := s.dbh.Replace(s.dbPath); err != nil {
		return fmt.Errorf("reopen database: %w", err)
	}

	// 3. Copy storage files.
	storageRoot := s.store.Root()
	srcStorage := filepath.Join(tmpDir, "storage")
	if fi, err := os.Stat(srcStorage); err == nil && fi.IsDir() {
		if err := copyDir(srcStorage, storageRoot); err != nil {
			return fmt.Errorf("restore storage: %w", err)
		}
	}

	slog.Info("backup restored successfully",
		"db", s.dbPath,
		"storage", storageRoot,
	)
	return nil
}

// ── Helpers ──────────────────────────────────────────────────────────────

func validateSQLite(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	magic := make([]byte, 16)
	if _, err := io.ReadFull(f, magic); err != nil {
		return fmt.Errorf("read header: %w", err)
	}
	if string(magic) != "SQLite format 3\x00" {
		return errors.New("not a valid SQLite database")
	}
	return nil
}

func reopenSQLite(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open restored db: %w", err)
	}
	conn.SetMaxOpenConns(1)
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ping restored db: %w", err)
	}
	return conn, nil
}

func copyFile(src, dst string) error {
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	d, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer d.Close()

	if _, err := io.Copy(d, s); err != nil {
		return err
	}
	return d.Sync()
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(dst, rel)

		if fi.IsDir() {
			return os.MkdirAll(dest, fi.Mode())
		}

		return copyFile(path, dest)
	})
}

// ── HTTP handlers ────────────────────────────────────────────────────────

// CreateHandler handles POST /api/backup/create.
// JSON body: { "passkey": "..." } (optional; empty or omitted = default key).
// Returns the encrypted backup file as a download.
func (s *Service) CreateHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body struct {
		Passkey string `json:"passkey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		body.Passkey = ""
	}

	if body.Passkey != "" && len(body.Passkey) < 4 {
		writeJSONError(w, http.StatusBadRequest, "passkey must be at least 4 characters")
		return
	}

	slog.Info("creating backup",
		"user", user.Username,
		"use_passkey", body.Passkey != "",
	)

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=my-cloud-backup.mpcbackup")

	if err := s.CreateBackup(w, body.Passkey); err != nil {
		slog.Error("backup creation failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "backup failed")
		return
	}
}

// RestoreHandler handles POST /api/backup/restore.
// Multipart form: "file" (the .mpcbackup file) + "passkey" (optional).
func (s *Service) RestoreHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 10<<30)

	if err := r.ParseMultipartForm(100 << 20); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid upload")
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "file field required")
		return
	}
	defer file.Close()

	passkeyStr := r.FormValue("passkey")
	var passkey *string
	if passkeyStr != "" {
		passkey = &passkeyStr
	}

	slog.Warn("restoring backup — server DB will be replaced",
		"user", user.Username,
		"has_passkey", passkey != nil,
	)

	if err := s.RestoreBackup(file, passkey); err != nil {
		slog.Error("backup restore failed", "error", err)
		if errors.Is(err, errBadKey) {
			writeJSONError(w, http.StatusUnauthorized, "wrong passkey or corrupted backup")
			return
		}
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Backup restored. The server will restart to reload the database.",
	})

	// Signal the main goroutine to restart the server so all services
	// pick up the new database connection.
	if s.RestartCh != nil {
		go func() {
			slog.Info("backup restore complete — restarting server in 500ms")
			time.Sleep(500 * time.Millisecond)
			s.RestartCh <- struct{}{}
		}()
	} else {
		slog.Warn("no restart channel set — manual restart required")
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
