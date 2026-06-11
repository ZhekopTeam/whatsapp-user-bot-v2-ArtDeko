package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"my-whatsapp-bot/wa-user-bot/domain"
)

type AccountsRepo struct {
	db *sql.DB
}

func NewAccountsRepo(db *sql.DB) *AccountsRepo {
	return &AccountsRepo{db: db}
}

func (r *AccountsRepo) UpsertMany(ctx context.Context, accounts []domain.Account) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin accounts tx: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO accounts (account_id, phone, status, created_at, updated_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_id) DO UPDATE SET
			phone = excluded.phone,
			status = CASE WHEN accounts.status IN ('blocked', 'failed', 'ready', 'disconnected') THEN accounts.status ELSE excluded.status END,
			updated_at = excluded.updated_at,
			last_seen_at = excluded.last_seen_at
	`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("prepare accounts upsert: %w", err)
	}
	defer stmt.Close()

	now := time.Now().UTC()
	for _, account := range accounts {
		createdAt := account.CreatedAt
		if createdAt.IsZero() {
			createdAt = now
		}
		updatedAt := account.UpdatedAt
		if updatedAt.IsZero() {
			updatedAt = now
		}
		lastSeenAt := account.LastSeenAt
		if lastSeenAt.IsZero() {
			lastSeenAt = now
		}
		status := account.Status
		if status == "" {
			status = domain.AccountStatusAuthRequired
		}

		if _, err := stmt.ExecContext(ctx, account.AccountID, account.Phone, status, createdAt, updatedAt, lastSeenAt); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("upsert account %d: %w", account.AccountID, err)
		}
	}

	return tx.Commit()
}

func (r *AccountsRepo) GetByID(ctx context.Context, accountID int64) (domain.Account, error) {
	var account domain.Account
	err := r.db.QueryRowContext(ctx, `
		SELECT account_id, phone, status, created_at, updated_at, last_seen_at
		FROM accounts WHERE account_id = ?
	`, accountID).Scan(
		&account.AccountID,
		&account.Phone,
		&account.Status,
		&account.CreatedAt,
		&account.UpdatedAt,
		&account.LastSeenAt,
	)
	if err != nil {
		return domain.Account{}, err
	}
	return account, nil
}

func (r *AccountsRepo) UpdateStatus(ctx context.Context, accountID int64, status string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE accounts
		SET status = ?, updated_at = ?
		WHERE account_id = ?
	`, status, time.Now().UTC(), accountID)
	if err != nil {
		return fmt.Errorf("update account status: %w", err)
	}
	return nil
}

func (r *AccountsRepo) UpdateStatusByPhone(ctx context.Context, phone, status string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE accounts
		SET status = ?, updated_at = ?
		WHERE phone = ?
	`, status, time.Now().UTC(), phone)
	if err != nil {
		return fmt.Errorf("update account status by phone: %w", err)
	}
	return nil
}
