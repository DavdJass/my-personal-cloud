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
	}

	for _, s := range stmts {
		if _, err := conn.Exec(s); err != nil {
			return fmt.Errorf("migrate: %w\nstatement: %s", err, s)
		}
	}
	return nil
}
