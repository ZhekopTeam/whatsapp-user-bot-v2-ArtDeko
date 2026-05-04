package sqlite

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

func Open(path string) (*sql.DB, error) {
	if err := ensureParentDir(path); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}

	if _, err := db.Exec(`PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL; PRAGMA busy_timeout = 5000;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure sqlite pragmas: %w", err)
	}

	if err := Migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func ensureParentDir(path string) error {
	if stringsHasPrefix(path, "file:") {
		return nil
	}
	parent := filepath.Dir(path)
	if parent == "." || parent == "" {
		return nil
	}
	return os.MkdirAll(parent, os.ModePerm)
}

func stringsHasPrefix(value string, prefix string) bool {
	if len(value) < len(prefix) {
		return false
	}
	return value[:len(prefix)] == prefix
}
