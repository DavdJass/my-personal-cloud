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

var schemaVersion = 1

func migrate(conn *sql.DB) error {
	// Create schema_version table if not exists.
	if _, err := conn.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}

	var current int
	err := conn.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&current)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	steps := []struct {
		version int
		sql     string
	}{
		{
			version: 1,
			sql: `
				CREATE TABLE IF NOT EXISTS users (
					id           INTEGER PRIMARY KEY AUTOINCREMENT,
					username     TEXT    NOT NULL UNIQUE,
					password_hash TEXT   NOT NULL,
					created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
				);
				CREATE TABLE IF NOT EXISTS files (
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
				);
				CREATE INDEX IF NOT EXISTS idx_files_user_parent ON files(user_id, parent_path);
				CREATE INDEX IF NOT EXISTS idx_files_user_image ON files(user_id, is_image);
				CREATE TABLE IF NOT EXISTS folders (
					id          TEXT    PRIMARY KEY,
					user_id     INTEGER NOT NULL,
					name        TEXT    NOT NULL,
					parent_path TEXT    NOT NULL DEFAULT '/',
					created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
				);
				CREATE INDEX IF NOT EXISTS idx_folders_user_parent ON folders(user_id, parent_path);
			`,
		},
		{
			version: 2,
			sql: `
				ALTER TABLE files ADD COLUMN deleted_at DATETIME;
			`,
		},
		{
			version: 3,
			sql: `
				CREATE TABLE IF NOT EXISTS share_links (
					id             TEXT    PRIMARY KEY,
					user_id        INTEGER NOT NULL,
					file_id        TEXT,
					folder_id      TEXT,
					token          TEXT    NOT NULL UNIQUE,
					expires_at     DATETIME NOT NULL,
					max_views      INTEGER NOT NULL DEFAULT 0,
					current_views  INTEGER NOT NULL DEFAULT 0,
					is_active      INTEGER NOT NULL DEFAULT 1,
					created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
					FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE SET NULL,
					FOREIGN KEY (folder_id) REFERENCES folders(id) ON DELETE SET NULL
				);
				CREATE INDEX IF NOT EXISTS idx_share_links_token ON share_links(token);
				CREATE INDEX IF NOT EXISTS idx_share_links_user ON share_links(user_id);
			`,
		},
	}

	tx, err := conn.Begin()
	if err != nil {
		return fmt.Errorf("begin migration tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	for _, step := range steps {
		if step.version <= current {
			continue
		}
		if _, err := tx.Exec(step.sql); err != nil {
			return fmt.Errorf("migrate v%d: %w\nstatement: %s", step.version, err, step.sql)
		}
		if _, err := tx.Exec(`INSERT INTO schema_version (version) VALUES (?)`, step.version); err != nil {
			return fmt.Errorf("record migration v%d: %w", step.version, err)
		}
	}

	return tx.Commit()
}
