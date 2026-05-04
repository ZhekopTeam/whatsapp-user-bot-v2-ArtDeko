package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"my-whatsapp-bot/internal/domain"
)

type SessionsRepo struct {
	db *sql.DB
}

func NewSessionsRepo(db *sql.DB) *SessionsRepo {
	return &SessionsRepo{db: db}
}

func (r *SessionsRepo) Upsert(ctx context.Context, session domain.SessionState) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO wa_sessions (account_id, device_jid, is_authorized, is_connected, last_connected_at, last_error, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_id) DO UPDATE SET
			device_jid = excluded.device_jid,
			is_authorized = excluded.is_authorized,
			is_connected = excluded.is_connected,
			last_connected_at = excluded.last_connected_at,
			last_error = excluded.last_error,
			updated_at = excluded.updated_at
	`, session.AccountID, session.DeviceJID, session.IsAuthorized, session.IsConnected, session.LastConnectedAt, session.LastError, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("upsert session state: %w", err)
	}
	return nil
}
