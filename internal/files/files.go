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

// File is the metadata record for a stored file exposed to API clients.
type File struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	ParentPath string    `json:"parent_path"`
	MimeType   string    `json:"mime_type"`
	SizeBytes  int64     `json:"size_bytes"`
	IsImage    bool      `json:"is_image"`
	CreatedAt  time.Time `json:"created_at"`
}

// Folder is a virtual directory; it has no physical counterpart on disk.
type Folder struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	ParentPath string    `json:"parent_path"`
	CreatedAt  time.Time `json:"created_at"`
}

// Service implements the file and folder CRUD endpoints.
type Service struct {
	db             *sql.DB
	store          *storage.LocalStore
	maxUploadBytes int64
}

// NewService wires the dependencies needed by the file handlers.
func NewService(db *sql.DB, store *storage.LocalStore, maxUploadBytes int64) *Service {
	return &Service{db: db, store: store, maxUploadBytes: maxUploadBytes}
}

// ── LIST ─────────────────────────────────────────────────────────────────────

// ListHandler handles GET /api/files?path=/foo and returns the folders and
// files inside the requested virtual directory for the authenticated user.
// Folders are always sorted before files; both are sorted by name.
func (s *Service) ListHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	parent := normalizeParent(r.URL.Query().Get("path"))

	folders, err := s.listFolders(r, user.ID, parent)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "list folders failed")
		return
	}

	rows, err := s.db.QueryContext(r.Context(),
		`SELECT id, name, parent_path, mime_type, size_bytes, is_image, created_at
		   FROM files
		  WHERE user_id = ? AND parent_path = ?
		  ORDER BY name COLLATE NOCASE`,
		user.ID, parent,
	)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "list files failed")
		return
	}
	defer rows.Close()

	outFiles := []File{}
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
		outFiles = append(outFiles, f)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"path":    parent,
		"folders": folders,
		"files":   outFiles,
	})
}

func (s *Service) listFolders(r *http.Request, userID int64, parent string) ([]Folder, error) {
	rows, err := s.db.QueryContext(r.Context(),
		`SELECT id, name, parent_path, created_at
		   FROM folders
		  WHERE user_id = ? AND parent_path = ?
		  ORDER BY name COLLATE NOCASE`,
		userID, parent,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Folder{}
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.ID, &f.Name, &f.ParentPath, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// ── UPLOAD ───────────────────────────────────────────────────────────────────

// UploadHandler handles POST /api/files/upload as multipart/form-data.
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

// ── DOWNLOAD ─────────────────────────────────────────────────────────────────

// DownloadHandler handles GET /api/files/{id}/download.
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
	_, _ = io.Copy(w, f)
}

// ── PATCH FILE ───────────────────────────────────────────────────────────────

// PatchFileHandler handles PATCH /api/files/{id} and supports rename and/or
// move. Both fields are optional; omitting one keeps the current value.
func (s *Service) PatchFileHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := chi.URLParam(r, "id")

	var body struct {
		Name       *string `json:"name"`
		ParentPath *string `json:"parent_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.Name == nil && body.ParentPath == nil {
		writeJSONError(w, http.StatusBadRequest, "provide at least name or parent_path")
		return
	}

	// Load current values.
	var cur File
	var isImage int
	err := s.db.QueryRowContext(r.Context(),
		`SELECT id, name, parent_path, mime_type, size_bytes, is_image, created_at
		   FROM files WHERE id = ? AND user_id = ?`,
		id, user.ID,
	).Scan(&cur.ID, &cur.Name, &cur.ParentPath, &cur.MimeType, &cur.SizeBytes, &isImage, &cur.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "file not found")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	cur.IsImage = isImage == 1

	newName := cur.Name
	newParent := cur.ParentPath
	if body.Name != nil {
		n := sanitizeFilename(*body.Name)
		if n == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid name")
			return
		}
		newName = n
	}
	if body.ParentPath != nil {
		newParent = normalizeParent(*body.ParentPath)
	}

	if _, err := s.db.ExecContext(r.Context(),
		`UPDATE files SET name = ?, parent_path = ? WHERE id = ? AND user_id = ?`,
		newName, newParent, id, user.ID,
	); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "update failed")
		return
	}

	cur.Name = newName
	cur.ParentPath = newParent
	writeJSON(w, http.StatusOK, cur)
}

// ── DELETE FILE ───────────────────────────────────────────────────────────────

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

// ── CREATE FOLDER ─────────────────────────────────────────────────────────────

// CreateFolderHandler handles POST /api/folders.
func (s *Service) CreateFolderHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body struct {
		Name       string `json:"name"`
		ParentPath string `json:"parent_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	name := sanitizeFilename(body.Name)
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid folder name")
		return
	}
	parent := normalizeParent(body.ParentPath)

	id := uuid.NewString()
	now := time.Now()
	_, err := s.db.ExecContext(r.Context(),
		`INSERT INTO folders (id, user_id, name, parent_path) VALUES (?, ?, ?, ?)`,
		id, user.ID, name, parent,
	)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "create folder failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, Folder{
		ID:         id,
		Name:       name,
		ParentPath: parent,
		CreatedAt:  now,
	})
}

// ── PATCH FOLDER ─────────────────────────────────────────────────────────────

