package storage

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndOpen(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	content := []byte("hello world")
	r := bytes.NewReader(content)

	rel, n, err := s.Save(1, r)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if n != int64(len(content)) {
		t.Fatalf("Save size: got %d, want %d", n, len(content))
	}
	if rel == "" {
		t.Fatal("Save returned empty path")
	}

	f, err := s.Open(rel)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	buf.ReadFrom(f)
	if buf.String() != string(content) {
		t.Fatalf("content mismatch: got %q, want %q", buf.String(), string(content))
	}
}

func TestDelete(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	r := bytes.NewReader([]byte("data"))
	rel, _, err := s.Save(1, r)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := s.Delete(rel); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// File should be gone.
	if _, err := s.Open(rel); err == nil {
		t.Fatal("expected error opening deleted file")
	}
}

func TestDeleteNonExistent(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	if err := s.Delete("nonexistent"); err != nil {
		t.Fatalf("Delete non-existent: %v", err)
	}
}

func TestPathTraversal(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	// Attempt to open a path outside the store root.
	_, err := s.Open("../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestUserSeparation(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	// Save file for user 1.
	rel1, _, err := s.Save(1, bytes.NewReader([]byte("user1")))
	if err != nil {
		t.Fatalf("Save user1: %v", err)
	}

	// Save file for user 2.
	rel2, _, err := s.Save(2, bytes.NewReader([]byte("user2")))
	if err != nil {
		t.Fatalf("Save user2: %v", err)
	}

	// Ensure paths are different (different user directories).
	if rel1 == rel2 {
		t.Fatal("expected different storage paths for different users")
	}

	// Verify they start with different user IDs.
	if !filepath.HasPrefix(rel1, "1") && !filepath.HasPrefix(rel1, "1\\") {
		t.Fatalf("expected rel path %q to start with user 1 dir", rel1)
	}
	if !filepath.HasPrefix(rel2, "2") && !filepath.HasPrefix(rel2, "2\\") {
		t.Fatalf("expected rel path %q to start with user 2 dir", rel2)
	}
}

func TestStat(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	rel, _, err := s.Save(1, bytes.NewReader([]byte("stats")))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	fi, err := s.Stat(rel)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if fi.Size() != 5 {
		t.Fatalf("Stat size: got %d, want 5", fi.Size())
	}

	// Non-existent should fail.
	_, err = s.Stat("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
	if !os.IsNotExist(err) {
		t.Fatalf("expected IsNotExist, got %v", err)
	}
}
