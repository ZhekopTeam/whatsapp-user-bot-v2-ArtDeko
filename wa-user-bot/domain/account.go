package domain

import "time"

const (
	AccountStatusNew          = "new"
	AccountStatusAuthRequired = "auth_required"
	AccountStatusReady        = "ready"
	AccountStatusDisconnected = "disconnected"
	AccountStatusFailed       = "failed"
	AccountStatusBlocked      = "blocked"
)

type Account struct {
	AccountID  int64
	Phone      string
	Status     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	LastSeenAt time.Time
}
