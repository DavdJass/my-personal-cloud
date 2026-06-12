package files

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/DavdJass/my-personal-cloud/internal/auth"
	"github.com/DavdJass/my-personal-cloud/internal/mime"
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
	DeletedAt  *string   `json:"deleted_at,omitempty"`
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
	logger         *slog.Logger
}

// NewService wires the dependencies needed by the file handlers.
func NewService(db *sql.DB, store *storage.LocalStore, maxUploadBytes int64) *Service {
	return &Service{db: db, store: store, maxUploadBytes: maxUploadBytes, logger: slog.Default()}
}

// ── LIST (with pagination) ─────────────────────────────────────────────────────

// ListHandler handles GET /api/files?path=/foo&limit=50&offset=0.
func (s *Service) ListHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	parent := normalizeParent(r.URL.Query().Get("path"))
	limit, offset := parsePagination(r)

	folders, err := s.listFolders(r, user.ID, parent, limit, offset)
	if err != nil {
		s.logger.Error("list folders failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "list folders failed")
		return
	}

	rows, err := s.db.QueryContext(r.Context(),
		`SELECT id, name, parent_path, mime_type, size_bytes, is_image, created_at
		   FROM files
		  WHERE user_id = ? AND parent_path = ? AND deleted_at IS NULL
		  ORDER BY name COLLATE NOCASE
		  LIMIT ? OFFSET ?`,
		user.ID, parent, limit, offset,
	)
	if err != nil {
		s.logger.Error("list files failed", "error", err)
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
			s.logger.Error("scan file failed", "error", err)
			writeJSONError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		f.IsImage = isImage == 1
		outFiles = append(outFiles, f)
	}

	// Total count for pagination.
	var total int
	_ = s.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM files WHERE user_id = ? AND parent_path = ? AND deleted_at IS NULL`,
		user.ID, parent,
	).Scan(&total)

	writeJSON(w, http.StatusOK, map[string]any{
		"path":    parent,
		"folders": folders,
		"files":   outFiles,
		"total":   total,
		"limit":   limit,
		"offset":  offset,
	})
}

func (s *Service) listFolders(r *http.Request, userID int64, parent string, limit, offset int) ([]Folder, error) {
	rows, err := s.db.QueryContext(r.Context(),
		`SELECT id, name, parent_path, created_at
		   FROM folders
		  WHERE user_id = ? AND parent_path = ?
		  ORDER BY name COLLATE NOCASE
		  LIMIT ? OFFSET ?`,
		userID, parent, limit, offset,
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

// ── SEARCH ────────────────────────────────────────────────────────────────────

// SearchHandler handles GET /api/files/search?q=term&limit=50&offset=0.
func (s *Service) SearchHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSONError(w, http.StatusBadRequest, "query parameter 'q' is required")
		return
	}
	limit, offset := parsePagination(r)
	pattern := "%" + q + "%"

	// Search files.
	rows, err := s.db.QueryContext(r.Context(),
		`SELECT id, name, parent_path, mime_type, size_bytes, is_image, created_at
		   FROM files
		  WHERE user_id = ? AND deleted_at IS NULL AND name LIKE ?
		  ORDER BY CASE WHEN name LIKE ? THEN 0 ELSE 1 END, name COLLATE NOCASE
		  LIMIT ? OFFSET ?`,
		user.ID, pattern, q, limit, offset,
	)
	if err != nil {
		s.logger.Error("search files failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "search failed")
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
			s.logger.Error("scan search result failed", "error", err)
			writeJSONError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		f.IsImage = isImage == 1
		outFiles = append(outFiles, f)
	}

	// Search folders.
	fRows, err := s.db.QueryContext(r.Context(),
		`SELECT id, name, parent_path, created_at
		   FROM folders
		  WHERE user_id = ? AND name LIKE ?
		  ORDER BY CASE WHEN name LIKE ? THEN 0 ELSE 1 END, name COLLATE NOCASE
		  LIMIT ? OFFSET ?`,
		user.ID, pattern, q, limit, offset,
	)
	if err != nil {
		s.logger.Error("search folders failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "search failed")
		return
	}
	defer fRows.Close()

	outFolders := []Folder{}
	for fRows.Next() {
		var f Folder
		if err := fRows.Scan(&f.ID, &f.Name, &f.ParentPath, &f.CreatedAt); err != nil {
			s.logger.Error("scan folder search result failed", "error", err)
			writeJSONError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		outFolders = append(outFolders, f)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"files":   outFiles,
		"folders": outFolders,
		"limit":   limit,
		"offset":  offset,
	})
}

// ── UPLOAD (with duplicate detection) ─────────────────────────────────────────

// UploadHandler handles POST /api/files/upload as multipart/form-data.
func (s *Service) UploadHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, s.maxUploadBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid multipart payload")
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

	// Detect actual MIME from magic bytes.
	var sniffBuf [mime.SniffSize]byte
	n, _ := io.ReadFull(src, sniffBuf[:])
	mimeVal := mime.DetectFromBytes(sniffBuf[:n])
	if mimeVal == "" {
		mimeVal = "application/octet-stream"
	}

	// Combine sniff buffer plus remaining data for the store.
	combinedReader := io.MultiReader(bytes.NewReader(sniffBuf[:n]), src)

	// Check for duplicate.
	var existingID string
	err = s.db.QueryRowContext(r.Context(),
		`SELECT id FROM files WHERE user_id = ? AND parent_path = ? AND name = ? AND deleted_at IS NULL`,
		user.ID, parent, name,
	).Scan(&existingID)
	if err == nil {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":  "A file with this name already exists in the target folder",
			"existing_file_id": existingID,
		})
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		s.logger.Error("duplicate check failed", "error", err)
	}

	relPath, size, err := s.store.Save(user.ID, combinedReader)
	if err != nil {
		s.logger.Error("save file failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "save failed")
		return
	}

	id := uuid.NewString()
	isImg := mime.IsImage(mimeVal)
	_, err = s.db.ExecContext(r.Context(),
		`INSERT INTO files (id, user_id, name, parent_path, storage_path, mime_type, size_bytes, is_image)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, user.ID, name, parent, relPath, mimeVal, size, boolInt(isImg),
	)
	if err != nil {
		_ = s.store.Delete(relPath)
		s.logger.Error("insert file metadata failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "insert metadata failed")
		return
	}

	s.logger.Info("file uploaded", "user_id", user.ID, "name", name, "parent", parent, "size", size)

	writeJSON(w, http.StatusCreated, File{
		ID:         id,
		Name:       name,
		ParentPath: parent,
		MimeType:   mimeVal,
		SizeBytes:  size,
		IsImage:    isImg,
		CreatedAt:  time.Now(),
	})
}

