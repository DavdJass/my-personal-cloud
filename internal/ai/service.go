package ai

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/DavdJass/my-personal-cloud/internal/auth"
	"github.com/DavdJass/my-personal-cloud/internal/storage"
)

// Service exposes the AI-powered semantic search HTTP handlers.
type Service struct {
	db     *sql.DB
	store  *storage.LocalStore
	client *Client
}

// NewService wires the dependencies needed by the AI handlers.
func NewService(db *sql.DB, store *storage.LocalStore, client *Client) *Service {
	return &Service{db: db, store: store, client: client}
}

// Model returns the embedding model name (used by main.go for logging).
func (s *Service) Model() string { return s.client.Model() }

// ── STATUS ───────────────────────────────────────────────────────────────────

// StatusHandler reports whether AI search is configured. The frontend uses
// this to decide whether to show the "AI search" UI elements at all.
func StatusHandler(enabled bool, model string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled": enabled,
			"model":   model,
		})
	}
}

// ── INDEX ────────────────────────────────────────────────────────────────────

// IndexHandler reads the file from disk, builds an indexable description,
// requests an embedding from DeepSeek, and stores the result. Returns the
// updated record. Errors here never affect the underlying file row.
func (s *Service) IndexHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := chi.URLParam(r, "id")

	var (
		name, mime, parent, storagePath string
	)
	err := s.db.QueryRowContext(r.Context(),
		`SELECT name, mime_type, parent_path, storage_path
		   FROM files WHERE id = ? AND user_id = ?`,
		id, user.ID,
	).Scan(&name, &mime, &parent, &storagePath)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "file not found")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	description := s.buildDescription(name, mime, parent, storagePath)

	// 25s context for the embedding call. The cloud as a whole stays
	// responsive thanks to chi's per-request goroutines.
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	vec, err := s.client.Embed(ctx, description)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "embedding failed: "+err.Error())
		return
	}

	blob := encodeFloats(vec)
	if _, err := s.db.ExecContext(r.Context(),
		`INSERT INTO file_embeddings (file_id, embedding, description)
		      VALUES (?, ?, ?)
		 ON CONFLICT(file_id) DO UPDATE SET
		      embedding = excluded.embedding,
		      description = excluded.description,
		      indexed_at = CURRENT_TIMESTAMP`,
		id, blob, description,
	); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "store embedding: "+err.Error())
		return
	}
	if _, err := s.db.ExecContext(r.Context(),
		`UPDATE files SET ai_indexed = 1 WHERE id = ? AND user_id = ?`,
		id, user.ID,
	); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "mark indexed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"file_id":     id,
		"description": description,
		"dimensions":  len(vec),
		"indexed_at":  time.Now(),
	})
}

// ── REMOVE INDEX ─────────────────────────────────────────────────────────────

// RemoveIndexHandler deletes the embedding row and clears the ai_indexed flag.
// The file itself is left untouched.
func (s *Service) RemoveIndexHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := chi.URLParam(r, "id")

	// Ownership check before touching anything.
	var owned int
	if err := s.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM files WHERE id = ? AND user_id = ?`,
		id, user.ID,
	).Scan(&owned); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if owned == 0 {
		writeJSONError(w, http.StatusNotFound, "file not found")
		return
	}

	if _, err := s.db.ExecContext(r.Context(),
		`DELETE FROM file_embeddings WHERE file_id = ?`, id,
	); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "delete embedding failed")
		return
	}
	if _, err := s.db.ExecContext(r.Context(),
		`UPDATE files SET ai_indexed = 0 WHERE id = ? AND user_id = ?`,
		id, user.ID,
	); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "clear flag failed")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ── SEARCH ───────────────────────────────────────────────────────────────────

// SearchResult is one row in the ranked search response.
type SearchResult struct {
	FileID     string  `json:"file_id"`
	Name       string  `json:"name"`
	ParentPath string  `json:"parent_path"`
	MimeType   string  `json:"mime_type"`
	IsImage    bool    `json:"is_image"`
	Score      float32 `json:"score"`
}

// SearchHandler embeds the query, loads the user's embeddings, ranks them by
// cosine similarity in Go, and returns the top matches. Default limit is 20.
func (s *Service) SearchHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSONError(w, http.StatusBadRequest, "missing 'q' parameter")
		return
	}

	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	queryVec, err := s.client.Embed(ctx, q)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "embedding query failed: "+err.Error())
		return
	}

	rows, err := s.db.QueryContext(r.Context(),
		`SELECT f.id, f.name, f.parent_path, f.mime_type, f.is_image, e.embedding
		   FROM files f
		   JOIN file_embeddings e ON e.file_id = f.id
		  WHERE f.user_id = ?`,
		user.ID,
	)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "load embeddings failed")
		return
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var (
			res     SearchResult
			isImage int
			blob    []byte
		)
		if err := rows.Scan(&res.FileID, &res.Name, &res.ParentPath, &res.MimeType, &isImage, &blob); err != nil {
			continue
		}
		res.IsImage = isImage == 1
		vec, err := decodeFloats(blob)
		if err != nil || len(vec) != len(queryVec) {
			continue
		}
		res.Score = cosineSimilarity(queryVec, vec)
		results = append(results, res)
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if len(results) > limit {
		results = results[:limit]
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"query":   q,
		"count":   len(results),
		"results": results,
	})
}

// ── HELPERS ──────────────────────────────────────────────────────────────────

// buildDescription returns the human-readable text that gets embedded for a
// given file. Strategy is intentionally simple and pure-Go (no PDF/Office
// parsing) so the binary stays small and Pi-friendly.
func (s *Service) buildDescription(name, mime, parent, storagePath string) string {
	header := fmt.Sprintf("File name: %s\nFolder: %s\nType: %s", name, parent, mime)

	low := strings.ToLower(mime)
	textLike := strings.HasPrefix(low, "text/") ||
		low == "application/json" ||
		low == "application/xml"

	if textLike {
		if body := s.readTextSnippet(storagePath, 4000); body != "" {
			return header + "\nContent:\n" + body
		}
	}
	if strings.HasPrefix(low, "image/") {
		return "Image file: " + name + "\nFolder: " + parent + "\nType: " + mime
	}
	return header
}

// readTextSnippet reads up to maxBytes from the file and returns it as a UTF-8
// safe string. Errors are swallowed because the description falls back to the
// header-only variant on failure.
func (s *Service) readTextSnippet(storagePath string, maxBytes int) string {
	f, err := s.store.Open(storagePath)
	if err != nil {
		return ""
	}
	defer f.Close()

	buf := make([]byte, maxBytes)
	n, _ := io.ReadFull(f, buf)
	if n == 0 {
		return ""
	}
	chunk := buf[:n]
	if !utf8.Valid(chunk) {
		// Trim trailing bytes that may have split a UTF-8 codepoint.
		for len(chunk) > 0 && !utf8.Valid(chunk) {
			chunk = chunk[:len(chunk)-1]
		}
	}
	return string(chunk)
}

// encodeFloats serialises a vector as little-endian float32 bytes. Compact
// and deterministic; cheaper to scan than JSON for thousands of vectors.
func encodeFloats(v []float32) []byte {
	out := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(f))
	}
	return out
}

// decodeFloats is the inverse of encodeFloats.
func decodeFloats(b []byte) ([]float32, error) {
	if len(b)%4 != 0 {
		return nil, errors.New("ai: invalid embedding blob length")
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out, nil
}

// cosineSimilarity computes the cosine similarity between two equal-length
// vectors. Returns 0 when either vector is all zeros.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		x := float64(a[i])
		y := float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
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
