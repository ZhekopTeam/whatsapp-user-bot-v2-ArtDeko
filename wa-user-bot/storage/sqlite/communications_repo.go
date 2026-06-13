package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"my-whatsapp-bot/wa-user-bot/domain"
)

type CommunicationsRepo struct {
	db *sql.DB
}

func NewCommunicationsRepo(db *sql.DB) *CommunicationsRepo {
	return &CommunicationsRepo{db: db}
}

func (r *CommunicationsRepo) UpsertMany(ctx context.Context, communications []domain.Communication) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin communications tx: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO communications (
			comm_id, account_1, account_2, start_date, end_date, enabled, count_days,
			sheet_hash, synced_at, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(comm_id) DO UPDATE SET
			account_1 = excluded.account_1,
			account_2 = excluded.account_2,
			start_date = excluded.start_date,
			end_date = excluded.end_date,
			enabled = excluded.enabled,
			count_days = excluded.count_days,
			sheet_hash = excluded.sheet_hash,
			synced_at = excluded.synced_at,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("prepare communications upsert: %w", err)
	}
	defer stmt.Close()

	now := time.Now()
	for _, communication := range communications {
		createdAt := communication.CreatedAt
		if createdAt.IsZero() {
			createdAt = now
		}
		updatedAt := communication.UpdatedAt
		if updatedAt.IsZero() {
			updatedAt = now
		}
		syncedAt := communication.SyncedAt
		if syncedAt.IsZero() {
			syncedAt = now
		}

		if _, err := stmt.ExecContext(
			ctx,
			communication.TaskID,
			communication.Account1,
			communication.Account2,
			communication.StartDate.Format(domain.CommunicationDateLayout),
			communication.EndDate.Format(domain.CommunicationDateLayout),
			communication.Enabled,
			communication.CountDays,
			communication.SheetHash,
			syncedAt,
			createdAt,
			updatedAt,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("upsert communication %d: %w", communication.TaskID, err)
		}
	}

	return tx.Commit()
}

func (r *CommunicationsRepo) ListEnabledForDate(ctx context.Context, day time.Time) ([]domain.Communication, error) {
	dateValue := day.Format(domain.CommunicationDateLayout)
	rows, err := r.db.QueryContext(ctx, `
		SELECT comm_id, account_1, account_2, start_date, end_date, enabled, count_days, sheet_hash, synced_at, created_at, updated_at
		FROM communications
		WHERE enabled = 1 AND start_date <= ? AND end_date >= ?
		ORDER BY comm_id
	`, dateValue, dateValue)
	if err != nil {
		return nil, fmt.Errorf("query communications: %w", err)
	}
	defer rows.Close()

	communications := make([]domain.Communication, 0)
	for rows.Next() {
		var communication domain.Communication
		var startDate string
		var endDate string
		if err := rows.Scan(
			&communication.TaskID,
			&communication.Account1,
			&communication.Account2,
			&startDate,
			&endDate,
			&communication.Enabled,
			&communication.CountDays,
			&communication.SheetHash,
			&communication.SyncedAt,
			&communication.CreatedAt,
			&communication.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan communication: %w", err)
		}
		communication.StartDate, err = time.Parse(domain.CommunicationDateLayout, startDate)
		if err != nil {
			return nil, fmt.Errorf("parse communication start_date: %w", err)
		}
		communication.EndDate, err = time.Parse(domain.CommunicationDateLayout, endDate)
		if err != nil {
			return nil, fmt.Errorf("parse communication end_date: %w", err)
		}
		communications = append(communications, communication)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate communications: %w", err)
	}

	return communications, nil
}