// ── DOWNLOAD ───────────────────────────────────────────────────────────────────

// DownloadHandler handles GET /api/files/{id}/download.
// When ?inline=1 is set, the Content-Disposition header is omitted so the
// browser plays the content inline (useful for video/audio previews).
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
		s.logger.Error("open file for download failed", "error", err, "storage_path", meta.storagePath)
		writeJSONError(w, http.StatusInternalServerError, "open failed")
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", meta.mime)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", meta.size))

	// When inline=1 is passed, omit Content-Disposition so the browser shows
	// the content inline (playable video, viewable PDF, etc.).
	if r.URL.Query().Get("inline") != "1" {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, meta.name))
	}

	_, _ = io.Copy(w, f)
}

// ── PATCH FILE ───────────────────────────────────────────────────────────────

// PatchFileHandler handles PATCH /api/files/{id}. Supports rename and/or
// move. Both fields are optional. Checks for duplicates on rename/move.
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

	var cur File
	var isImage int
	err := s.db.QueryRowContext(r.Context(),
		`SELECT id, name, parent_path, mime_type, size_bytes, is_image, created_at
		   FROM files WHERE id = ? AND user_id = ? AND deleted_at IS NULL`,
		id, user.ID,
	).Scan(&cur.ID, &cur.Name, &cur.ParentPath, &cur.MimeType, &cur.SizeBytes, &isImage, &cur.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "file not found")
		return
	}
	if err != nil {
		s.logger.Error("lookup file for patch failed", "error", err)
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

	// Duplicate check: if name or parent changed, check for conflict.
	if newName != cur.Name || newParent != cur.ParentPath {
		var dupID string
		err := s.db.QueryRowContext(r.Context(),
			`SELECT id FROM files WHERE user_id = ? AND parent_path = ? AND name = ? AND id != ? AND deleted_at IS NULL`,
			user.ID, newParent, newName, id,
		).Scan(&dupID)
		if err == nil {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":  "A file with this name already exists in the target folder",
				"existing_file_id": dupID,
			})
			return
		}
		if !errors.Is(err, sql.ErrNoRows) {
			s.logger.Error("duplicate check on patch failed", "error", err)
		}
	}

	if _, err := s.db.ExecContext(r.Context(),
		`UPDATE files SET name = ?, parent_path = ? WHERE id = ? AND user_id = ?`,
		newName, newParent, id, user.ID,
	); err != nil {
		s.logger.Error("update file failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "update failed")
		return
	}

	cur.Name = newName
	cur.ParentPath = newParent
	writeJSON(w, http.StatusOK, cur)
}

