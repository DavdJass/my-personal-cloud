package photos

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/disintegration/imaging"
	"github.com/go-chi/chi/v5"

	"github.com/DavdJass/my-personal-cloud/internal/auth"
	"github.com/DavdJass/my-personal-cloud/internal/logger"
	"github.com/DavdJass/my-personal-cloud/internal/storage"
)

// Photo is the gallery-friendly view of an image file.
type Photo struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	MimeType  string    `json:"mime_type"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

// Service exposes the read-only gallery endpoints. Thumbnails are generated
// lazily on first request and cached on disk under <store>/thumbs/<id>.jpg.
type Service struct {
	db        *sql.DB
	store     *storage.LocalStore
	thumbsDir string

	// inflight deduplicates concurrent thumbnail generation for the same id.
	inflight sync.Map // map[string]*sync.Mutex
}

// NewService builds the photo gallery service.
func NewService(db *sql.DB, store *storage.LocalStore, storageRoot string) (*Service, error) {
	thumbsDir := filepath.Join(storageRoot, "_thumbs")
	if err := os.MkdirAll(thumbsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create thumbs dir: %w", err)
	}
	return &Service{db: db, store: store, thumbsDir: thumbsDir}, nil
}

// ListHandler handles GET /api/photos?limit=50&offset=0 and returns every
// image owned by the authenticated user, newest first.
func (s *Service) ListHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	rows, err := s.db.QueryContext(r.Context(),
		`SELECT id, name, mime_type, size_bytes, created_at
		   FROM files
		  WHERE user_id = ? AND is_image = 1 AND deleted_at IS NULL
		  ORDER BY created_at DESC
		  LIMIT ? OFFSET ?`,
		user.ID, limit, offset,
	)
	if err != nil {
		logger.Error("list photos failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "list photos failed")
		return
	}
	defer rows.Close()

	out := []Photo{}
	for rows.Next() {
		var p Photo
		if err := rows.Scan(&p.ID, &p.Name, &p.MimeType, &p.SizeBytes, &p.CreatedAt); err != nil {
			logger.Error("scan photo failed", "error", err)
			writeJSONError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		out = append(out, p)
	}

	var total int
	_ = s.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM files WHERE user_id = ? AND is_image = 1 AND deleted_at IS NULL`,
		user.ID,
	).Scan(&total)

	writeJSON(w, http.StatusOK, map[string]any{
		"photos": out,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// ThumbHandler handles GET /api/photos/{id}/thumb?size=256 and returns a
// JPEG-encoded square thumbnail. Default size is 256px.
func (s *Service) ThumbHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := chi.URLParam(r, "id")

	size := 256
	if v := r.URL.Query().Get("size"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1024 {
			size = n
		}
	}

	storagePath, ok, err := s.lookupImage(r, user.ID, id)
	if err != nil {
		logger.Error("lookup image for thumb failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if !ok {
		writeJSONError(w, http.StatusNotFound, "photo not found")
		return
	}

	thumbPath := filepath.Join(s.thumbsDir, fmt.Sprintf("%s_%d.jpg", id, size))

	if _, err := os.Stat(thumbPath); errors.Is(err, os.ErrNotExist) {
		if err := s.generateThumb(id, storagePath, thumbPath, size); err != nil {
			logger.Error("generate thumbnail failed", "error", err, "photo_id", id)
			writeJSONError(w, http.StatusInternalServerError, "thumb generation failed: "+err.Error())
			return
		}
	}

	f, err := os.Open(thumbPath)
	if err != nil {
		logger.Error("open thumb file failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "open thumb failed")
		return
	}
	defer f.Close()

	stat, _ := f.Stat()
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	if stat != nil {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
	}
	_, _ = io.Copy(w, f)
}

// FullHandler handles GET /api/photos/{id}/full and streams the original
// image bytes (used by the lightbox in the gallery).
func (s *Service) FullHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := chi.URLParam(r, "id")

	var (
		storagePath string
		mime        string
		size        int64
	)
	err := s.db.QueryRowContext(r.Context(),
		`SELECT storage_path, mime_type, size_bytes
		   FROM files WHERE id = ? AND user_id = ? AND is_image = 1`,
		id, user.ID,
	).Scan(&storagePath, &mime, &size)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "photo not found")
		return
	}
	if err != nil {
		logger.Error("lookup photo for full failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	f, err := s.store.Open(storagePath)
	if err != nil {
		logger.Error("open photo file failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "open failed")
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", size))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = io.Copy(w, f)
}

func (s *Service) lookupImage(r *http.Request, userID int64, id string) (string, bool, error) {
	var storagePath string
	err := s.db.QueryRowContext(r.Context(),
		`SELECT storage_path FROM files
		  WHERE id = ? AND user_id = ? AND is_image = 1`,
		id, userID,
	).Scan(&storagePath)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return storagePath, true, nil
}

// generateThumb decodes the source image and writes a JPEG thumbnail at the
// requested square size. A per-id mutex avoids redundant work when many
// gallery tiles request the same thumbnail simultaneously.
func (s *Service) generateThumb(id, storagePath, thumbPath string, size int) error {
	muVal, _ := s.inflight.LoadOrStore(id, &sync.Mutex{})
	mu := muVal.(*sync.Mutex)
	mu.Lock()
	defer func() {
		mu.Unlock()
		s.inflight.Delete(id)
	}()

	// Re-check after acquiring the lock; another goroutine may have produced it.
	if _, err := os.Stat(thumbPath); err == nil {
		return nil
	}

	src, err := s.store.Open(storagePath)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer src.Close()

	img, _, err := image.Decode(src)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}

	// Fit-and-crop to a centered square at the requested size.
	thumb := imaging.Fill(img, size, size, imaging.Center, imaging.Lanczos)

	var buf bytes.Buffer
	if err := imaging.Encode(&buf, thumb, imaging.JPEG, imaging.JPEGQuality(82)); err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	tmp := thumbPath + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, thumbPath); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
