// Package shares implements shareable links for files and folders.
// Users can generate time-limited, view-limited URLs to share their
// content with anyone, even without authentication.
package shares

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/DavdJass/my-personal-cloud/internal/auth"
	"github.com/DavdJass/my-personal-cloud/internal/storage"
)

// ShareLink represents a shareable link for a file or folder.
type ShareLink struct {
	ID           string    `json:"id"`
	UserID       int64     `json:"user_id"`
	FileID       *string   `json:"file_id,omitempty"`
	FolderID     *string   `json:"folder_id,omitempty"`
	Token        string    `json:"-"`
	ShareURL     string    `json:"url"`
	ExpiresAt    time.Time `json:"expires_at"`
	MaxViews     int       `json:"max_views"`
	CurrentViews int       `json:"current_views"`
	IsActive     bool      `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
	Name         string    `json:"name,omitempty"`
}

// PublicFile is the view of a file exposed through a share link.
type PublicFile struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	MimeType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
	IsImage   bool   `json:"is_image"`
}

// PublicFolder is the view of a folder exposed through a share link.
type PublicFolder struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Service handles share link CRUD and public access.
type Service struct {
	db    *sql.DB
	store *storage.LocalStore
}

// NewService creates a shares service.
func NewService(db *sql.DB, store *storage.LocalStore) *Service {
	return &Service{db: db, store: store}
}

// generateToken creates a cryptographically random URL-safe token.
func generateToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// CreateShare creates a new share link.
func (s *Service) CreateShare(ctx context.Context, userID int64, fileID, folderID *string, expiresInHours int, maxViews int) (*ShareLink, error) {
	if fileID == nil && folderID == nil {
		return nil, errors.New("provide file_id or folder_id")
	}
	if fileID != nil && folderID != nil {
		return nil, errors.New("provide only one of file_id or folder_id")
	}

	if expiresInHours <= 0 {
		expiresInHours = 1
	}
	if expiresInHours > 168 { // 7 days
		expiresInHours = 168
	}
	if maxViews < 0 {
		maxViews = 0
	}

	token, err := generateToken()
	if err != nil {
		return nil, err
	}

	id := uuid.NewString()
	now := time.Now().UTC()
	expiresAt := now.Add(time.Duration(expiresInHours) * time.Hour)

	// Verify the file/folder belongs to the user.
	if fileID != nil {
		var exists int
		err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM files WHERE id = ? AND user_id = ?`,
			*fileID, userID,
		).Scan(&exists)
		if err != nil {
			return nil, fmt.Errorf("check file: %w", err)
		}
		if exists == 0 {
			return nil, errors.New("file not found")
		}
	}
	if folderID != nil {
		var exists int
		err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM folders WHERE id = ? AND user_id = ?`,
			*folderID, userID,
		).Scan(&exists)
		if err != nil {
			return nil, fmt.Errorf("check folder: %w", err)
		}
		if exists == 0 {
			return nil, errors.New("folder not found")
		}
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO share_links (id, user_id, file_id, folder_id, token, expires_at, max_views)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, userID, fileID, folderID, token, expiresAt, maxViews,
	)
	if err != nil {
		return nil, fmt.Errorf("insert share: %w", err)
	}

	shareURL := fmt.Sprintf("/share/%s", token)
	share := &ShareLink{
		ID:           id,
		UserID:       userID,
		FileID:       fileID,
		FolderID:     folderID,
		Token:        token,
		ShareURL:     shareURL,
		ExpiresAt:    expiresAt,
		MaxViews:     maxViews,
		CurrentViews: 0,
		IsActive:     true,
		CreatedAt:    now,
	}
	return share, nil
}

// ListShares returns all active share links for a user.
func (s *Service) ListShares(ctx context.Context, userID int64) ([]ShareLink, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT sl.id, sl.user_id, sl.file_id, sl.folder_id, sl.token,
		        sl.expires_at, sl.max_views, sl.current_views, sl.is_active, sl.created_at,
		        COALESCE(f.name, fo.name, '')
		 FROM share_links sl
		 LEFT JOIN files f ON sl.file_id = f.id
		 LEFT JOIN folders fo ON sl.folder_id = fo.id
		 WHERE sl.user_id = ?
		 ORDER BY sl.created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list shares: %w", err)
	}
	defer rows.Close()

	out := []ShareLink{}
	for rows.Next() {
		var sl ShareLink
		var fileID, folderID sql.NullString
		var isActive int
		if err := rows.Scan(&sl.ID, &sl.UserID, &fileID, &folderID, &sl.Token,
			&sl.ExpiresAt, &sl.MaxViews, &sl.CurrentViews, &isActive, &sl.CreatedAt, &sl.Name); err != nil {
			return nil, fmt.Errorf("scan share: %w", err)
		}
		if fileID.Valid {
			sl.FileID = &fileID.String
		}
		if folderID.Valid {
			sl.FolderID = &folderID.String
		}
		sl.IsActive = isActive == 1
		sl.ShareURL = fmt.Sprintf("/share/%s", sl.Token)
		out = append(out, sl)
	}
	return out, nil
}

// RevokeShare deactivates a share link.
func (s *Service) RevokeShare(ctx context.Context, userID int64, id string) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE share_links SET is_active = 0 WHERE id = ? AND user_id = ?`,
		id, userID,
	)
	if err != nil {
		return fmt.Errorf("revoke share: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return errors.New("share not found")
	}
	return nil
}