// ── SOFT DELETE / RESTORE ─────────────────────────────────────────────────────

// DeleteHandler handles DELETE /api/files/{id}. Soft-deletes (sets deleted_at).
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

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(r.Context(),
		`UPDATE files SET deleted_at = ? WHERE id = ? AND user_id = ?`,
		now, id, user.ID,
	); err != nil {
		s.logger.Error("soft-delete file failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "delete failed")
		return
	}

	_ = s.store.Delete(meta.storagePath)

	s.logger.Info("file soft-deleted", "user_id", user.ID, "file_id", id, "name", meta.name)
	w.WriteHeader(http.StatusNoContent)
}

// RestoreHandler handles POST /api/files/{id}/restore.
func (s *Service) RestoreHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := chi.URLParam(r, "id")

	result, err := s.db.ExecContext(r.Context(),
		`UPDATE files SET deleted_at = NULL WHERE id = ? AND user_id = ? AND deleted_at IS NOT NULL`,
		id, user.ID,
	)
	if err != nil {
		s.logger.Error("restore file failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "restore failed")
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		writeJSONError(w, http.StatusNotFound, "file not found in trash")
		return
	}

	s.logger.Info("file restored", "user_id", user.ID, "file_id", id)
	w.WriteHeader(http.StatusNoContent)
}

// ── TRASH LISTING ─────────────────────────────────────────────────────────────

