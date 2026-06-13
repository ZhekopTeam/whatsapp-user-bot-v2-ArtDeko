package domain

import "time"

const (
	RunStatusPlanned   = "planned"
	RunStatusActive    = "active"
	RunStatusDone      = "done"
	RunStatusCancelled = "cancelled"
	RunStatusSkipped   = "skipped"

	JobStatusPending   = "pending"
	JobStatusSending   = "sending"
	JobStatusSent      = "sent"
	JobStatusFailed    = "failed"
	JobStatusCancelled = "cancelled"
)

type CommunicationRun struct {
	ID        int64
	TaskID    int64
	RunDate   time.Time
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Message struct {
	ID                int64
	CommID            int64
	RunDate           time.Time
	StepNo            int
	SenderAccountID   int64
	ReceiverAccountID int64
	PlannedAt         time.Time
	Status            string
	MessageText       string
	AttemptCount      int
	LastError         string
	SentAt            *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
