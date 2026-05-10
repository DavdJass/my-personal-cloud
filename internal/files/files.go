package files

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/DavdJass/my-personal-cloud/internal/auth"
	"github.com/DavdJass/my-personal-cloud/internal/storage"
)

// File is the metadata record exposed to API clients.
type File struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	ParentPath string    `json:"parent_path"`
	MimeType   string    `json:"mime_type"`
	SizeBytes  int64     `json:"size_bytes"`
	IsImage    bool      `json:"is_image"`
	CreatedAt  time.Time `json:"created_at"`
}

// Service implements the file CRUD endpoints. It owns no goroutines and is
// safe for concurrent use as long as the underlying *sql.DB is.
type Service struct {
	db             *sql.DB
	store          *storage.LocalStore
	maxUploadBytes int64
}

// NewService wires the dependencies needed by the file handlers.
func NewService(db *sql.DB, store *storage.LocalStore, maxUploadBytes int64) *Service {
	return &Service{db: db, store: store, maxUploadBytes: maxUploadBytes}
}

// ListHandler handles GET /api/files?path=/foo and returns the contents of
// the requested virtual directory for the authenticated user.
func (s *Service) ListHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	parent := normalizeParent(r.URL.Query().Get("path"))

	rows, err := s.db.QueryContext(r.Context(),
		`SELECT id, name, parent_path, mime_type, size_bytes, is_image, created_at
		   FROM files
		  WHERE user_id = ? AND parent_path = ?
		  ORDER BY name COLLATE NOCASE`,
		user.ID, parent,
	)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "list failed")
		return
	}
	defer rows.Close()

	out := []File{}
	for rows.Next() {
		var (
			f       File
			isImage int
		)
		if err := rows.Scan(&f.ID, &f.Name, &f.ParentPath, &f.MimeType, &f.SizeBytes, &isImage, &f.CreatedAt); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		f.IsImage = isImage == 1
		out = append(out, f)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"path":  parent,
		"files": out,
	})
}

// UploadHandler handles POST /api/files/upload as multipart/form-data with a
// "file" field and an optional "path" form field for the destination folder.
func (s *Service) UploadHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, s.maxUploadBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid multipart payload: "+err.Error())
		return
	}

	parent := normalizeParent(r.FormValue("path"))

	src, header, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "missing 'file' field")
		return
	}
	defer src.Close()

	name := sanitizeFilename(header.Filename)
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid filename")
		return
	}

	mime := header.Header.Get("Content-Type")
	if mime == "" {
		mime = "application/octet-stream"
	}

	relPath, size, err := s.store.Save(user.ID, src)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "save failed: "+err.Error())
		return
	}

	id := uuid.NewString()
	isImage := isImageMime(mime)
	_, err = s.db.ExecContext(r.Context(),
		`INSERT INTO files (id, user_id, name, parent_path, storage_path, mime_type, size_bytes, is_image)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, user.ID, name, parent, relPath, mime, size, boolInt(isImage),
	)
	if err != nil {
		_ = s.store.Delete(relPath)
		writeJSONError(w, http.StatusInternalServerError, "insert metadata: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, File{
		ID:         id,
		Name:       name,
		ParentPath: parent,
		MimeType:   mime,
		SizeBytes:  size,
		IsImage:    isImage,
		CreatedAt:  time.Now(),
	})
}

// DownloadHandler handles GET /api/files/{id}/download and streams the file
// bytes to the client. The Authorization header may be passed as a "token"
// query parameter so plain <a href> downloads also work from the browser.
func (s *Service) DownloadHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := chi.URLParam(r, "id")

	meta, err := s.lookup(r, user.ID, id)
	if err != nil {
		respondLookupError(w, err)
		return
	}

	f, err := s.store.Open(meta.storagePath)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "open failed")
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", meta.mime)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", meta.size))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, meta.name))
	if _, err := io.Copy(w, f); err != nil {
		// Connection likely closed; nothing useful to return at this point.
		return
	}
}

// DeleteHandler handles DELETE /api/files/{id}.
func (s *Service) DeleteHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := chi.URLParam(r, "id")

	meta, err := s.lookup(r, user.ID, id)
	if err != nil {
		respondLookupError(w, err)
		return
	}

	if _, err := s.db.ExecContext(r.Context(),
		`DELETE FROM files WHERE id = ? AND user_id = ?`,
		id, user.ID,
	); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "delete metadata failed")
		return
	}
	_ = s.store.Delete(meta.storagePath)

	w.WriteHeader(http.StatusNoContent)
}

type fileMeta struct {
	name        string
	mime        string
	size        int64
	storagePath string
	isImage     bool
}

func (s *Service) lookup(r *http.Request, userID int64, id string) (fileMeta, error) {
	var (
		m       fileMeta
		isImage int
	)
	err := s.db.QueryRowContext(r.Context(),
		`SELECT name, mime_type, size_bytes, storage_path, is_image
		   FROM files WHERE id = ? AND user_id = ?`,
		id, userID,
	).Scan(&m.name, &m.mime, &m.size, &m.storagePath, &isImage)
	if errors.Is(err, sql.ErrNoRows) {
		return m, errNotFound
	}
	m.isImage = isImage == 1
	return m, err
}

var errNotFound = errors.New("not found")

func respondLookupError(w http.ResponseWriter, err error) {
	if errors.Is(err, errNotFound) {
		writeJSONError(w, http.StatusNotFound, "file not found")
		return
	}
	writeJSONError(w, http.StatusInternalServerError, "lookup failed")
}

// normalizeParent ensures the parent path is in the canonical form "/foo/bar"
// (no trailing slash unless root, always leading slash, no ".." segments).
func normalizeParent(p string) string {
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	cleaned := path.Clean(p)
	if cleaned == "." {
		return "/"
	}
	return cleaned
}

func sanitizeFilename(name string) string {
	// Strip any directory components clients may have included; we only want
	// the base filename and explicitly disallow path separators.
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	name = strings.TrimSpace(name)
	if name == "." || name == ".." || name == "/" {
		return ""
	}
	return name
}

func isImageMime(mime string) bool {
	return strings.HasPrefix(strings.ToLower(mime), "image/")
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
