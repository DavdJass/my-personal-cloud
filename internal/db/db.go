package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Open opens (or creates) the SQLite database at path and applies the schema.
// We use the pure-Go modernc.org/sqlite driver so the binary stays CGO-free
// and cross-compiles cleanly to ARM64 for the Raspberry Pi.
func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// SQLite serializes writers; using a single connection avoids "database is
	// locked" errors under concurrent uploads while keeping things simple.
	conn.SetMaxOpenConns(1)

	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	if err := migrate(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}

	return conn, nil
}

func migrate(conn *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			username     TEXT    NOT NULL UNIQUE,
			password_hash TEXT   NOT NULL,
			created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS files (
			id           TEXT    PRIMARY KEY,
			user_id      INTEGER NOT NULL,
			name         TEXT    NOT NULL,
			parent_path  TEXT    NOT NULL DEFAULT '/',
			storage_path TEXT    NOT NULL,
			mime_type    TEXT    NOT NULL,
			size_bytes   INTEGER NOT NULL,
			is_image     INTEGER NOT NULL DEFAULT 0,
			created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_files_user_parent ON files(user_id, parent_path)`,
		`CREATE INDEX IF NOT EXISTS idx_files_user_image ON files(user_id, is_image)`,
		`CREATE TABLE IF NOT EXISTS folders (
			id          TEXT    PRIMARY KEY,
			user_id     INTEGER NOT NULL,
			name        TEXT    NOT NULL,
			parent_path TEXT    NOT NULL DEFAULT '/',
			created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_folders_user_parent ON folders(user_id, parent_path)`,
		// AI search — kept in a separate table so the AI module is fully
		// isolated from the rest of the schema. Records here are optional;
		// removing them never affects the underlying file row.
		`CREATE TABLE IF NOT EXISTS file_embeddings (
			file_id     TEXT     PRIMARY KEY,
			embedding   BLOB     NOT NULL,
			description TEXT     NOT NULL,
			indexed_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE
		)`,
	}

	for _, s := range stmts {
		if _, err := conn.Exec(s); err != nil {
			return fmt.Errorf("migrate: %w\nstatement: %s", err, s)
		}
	}

	// Backwards-compatible column add for existing databases. SQLite has no
	// IF NOT EXISTS for ADD COLUMN, so we ignore the "duplicate column" error.
	if _, err := conn.Exec(`ALTER TABLE files ADD COLUMN ai_indexed INTEGER NOT NULL DEFAULT 0`); err != nil {
		if !isDuplicateColumn(err) {
			return fmt.Errorf("migrate add ai_indexed: %w", err)
		}
	}

	return nil
}

// isDuplicateColumn detects the SQLite error returned when ADD COLUMN runs
// against a column that already exists; we treat it as a no-op.
func isDuplicateColumn(err error) bool {
	msg := err.Error()
	return contains(msg, "duplicate column") || contains(msg, "already exists")
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