// ── Public access ─────────────────────────────────────────────────────────

// shareInfo holds the resolved share link data for public access handlers.
type shareInfo struct {
	shareID    string
	fileID     *string
	folderID   *string
	token      string
	expiresAt  time.Time
	maxViews   int
	curViews   int
}

// resolveShare looks up a share by token and validates it.
func (s *Service) resolveShare(ctx context.Context, token string) (*shareInfo, error) {
	var si shareInfo
	var fileID, folderID sql.NullString
	var isActive int

	err := s.db.QueryRowContext(ctx,
		`SELECT id, file_id, folder_id, token, expires_at, max_views, current_views, is_active
		 FROM share_links WHERE token = ?`,
		token,
	).Scan(&si.shareID, &fileID, &folderID, &si.token, &si.expiresAt, &si.maxViews, &si.curViews, &isActive)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("not found")
	}
	if err != nil {
		return nil, fmt.Errorf("lookup share: %w", err)
	}
	if fileID.Valid {
		si.fileID = &fileID.String
	}
	if folderID.Valid {
		si.folderID = &folderID.String
	}

	if isActive == 0 {
		return nil, errors.New("share has been revoked")
	}
	if time.Now().UTC().After(si.expiresAt) {
		return nil, errors.New("share has expired")
	}
	if si.maxViews > 0 && si.curViews >= si.maxViews {
		return nil, errors.New("share has reached maximum views")
	}

	return &si, nil
}

// incrementViews atomically increments the view counter.
func (s *Service) incrementViews(ctx context.Context, id string) {
	_, _ = s.db.ExecContext(ctx,
		`UPDATE share_links SET current_views = current_views + 1 WHERE id = ?`,
		id,
	)
}

// PublicFileHandler handles GET /share/{token} for a file share.
func (s *Service) PublicFileHandler(w http.ResponseWriter, r *http.Request) {
	si, err := s.resolveShare(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}
	if si.fileID == nil {
		writeJSONError(w, http.StatusBadRequest, "this share points to a folder, not a file")
		return
	}

	var f PublicFile
	var isImage int
	err = s.db.QueryRowContext(r.Context(),
		`SELECT id, name, mime_type, size_bytes, is_image FROM files WHERE id = ?`,
		*si.fileID,
	).Scan(&f.ID, &f.Name, &f.MimeType, &f.SizeBytes, &isImage)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "file not found")
		return
	}
	if err != nil {
		slog.Error("lookup shared file failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	f.IsImage = isImage == 1

	s.incrementViews(r.Context(), si.shareID)

	inline := r.URL.Query().Get("inline") == "1"
	if inline || strings.HasPrefix(f.MimeType, "image/") || strings.HasPrefix(f.MimeType, "video/") {
		// Stream inline for media
		var storagePath string
		err := s.db.QueryRowContext(r.Context(),
			`SELECT storage_path FROM files WHERE id = ?`, *si.fileID,
		).Scan(&storagePath)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "storage lookup failed")
			return
		}
		file, err := s.store.Open(storagePath)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "open failed")
			return
		}
		defer file.Close()

		w.Header().Set("Content-Type", f.MimeType)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", f.SizeBytes))
		http.ServeContent(w, r, f.Name, time.Time{}, file)
		return
	}

	// Return file metadata as JSON (for download links in the UI)
	writeJSON(w, http.StatusOK, map[string]any{
		"type":       "file",
		"file":       f,
		"share_id":   si.shareID,
	})
}

