package storage

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// LocalStore stores files on the local filesystem under a root directory.
// Files are stored at <root>/<userID>/<uuid> so metadata lookups stay fast
// and path-traversal bugs are contained to a single user's sandbox.
type LocalStore struct {
	root string
}

// New creates a LocalStore rooted at the given directory.
func New(root string) *LocalStore {
	return &LocalStore{root: root}
}

// Save copies the reader into the store and returns the relative storage
// path and the number of bytes written. The returned path is safe to use
// with Open and Delete.
func (s *LocalStore) Save(userID int64, src io.Reader) (string, int64, error) {
	dir := filepath.Join(s.root, strconv.FormatInt(userID, 10))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, fmt.Errorf("create user dir: %w", err)
	}

	name := uuid.NewString()
	dst := filepath.Join(dir, name)

	f, err := os.Create(dst)
	if err != nil {
		return "", 0, fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	n, err := io.Copy(f, src)
	if err != nil {
		os.Remove(dst)
		return "", 0, fmt.Errorf("write file: %w", err)
	}

	rel, _ := filepath.Rel(s.root, dst)
	return rel, n, nil
}

// Open opens a file by its relative storage path.
func (s *LocalStore) Open(path string) (*os.File, error) {
	full := s.abs(path)
	f, err := os.Open(full)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", full, err)
	}
	return f, nil
}

// Stat returns the FileInfo for the given relative storage path.
func (s *LocalStore) Stat(path string) (fs.FileInfo, error) {
	full := s.abs(path)
	return os.Stat(full)
}

// Delete removes the file at the given relative storage path.
func (s *LocalStore) Delete(path string) error {
	full := s.abs(path)
	err := os.Remove(full)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete %s: %w", full, err)
	}
	return nil
}

// abs resolves a relative storage path against the store root, ensuring it
// stays within bounds.
func (s *LocalStore) abs(rel string) string {
	// filepath.Join cleans the path, removing any ".." traversal attempts.
	cleaned := filepath.Join(s.root, rel)
	// Safety: ensure the cleaned path is still under root (defence in depth).
	if !strings.HasPrefix(cleaned, filepath.Clean(s.root)+string(filepath.Separator)) &&
		cleaned != filepath.Clean(s.root) {
		// If someone somehow gets ".." past Join, fall back to a safe path.
		return filepath.Join(s.root, "_invalid")
	}
	return cleaned
}
