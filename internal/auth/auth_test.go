package auth

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		t.Fatalf("create users table: %v", err)
	}
	return db
}

func TestEnsureUser(t *testing.T) {
	db := openTestDB(t)
	s := NewService(db, []byte("test-secret"), 24*time.Hour)
	ctx := context.Background()

	if err := s.EnsureUser(ctx, "admin", "secret123"); err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}

	// EnsureUser should be idempotent.
	if err := s.EnsureUser(ctx, "admin", "secret123"); err != nil {
		t.Fatalf("EnsureUser idempotent: %v", err)
	}

	// Empty credentials should fail.
	if err := s.EnsureUser(ctx, "", ""); err == nil {
		t.Fatal("expected error for empty credentials")
	}
}

func TestLogin(t *testing.T) {
	db := openTestDB(t)
	s := NewService(db, []byte("test-secret"), 24*time.Hour)
	ctx := context.Background()

	_ = s.EnsureUser(ctx, "testuser", "correctpw")

	// Valid login.
	token, user, err := s.Login(ctx, "testuser", "correctpw")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	if user.Username != "testuser" {
		t.Fatalf("expected username 'testuser', got %q", user.Username)
	}

	// Invalid password.
	_, _, err = s.Login(ctx, "testuser", "wrongpw")
	if err == nil {
		t.Fatal("expected error for wrong password")
	}

	// Non-existent user.
	_, _, err = s.Login(ctx, "unknown", "pw")
	if err == nil {
		t.Fatal("expected error for unknown user")
	}
}

func TestParseToken(t *testing.T) {
	db := openTestDB(t)
	s := NewService(db, []byte("test-secret"), 24*time.Hour)

	token, err := s.issueToken(1, "alice")
	if err != nil {
		t.Fatalf("issueToken: %v", err)
	}

	user, err := s.parseToken(token)
	if err != nil {
		t.Fatalf("parseToken: %v", err)
	}
	if user.ID != 1 || user.Username != "alice" {
		t.Fatalf("got user %+v, want {1 alice}", user)
	}

	// Invalid signature.
	_, err = s.parseToken(token + "tampered")
	if err == nil {
		t.Fatal("expected error for tampered token")
	}

	// Wrong secret.
	s2 := NewService(db, []byte("different-secret"), 24*time.Hour)
	_, err = s2.parseToken(token)
	if err == nil {
		t.Fatal("expected error for wrong secret")
	}
}

func TestExpiredToken(t *testing.T) {
	db := openTestDB(t)
	s := NewService(db, []byte("test-secret"), 1*time.Nanosecond)
	ctx := context.Background()

	_ = s.EnsureUser(ctx, "user", "pass")
	token, _, err := s.Login(ctx, "user", "pass")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	// Wait a tiny bit for the token to expire.
	time.Sleep(10 * time.Millisecond)

	_, err = s.parseToken(token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}
