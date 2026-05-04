package domain

import "time"

type SessionState struct {
	AccountID       int64
	DeviceJID       string
	IsAuthorized    bool
	IsConnected     bool
	LastConnectedAt *time.Time
	LastError       string
	UpdatedAt       time.Time
}
