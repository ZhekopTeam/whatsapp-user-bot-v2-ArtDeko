package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"

	_ "github.com/mattn/go-sqlite3"
	"my-whatsapp-bot/wa-user-bot/domain"
)

// AdminProxyRepo reads/writes the Python admin bot's SQLite database
// (accounts, proxies, groups). Path is resolved to the file that actually
// contains the accounts table — Go and Python can otherwise look at different
// relative "data/wa_bot_accounts.db" files depending on cwd.
type AdminProxyRepo struct {
	mu            sync.Mutex
	db            *sql.DB
	path          string
	configured    string
	readyLogged   bool
}

// NewAdminProxyRepo opens the admin bot database and returns a repo.
// Returns nil (no error) if no usable path is configured.
func NewAdminProxyRepo(adminDBPath string) (*AdminProxyRepo, error) {
	if adminDBPath == "" {
		adminDBPath = "/admin-tg-bot/data/wa_bot_accounts.db"
	}
	r := &AdminProxyRepo{configured: adminDBPath}
	if err := r.reopenLocked(context.Background()); err != nil {
		// Non-fatal at startup: Telegram bot may create the schema a moment later.
		fmt.Printf("admin db: initial open deferred (%v) — will retry on first use\n", err)
	}
	return r, nil
}

func (r *AdminProxyRepo) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.db != nil {
		_ = r.db.Close()
		r.db = nil
	}
}

func adminDBCandidates(configured string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, 6)
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	add(configured)
	add(os.Getenv("DATABASE_PATH"))
	add("/admin-tg-bot/data/wa_bot_accounts.db")
	add("/data/wa_bot_accounts.db")
	add("/app/data/wa_bot_accounts.db")
	add("data/wa_bot_accounts.db")
	return out
}

func openAdminSQLite(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func hasAccountsTable(db *sql.DB) bool {
	var name string
	err := db.QueryRow(`
		SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'accounts' LIMIT 1
	`).Scan(&name)
	return err == nil && name == "accounts"
}

// reopenLocked picks the best admin DB path and opens it. Caller must hold r.mu.
func (r *AdminProxyRepo) reopenLocked(ctx context.Context) error {
	_ = ctx
	if r.db != nil {
		_ = r.db.Close()
		r.db = nil
	}

	var fallbackPath string
	var fallbackDB *sql.DB

	for _, path := range adminDBCandidates(r.configured) {
		db, err := openAdminSQLite(path)
		if err != nil {
			continue
		}
		if hasAccountsTable(db) {
			r.db = db
			r.path = path
			if !r.readyLogged {
				fmt.Printf("admin db ready: %s (accounts table found)\n", path)
				r.readyLogged = true
			}
			if fallbackDB != nil {
				_ = fallbackDB.Close()
			}
			return nil
		}
		// Keep first openable file as fallback (schema may appear later).
		if fallbackDB == nil {
			fallbackDB = db
			fallbackPath = path
		} else {
			_ = db.Close()
		}
	}

	if fallbackDB != nil {
		r.db = fallbackDB
		r.path = fallbackPath
		fmt.Printf("admin db %q: table 'accounts' not found yet — "+
			"will retry when Telegram bot creates the schema\n", fallbackPath)
		return nil
	}
	return fmt.Errorf("no reachable admin db among candidates for %q", r.configured)
}

// ensureReady reopens the admin DB if the accounts table is missing.
func (r *AdminProxyRepo) ensureReady(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.db != nil && hasAccountsTable(r.db) {
		return nil
	}
	return r.reopenLocked(ctx)
}

func (r *AdminProxyRepo) withDB(ctx context.Context, fn func(*sql.DB, string) error) error {
	if r == nil {
		return nil
	}
	if err := r.ensureReady(ctx); err != nil {
		return err
	}
	r.mu.Lock()
	db, path := r.db, r.path
	r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("admin db not open")
	}
	err := fn(db, path)
	if err != nil && strings.Contains(err.Error(), "no such table") {
		// Schema appeared in another file, or was created after we opened an empty one.
		r.mu.Lock()
		_ = r.reopenLocked(ctx)
		db, path = r.db, r.path
		r.mu.Unlock()
		if db == nil {
			return err
		}
		return fn(db, path)
	}
	return err
}

// GetProxyForPhone returns the proxy assigned to the account,
// or nil if the account has no proxy.
func (r *AdminProxyRepo) GetProxyForPhone(ctx context.Context, phone string) (*domain.Proxy, error) {
	var result *domain.Proxy
	err := r.withDB(ctx, func(db *sql.DB, _ string) error {
		var proxy domain.Proxy
		var username, password sql.NullString

		qerr := db.QueryRowContext(ctx, `
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
		if qerr == nil {
			proxy.Username = username.String
			proxy.Password = password.String
			result = &proxy
			return nil
		}
		if qerr != sql.ErrNoRows {
			_ = qerr
		}

		qerr = db.QueryRowContext(ctx, `
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
		if qerr == sql.ErrNoRows || qerr != nil {
			return nil
		}
		proxy.Username = username.String
		proxy.Password = password.String
		result = &proxy
		return nil
	})
	return result, err
}

// ListEnabledCommIDs returns comm_id values for groups that are still enabled.
func (r *AdminProxyRepo) ListEnabledCommIDs(ctx context.Context) (map[int64]struct{}, error) {
	out := make(map[int64]struct{})
	err := r.withDB(ctx, func(db *sql.DB, _ string) error {
		rows, qerr := db.QueryContext(ctx, `
			SELECT comm_id
			FROM account_groups
			WHERE status = 'enabled' AND comm_id IS NOT NULL AND comm_id != 0
		`)
		if qerr != nil {
			return nil
		}
		defer rows.Close()
		for rows.Next() {
			var id sql.NullInt64
			if scanErr := rows.Scan(&id); scanErr != nil {
				return scanErr
			}
			if id.Valid && id.Int64 != 0 {
				out[id.Int64] = struct{}{}
			}
		}
		return rows.Err()
	})
	return out, err
}

// SetAccountStatusByPhone updates account status in the admin Telegram bot database.
func (r *AdminProxyRepo) SetAccountStatusByPhone(ctx context.Context, phone, status string) error {
	digits := normalizeAdminPhone(phone)
	return r.withDB(ctx, func(db *sql.DB, path string) error {
		res, err := db.ExecContext(ctx, `
			UPDATE accounts
			SET status = ?
			WHERE REPLACE(phone, '+', '') = ? OR phone = ? OR phone = ?
		`, status, digits, phone, "+"+digits)
		if err != nil {
			return fmt.Errorf("set admin account status for %s in %q: %w", phone, path, err)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return fmt.Errorf("%w: account %s not found in %q", sql.ErrNoRows, phone, path)
		}
		return nil
	})
}

func normalizeAdminPhone(phone string) string {
	var b strings.Builder
	for _, c := range phone {
		if c >= '0' && c <= '9' {
			b.WriteRune(c)
		}
	}
	return b.String()
}