// PublicFolderHandler handles GET /share/{token} for a folder share.
func (s *Service) PublicFolderHandler(w http.ResponseWriter, r *http.Request) {
	si, err := s.resolveShare(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}
	if si.folderID == nil {
		writeJSONError(w, http.StatusBadRequest, "this share points to a file, not a folder")
		return
	}

	// Get folder path info
	var folderName, parentPath string
	err = s.db.QueryRowContext(r.Context(),
		`SELECT name, parent_path FROM folders WHERE id = ?`,
		*si.folderID,
	).Scan(&folderName, &parentPath)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "folder not found")
		return
	}
	if err != nil {
		slog.Error("lookup shared folder failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	// Build the full path of the folder for listing contents
	fullPath := parentPath
	if fullPath == "/" {
		fullPath = "/" + folderName
	} else {
		fullPath = fullPath + "/" + folderName
	}

	// List sub-folders
	fRows, err := s.db.QueryContext(r.Context(),
		`SELECT id, name FROM folders WHERE parent_path = ? ORDER BY name COLLATE NOCASE`,
		fullPath,
	)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "list folders failed")
		return
	}
	folders := []PublicFolder{}
	for fRows.Next() {
		var f PublicFolder
		if err := fRows.Scan(&f.ID, &f.Name); err == nil {
			folders = append(folders, f)
		}
	}
	fRows.Close()

	// List files
	fileRows, err := s.db.QueryContext(r.Context(),
		`SELECT id, name, mime_type, size_bytes, is_image FROM files
		 WHERE parent_path = ? AND deleted_at IS NULL
		 ORDER BY name COLLATE NOCASE`,
		fullPath,
	)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "list files failed")
		return
	}
	files := []PublicFile{}
	for fileRows.Next() {
		var f PublicFile
		var isImage int
		if err := fileRows.Scan(&f.ID, &f.Name, &f.MimeType, &f.SizeBytes, &isImage); err == nil {
			f.IsImage = isImage == 1
			files = append(files, f)
		}
	}
	fileRows.Close()

	s.incrementViews(r.Context(), si.shareID)

	writeJSON(w, http.StatusOK, map[string]any{
		"type":         "folder",
		"folder_name":  folderName,
		"folders":      folders,
		"files":        files,
		"share_id":     si.shareID,
	})
}

// PublicShareRouter handles both file and folder shares via a single endpoint
// that inspects the share target and redirects accordingly.
// It also supports ?file_id=X for downloading individual files within a shared folder.
func (s *Service) PublicShareRouter(w http.ResponseWriter, r *http.Request) {
	si, err := s.resolveShare(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}

	// If downloading a specific file within a shared folder
	if fileID := r.URL.Query().Get("file_id"); fileID != "" && si.folderID != nil {
		s.streamSharedFile(w, r, fileID, si)
		return
	}

	// Dispatch
	if si.fileID != nil {
		s.PublicFileHandler(w, r)
	} else if si.folderID != nil {
		s.PublicFolderHandler(w, r)
	} else {
		writeJSONError(w, http.StatusInternalServerError, "invalid share")
	}
}

// streamSharedFile streams a file by ID within a shared folder context.
func (s *Service) streamSharedFile(w http.ResponseWriter, r *http.Request, fileID string, si *shareInfo) {
	var f PublicFile
	var isImage int
	err := s.db.QueryRowContext(r.Context(),
		`SELECT id, name, mime_type, size_bytes, is_image FROM files WHERE id = ?`,
		fileID,
	).Scan(&f.ID, &f.Name, &f.MimeType, &f.SizeBytes, &isImage)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "file not found")
		return
	}
	if err != nil {
		slog.Error("lookup shared file failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	f.IsImage = isImage == 1

	s.incrementViews(r.Context(), si.shareID)

	var storagePath string
	err = s.db.QueryRowContext(r.Context(),
		`SELECT storage_path FROM files WHERE id = ?`, fileID,
	).Scan(&storagePath)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "storage lookup failed")
		return
	}
	file, err := s.store.Open(storagePath)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "open failed")
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", f.MimeType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", f.SizeBytes))
	http.ServeContent(w, r, f.Name, time.Time{}, file)
}

// ── Authenticated handlers ──────────────────────────────────────────────

// CreateHandler handles POST /api/shares.
func (s *Service) CreateHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body struct {
		FileID         *string `json:"file_id"`
		FolderID       *string `json:"folder_id"`
		ExpiresInHours int     `json:"expires_in_hours"`
		MaxViews       int     `json:"max_views"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.ExpiresInHours <= 0 {
		body.ExpiresInHours = 1
	}

	share, err := s.CreateShare(r.Context(), user.ID, body.FileID, body.FolderID, body.ExpiresInHours, body.MaxViews)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, share)
}

// ListHandler handles GET /api/shares.
func (s *Service) ListHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	shares, err := s.ListShares(r.Context(), user.ID)
	if err != nil {
		slog.Error("list shares failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "list shares failed")
		return
	}
	if shares == nil {
		shares = []ShareLink{}
	}

	writeJSON(w, http.StatusOK, map[string]any{"shares": shares})
}

// RevokeHandler handles DELETE /api/shares/{id}.
func (s *Service) RevokeHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := chi.URLParam(r, "id")
	if err := s.RevokeShare(r.Context(), user.ID, id); err != nil {
		if err.Error() == "share not found" {
			writeJSONError(w, http.StatusNotFound, "share not found")
			return
		}
		slog.Error("revoke share failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "revoke failed")
		return
	}

	w.WriteHeader(http.StatusNoContent)
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
