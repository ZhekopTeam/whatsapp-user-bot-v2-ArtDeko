package sqlite

import (
	"database/sql"
	"fmt"
)

var schemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS accounts (
		account_id INTEGER PRIMARY KEY,
		phone TEXT NOT NULL,
		status TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL,
		last_seen_at TIMESTAMP NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS communications (
		comm_id INTEGER PRIMARY KEY,
		account_1 INTEGER NOT NULL,
		account_2 INTEGER NOT NULL,
		start_date TEXT NOT NULL,
		end_date TEXT NOT NULL,
		enabled BOOLEAN NOT NULL,
		count_days INTEGER NOT NULL,
		name TEXT NOT NULL,
		sheet_hash TEXT NOT NULL,
		synced_at TIMESTAMP NOT NULL,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS communication_runs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		comm_id INTEGER NOT NULL,
		run_date TEXT NOT NULL,
		status TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL,
		UNIQUE(comm_id, run_date)
	);`,
	`CREATE TABLE IF NOT EXISTS message_jobs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		comm_id INTEGER NOT NULL,
		run_date TEXT NOT NULL,
		step_no INTEGER NOT NULL,
		sender_account_id INTEGER NOT NULL,
		receiver_account_id INTEGER NOT NULL,
		planned_at TIMESTAMP NOT NULL,
		status TEXT NOT NULL,
		message_text TEXT NOT NULL,
		attempt_count INTEGER NOT NULL DEFAULT 0,
		last_error TEXT NOT NULL DEFAULT '',
		sent_at TIMESTAMP,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL,
		UNIQUE(comm_id, run_date, step_no)
	);`,
	`CREATE TABLE IF NOT EXISTS wa_sessions (
		account_id INTEGER PRIMARY KEY,
		device_jid TEXT NOT NULL DEFAULT '',
		is_authorized BOOLEAN NOT NULL DEFAULT FALSE,
		is_connected BOOLEAN NOT NULL DEFAULT FALSE,
		last_connected_at TIMESTAMP,
		last_error TEXT NOT NULL DEFAULT '',
		updated_at TIMESTAMP NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS sync_state (
		source_name TEXT PRIMARY KEY,
		last_sync_at TIMESTAMP,
		last_success_at TIMESTAMP,
		last_error TEXT NOT NULL DEFAULT ''
	);`,
	`CREATE INDEX IF NOT EXISTS idx_accounts_phone ON accounts(phone);`,
	`CREATE INDEX IF NOT EXISTS idx_communications_enabled_dates ON communications(enabled, start_date, end_date);`,
	`CREATE INDEX IF NOT EXISTS idx_message_jobs_due ON message_jobs(status, planned_at);`,
}

func Migrate(db *sql.DB) error {
	for _, statement := range schemaStatements {
		if _, err := db.Exec(statement); err != nil {
			return fmt.Errorf("apply migration: %w", err)
		}
	}
	// Existing DBs created before "name" was added — CREATE IF NOT EXISTS won't alter them.
	if err := ensureColumn(db, "communications", "name", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	return nil
}

func ensureColumn(db *sql.DB, table, column, definition string) error {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return fmt.Errorf("pragma table_info(%s): %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("scan table_info(%s): %w", table, err)
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	ddl := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition)
	if _, err := db.Exec(ddl); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}
