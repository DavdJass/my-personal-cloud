package backup

import (
	"database/sql"
	"fmt"
)

// DBHolder wraps a *sql.DB so it can survive a backup restore.
// After RestoreBackup, the holder's Replace method creates a new
// connection from the restored file.
type DBHolder struct {
	db *sql.DB
}

// NewDBHolder wraps an existing *sql.DB connection.
func NewDBHolder(db *sql.DB) *DBHolder {
	return &DBHolder{db: db}
}

// DB returns the current *sql.DB.
func (h *DBHolder) DB() *sql.DB {
	return h.db
}

// Replace copies the restored database over the live DB file,
// closes the old connection, and opens a new one.
// After calling Replace, all services that cached the old *sql.DB
// will see "database is closed" — the server must restart.
func (h *DBHolder) Replace(path string) (*sql.DB, error) {
	if err := h.db.Close(); err != nil {
		return nil, fmt.Errorf("close old db: %w", err)
	}
	conn, err := reopenSQLite(path)
	if err != nil {
		return nil, err
	}
	h.db = conn
	return conn, nil
}
