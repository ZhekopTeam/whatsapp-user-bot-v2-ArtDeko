package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type SyncStateRepo struct {
	db *sql.DB
}

func NewSyncStateRepo(db *sql.DB) *SyncStateRepo {
	return &SyncStateRepo{db: db}
}

func (r *SyncStateRepo) MarkSuccess(ctx context.Context, source string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO sync_state (source_name, last_sync_at, last_success_at, last_error)
		VALUES (?, ?, ?, '')
		ON CONFLICT(source_name) DO UPDATE SET
			last_sync_at = excluded.last_sync_at,
			last_success_at = excluded.last_success_at,
			last_error = ''
	`, source, time.Now().UTC(), time.Now().UTC())
	if err != nil {
		return fmt.Errorf("mark sync success: %w", err)
	}
	return nil
}

func (r *SyncStateRepo) MarkFailure(ctx context.Context, source string, message string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO sync_state (source_name, last_sync_at, last_success_at, last_error)
		VALUES (?, ?, NULL, ?)
		ON CONFLICT(source_name) DO UPDATE SET
			last_sync_at = excluded.last_sync_at,
			last_error = excluded.last_error
	`, source, time.Now().UTC(), message)
	if err != nil {
		return fmt.Errorf("mark sync failure: %w", err)
	}
	return nil
}