// PatchFolderHandler handles PATCH /api/folders/{id}. It supports rename
// and/or move. Moving cascades the path update to all descendant files and
// folders by replacing the old full path prefix with the new one.
func (s *Service) PatchFolderHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := chi.URLParam(r, "id")

	var body struct {
		Name       *string `json:"name"`
		ParentPath *string `json:"parent_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.Name == nil && body.ParentPath == nil {
		writeJSONError(w, http.StatusBadRequest, "provide at least name or parent_path")
		return
	}

	var cur Folder
	err := s.db.QueryRowContext(r.Context(),
		`SELECT id, name, parent_path, created_at FROM folders WHERE id = ? AND user_id = ?`,
		id, user.ID,
	).Scan(&cur.ID, &cur.Name, &cur.ParentPath, &cur.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "folder not found")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	newName := cur.Name
	newParent := cur.ParentPath
	if body.Name != nil {
		n := sanitizeFilename(*body.Name)
		if n == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid folder name")
			return
		}
		newName = n
	}
	if body.ParentPath != nil {
		newParent = normalizeParent(*body.ParentPath)
	}

	oldFull := folderFullPath(cur.ParentPath, cur.Name)
	newFull := folderFullPath(newParent, newName)

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "begin tx failed")
		return
	}
	defer tx.Rollback() //nolint:errcheck

	// Update the folder row itself.
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE folders SET name = ?, parent_path = ? WHERE id = ? AND user_id = ?`,
		newName, newParent, id, user.ID,
	); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "update folder failed")
		return
	}

	if oldFull != newFull {
		// Cascade: update parent_path of all descendant files.
		// SUBSTR(parent_path, oldLen+1) strips the old prefix, then newFull is
		// prepended. Rows at exactly oldFull or below oldFull/ are matched.
		oldLen := len(oldFull)
		if _, err := tx.ExecContext(r.Context(),
			`UPDATE files
			    SET parent_path = ? || SUBSTR(parent_path, ?)
			  WHERE user_id = ? AND (parent_path = ? OR parent_path LIKE ?)`,
			newFull, oldLen+1, user.ID, oldFull, oldFull+"/%",
		); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "cascade files failed")
			return
		}

		// Cascade: update parent_path of all descendant folders.
		if _, err := tx.ExecContext(r.Context(),
			`UPDATE folders
			    SET parent_path = ? || SUBSTR(parent_path, ?)
			  WHERE user_id = ? AND (parent_path = ? OR parent_path LIKE ?)`,
			newFull, oldLen+1, user.ID, oldFull, oldFull+"/%",
		); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "cascade folders failed")
			return
		}
	}

	if err := tx.Commit(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "commit failed")
		return
	}

	cur.Name = newName
	cur.ParentPath = newParent
	writeJSON(w, http.StatusOK, cur)
}

// ── DELETE FOLDER ─────────────────────────────────────────────────────────────

// DeleteFolderHandler handles DELETE /api/folders/{id}. It recursively deletes
// all descendant files (and their physical data on disk) and subfolders.
func (s *Service) DeleteFolderHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := chi.URLParam(r, "id")

	var cur Folder
	err := s.db.QueryRowContext(r.Context(),
		`SELECT id, name, parent_path FROM folders WHERE id = ? AND user_id = ?`,
		id, user.ID,
	).Scan(&cur.ID, &cur.Name, &cur.ParentPath)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "folder not found")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	fullPath := folderFullPath(cur.ParentPath, cur.Name)

	// Collect storage paths to delete from disk after the DB transaction.
	rows, err := s.db.QueryContext(r.Context(),
		`SELECT storage_path FROM files
		  WHERE user_id = ? AND (parent_path = ? OR parent_path LIKE ?)`,
		user.ID, fullPath, fullPath+"/%",
	)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "list descendants failed")
		return
	}
	var storagePaths []string
	for rows.Next() {
		var sp string
		if err := rows.Scan(&sp); err == nil {
			storagePaths = append(storagePaths, sp)
		}
	}
	rows.Close()

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "begin tx failed")
		return
	}
	defer tx.Rollback() //nolint:errcheck

	// Delete all descendant files from metadata.
	if _, err := tx.ExecContext(r.Context(),
		`DELETE FROM files WHERE user_id = ? AND (parent_path = ? OR parent_path LIKE ?)`,
		user.ID, fullPath, fullPath+"/%",
	); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "delete descendant files failed")
		return
	}

	// Delete all descendant folders.
	if _, err := tx.ExecContext(r.Context(),
		`DELETE FROM folders WHERE user_id = ? AND (parent_path = ? OR parent_path LIKE ?)`,
		user.ID, fullPath, fullPath+"/%",
	); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "delete descendant folders failed")
		return
	}

	// Delete the folder itself.
	if _, err := tx.ExecContext(r.Context(),
		`DELETE FROM folders WHERE id = ? AND user_id = ?`,
		id, user.ID,
	); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "delete folder failed")
		return
	}

	if err := tx.Commit(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "commit failed")
		return
	}

	// Best-effort cleanup of physical files after the DB is consistent.
	for _, sp := range storagePaths {
		_ = s.store.Delete(sp)
	}

	w.WriteHeader(http.StatusNoContent)
}

// ── HELPERS ───────────────────────────────────────────────────────────────────

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

// folderFullPath returns the canonical virtual path of a folder given its
// parent and name. E.g. parent="/" name="docs" → "/docs".
func folderFullPath(parent, name string) string {
	if parent == "/" {
		return "/" + name
	}
	return parent + "/" + name
}

// normalizeParent ensures the parent path is in the canonical form "/foo/bar".
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