// TrashHandler handles GET /api/trash and returns all soft-deleted files.
func (s *Service) TrashHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	limit, offset := parsePagination(r)

	rows, err := s.db.QueryContext(r.Context(),
		`SELECT id, name, parent_path, mime_type, size_bytes, is_image, created_at, deleted_at
		   FROM files
		  WHERE user_id = ? AND deleted_at IS NOT NULL
		  ORDER BY deleted_at DESC
		  LIMIT ? OFFSET ?`,
		user.ID, limit, offset,
	)
	if err != nil {
		s.logger.Error("list trash failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "list trash failed")
		return
	}
	defer rows.Close()

	out := []File{}
	for rows.Next() {
		var (
			f        File
			isImage  int
			deleted  sql.NullString
		)
		if err := rows.Scan(&f.ID, &f.Name, &f.ParentPath, &f.MimeType, &f.SizeBytes, &isImage, &f.CreatedAt, &deleted); err != nil {
			s.logger.Error("scan trash file failed", "error", err)
			writeJSONError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		f.IsImage = isImage == 1
		if deleted.Valid {
			f.DeletedAt = &deleted.String
		}
		out = append(out, f)
	}

	var total int
	_ = s.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM files WHERE user_id = ? AND deleted_at IS NOT NULL`,
		user.ID,
	).Scan(&total)

	writeJSON(w, http.StatusOK, map[string]any{
		"files":  out,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// EmptyTrashHandler handles POST /api/trash/empty. Permanently deletes all
// soft-deleted files for the user.
func (s *Service) EmptyTrashHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Collect storage paths before deleting metadata.
	rows, err := s.db.QueryContext(r.Context(),
		`SELECT storage_path FROM files WHERE user_id = ? AND deleted_at IS NOT NULL`,
		user.ID,
	)
	if err != nil {
		s.logger.Error("list trashed files for empty failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "empty trash failed")
		return
	}
	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err == nil {
			paths = append(paths, p)
		}
	}
	rows.Close()

	if _, err := s.db.ExecContext(r.Context(),
		`DELETE FROM files WHERE user_id = ? AND deleted_at IS NOT NULL`,
		user.ID,
	); err != nil {
		s.logger.Error("empty trash delete failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "empty trash failed")
		return
	}

	for _, p := range paths {
		_ = s.store.Delete(p)
	}

	s.logger.Info("trash emptied", "user_id", user.ID, "files_count", len(paths))
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

	// Duplicate check for folder.
	var existingID string
	err := s.db.QueryRowContext(r.Context(),
		`SELECT id FROM folders WHERE user_id = ? AND parent_path = ? AND name = ?`,
		user.ID, parent, name,
	).Scan(&existingID)
	if err == nil {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "A folder with this name already exists in the target path",
			"existing_folder_id": existingID,
		})
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		s.logger.Error("duplicate folder check failed", "error", err)
	}

	id := uuid.NewString()
	now := time.Now()
	_, err = s.db.ExecContext(r.Context(),
		`INSERT INTO folders (id, user_id, name, parent_path) VALUES (?, ?, ?, ?)`,
		id, user.ID, name, parent,
	)
	if err != nil {
		s.logger.Error("create folder failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "create folder failed")
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
		s.logger.Error("lookup folder for patch failed", "error", err)
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
		s.logger.Error("begin tx for folder patch failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "begin tx failed")
		return
	}
	defer tx.Rollback() //nolint:errcheck

	// Update the folder row itself.
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE folders SET name = ?, parent_path = ? WHERE id = ? AND user_id = ?`,
		newName, newParent, id, user.ID,
	); err != nil {
		s.logger.Error("update folder failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "update folder failed")
		return
	}

	if oldFull != newFull {
		oldLen := len(oldFull)
		if _, err := tx.ExecContext(r.Context(),
			`UPDATE files
			    SET parent_path = ? || SUBSTR(parent_path, ?)
			  WHERE user_id = ? AND (parent_path = ? OR parent_path LIKE ?)`,
			newFull, oldLen+1, user.ID, oldFull, oldFull+"/%",
		); err != nil {
			s.logger.Error("cascade file paths failed", "error", err)
			writeJSONError(w, http.StatusInternalServerError, "cascade files failed")
			return
		}

		if _, err := tx.ExecContext(r.Context(),
			`UPDATE folders
			    SET parent_path = ? || SUBSTR(parent_path, ?)
			  WHERE user_id = ? AND (parent_path = ? OR parent_path LIKE ?)`,
			newFull, oldLen+1, user.ID, oldFull, oldFull+"/%",
		); err != nil {
			s.logger.Error("cascade folder paths failed", "error", err)
			writeJSONError(w, http.StatusInternalServerError, "cascade folders failed")
			return
		}
	}

	if err := tx.Commit(); err != nil {
		s.logger.Error("commit folder patch failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "commit failed")
		return
	}

	cur.Name = newName
	cur.ParentPath = newParent
	writeJSON(w, http.StatusOK, cur)
}

// ── DELETE FOLDER (soft delete) ──────────────────────────────────────────────

// DeleteFolderHandler handles DELETE /api/folders/{id}. Soft-deletes all
// descendant files too.
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
		s.logger.Error("lookup folder for delete failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	fullPath := folderFullPath(cur.ParentPath, cur.Name)

	// Collect storage paths to delete from disk.
	rows, err := s.db.QueryContext(r.Context(),
		`SELECT storage_path FROM files
		  WHERE user_id = ? AND (parent_path = ? OR parent_path LIKE ?)`,
		user.ID, fullPath, fullPath+"/%",
	)
	if err != nil {
		s.logger.Error("list descendant storage paths failed", "error", err)
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

	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.logger.Error("begin tx for folder delete failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "begin tx failed")
		return
	}
	defer tx.Rollback() //nolint:errcheck

	// Soft-delete descendant files.
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE files SET deleted_at = ? WHERE user_id = ? AND (parent_path = ? OR parent_path LIKE ?)`,
		now, user.ID, fullPath, fullPath+"/%",
	); err != nil {
		s.logger.Error("soft-delete descendant files failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "delete descendant files failed")
		return
	}

	// Delete descendant folders.
	if _, err := tx.ExecContext(r.Context(),
		`DELETE FROM folders WHERE user_id = ? AND (parent_path = ? OR parent_path LIKE ?)`,
		user.ID, fullPath, fullPath+"/%",
	); err != nil {
		s.logger.Error("delete descendant folders failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "delete descendant folders failed")
		return
	}

	// Delete the folder itself.
	if _, err := tx.ExecContext(r.Context(),
		`DELETE FROM folders WHERE id = ? AND user_id = ?`,
		id, user.ID,
	); err != nil {
		s.logger.Error("delete folder failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "delete folder failed")
		return
	}

	if err := tx.Commit(); err != nil {
		s.logger.Error("commit folder delete failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "commit failed")
		return
	}

	for _, sp := range storagePaths {
		_ = s.store.Delete(sp)
	}

	s.logger.Info("folder soft-deleted", "user_id", user.ID, "folder_id", id, "name", cur.Name)
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

func folderFullPath(parent, name string) string {
	if parent == "/" {
		return "/" + name
	}
	return parent + "/" + name
}

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

func parsePagination(r *http.Request) (limit, offset int) {
	limit = 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
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
