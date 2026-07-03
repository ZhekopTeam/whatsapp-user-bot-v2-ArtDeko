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

// GetProxyForPhone returns the proxy assigned to the account with the given phone number,
// or nil if no proxy is assigned or the proxies table does not exist.
func (r *AdminProxyRepo) GetProxyForPhone(ctx context.Context, phone string) (*domain.Proxy, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}

	var proxy domain.Proxy
	var username, password sql.NullString

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
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		// Table may not exist if the bot hasn't created it yet — treat as no proxy
		return nil, nil
	}

	proxy.Username = username.String
	proxy.Password = password.String
	return &proxy, nil
}
