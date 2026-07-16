package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
	"my-whatsapp-bot/wa-user-bot/domain"
)

// AdminProxyRepo reads proxy assignments from the Python admin bot's SQLite database.
// The file is shared via a Docker volume at /app/data/wa_bot_accounts.db.
type AdminProxyRepo struct {
	db *sql.DB
}

// NewAdminProxyRepo opens the admin bot database (read-only) and returns a repo.
// Returns nil (no error) if the file does not exist yet — proxy support is optional.
func NewAdminProxyRepo(adminDBPath string) (*AdminProxyRepo, error) {
	if adminDBPath == "" {
		return nil, nil
	}
	db, err := sql.Open("sqlite3", adminDBPath+"?mode=ro&_foreign_keys=on")
	if err != nil {
		// Non-fatal: proxy table may not exist yet
		return nil, fmt.Errorf("open admin db: %w", err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping admin db: %w", err)
	}
	return &AdminProxyRepo{db: db}, nil
}

func (r *AdminProxyRepo) Close() {
	if r.db != nil {
		_ = r.db.Close()
	}
}

// GetProxyForPhone returns the proxy assigned to the account's group,
// or nil if the account is not in a group / group has no proxy.
func (r *AdminProxyRepo) GetProxyForPhone(ctx context.Context, phone string) (*domain.Proxy, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}

	var proxy domain.Proxy
	var username, password sql.NullString

	// Preferred: proxy bound directly to the account. This is the source of
	// truth now — the account is authorized through this proxy, so the IP stays
	// consistent between authorization and runtime.
	err := r.db.QueryRowContext(ctx, `
		SELECT p.id, p.name, p.proxy_type, p.host, p.port, p.username, p.password
		FROM accounts a
		JOIN proxies p ON a.proxy_id = p.id
		WHERE a.phone = ?
		LIMIT 1
	`, phone).Scan(
		&proxy.ID,
		&proxy.Name,
		&proxy.ProxyType,
		&proxy.Host,
		&proxy.Port,
		&username,
		&password,
	)
	if err == nil {
		proxy.Username = username.String
		proxy.Password = password.String
		return &proxy, nil
	}
	if err != sql.ErrNoRows {
		// Tables may not exist yet — fall through to legacy group lookup
		_ = err
	}

	// Legacy fallback: proxy bound to the account's active group.
	err = r.db.QueryRowContext(ctx, `
		SELECT p.id, p.name, p.proxy_type, p.host, p.port, p.username, p.password
		FROM accounts a
		JOIN account_group_members m ON m.account_id = a.id
		JOIN account_groups g ON g.id = m.group_id
		JOIN proxies p ON p.id = g.proxy_id
		WHERE a.phone = ?
		LIMIT 1
	`, phone).Scan(
		&proxy.ID,
		&proxy.Name,
		&proxy.ProxyType,
		&proxy.Host,
		&proxy.Port,
		&username,
		&password,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, nil
	}

	proxy.Username = username.String
	proxy.Password = password.String
	return &proxy, nil
}

// ListEnabledCommIDs returns comm_id values for groups that are still enabled.
// Used to scope ring-rebuild to active warmups only (ignore stale leftover jobs).
func (r *AdminProxyRepo) ListEnabledCommIDs(ctx context.Context) (map[int64]struct{}, error) {
	out := make(map[int64]struct{})
	if r == nil || r.db == nil {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT comm_id
		FROM account_groups
		WHERE status = 'enabled' AND comm_id IS NOT NULL AND comm_id != 0
	`)
	if err != nil {
		// Table may not exist yet
		return out, nil
	}
	defer rows.Close()
	for rows.Next() {
		var id sql.NullInt64
		if err := rows.Scan(&id); err != nil {
			return out, err
		}
		if id.Valid && id.Int64 != 0 {
			out[id.Int64] = struct{}{}
		}
	}
	return out, rows.Err()
}
