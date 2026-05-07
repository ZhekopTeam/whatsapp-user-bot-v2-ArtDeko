package domain

import "time"

const CommunicationDateLayout = "2006-01-02"

type Communication struct {
	TaskID    int64
	Account1  int64
	Account2  int64
	StartDate time.Time
	EndDate   time.Time
	Enabled   bool
	CountDays int
	SheetHash string
	SyncedAt  time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (c Communication) IncludesDate(day time.Time) bool {
	target := normalizeDate(day)
	start := normalizeDate(c.StartDate)
	end := normalizeDate(c.EndDate)
	return !target.Before(start) && !target.After(end)
}

func normalizeDate(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}
