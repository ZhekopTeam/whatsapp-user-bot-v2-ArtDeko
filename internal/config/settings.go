package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	defaultSessionDBPath       = "file:sessions/multi.db?_foreign_keys=on"
	defaultRuntimeDBPath       = "sessions/runtime.db"
	defaultSyncInterval        = 2 * time.Minute
	defaultPlanningInterval    = time.Minute
	defaultDispatchInterval    = 15 * time.Second
	defaultDispatchBatchSize   = 10
	defaultTimezone            = "UTC"
	defaultFirstWindowStart    = "10:00"
	defaultFirstWindowEnd      = "14:00"
	defaultReplyDelayMinMinute = 40
	defaultReplyDelayMaxMinute = 60
)

type Settings struct {
	SpreadsheetID           string
	CredentialsPath         string
	SentencesPath           string
	AccountsSheetName       string
	CommunicationsSheetName string
	SessionDBPath           string
	RuntimeDBPath           string
	SyncInterval            time.Duration
	PlanningInterval        time.Duration
	DispatchInterval        time.Duration
	DispatchBatchSize       int
	Timezone                string
	FirstMessageWindowStart string
	FirstMessageWindowEnd   string
	ReplyDelayMinMinutes    int
	ReplyDelayMaxMinutes    int
}

func Load() (*Settings, error) {
	_ = godotenv.Load()

	settings := &Settings{
		SpreadsheetID:           strings.TrimSpace(os.Getenv("SPREADSHEET_ID")),
		CredentialsPath:         strings.TrimSpace(os.Getenv("CREDENTIALS_PATH")),
		SentencesPath:           strings.TrimSpace(os.Getenv("SENTENCES_PATH")),
		AccountsSheetName:       strings.TrimSpace(os.Getenv("ACCOUNTS_SHEET_NAME")),
		CommunicationsSheetName: strings.TrimSpace(os.Getenv("COMMUNICATIONS_SHEET_NAME")),
		SessionDBPath:           envString("SESSION_DB_PATH", defaultSessionDBPath),
		RuntimeDBPath:           envString("RUNTIME_DB_PATH", defaultRuntimeDBPath),
		SyncInterval:            envDuration("SYNC_INTERVAL", defaultSyncInterval),
		PlanningInterval:        envDuration("PLANNING_INTERVAL", defaultPlanningInterval),
		DispatchInterval:        envDuration("DISPATCH_INTERVAL", defaultDispatchInterval),
		DispatchBatchSize:       envInt("DISPATCH_BATCH_SIZE", defaultDispatchBatchSize),
		Timezone:                envString("BOT_TIMEZONE", defaultTimezone),
		FirstMessageWindowStart: envString("FIRST_MESSAGE_WINDOW_START", defaultFirstWindowStart),
		FirstMessageWindowEnd:   envString("FIRST_MESSAGE_WINDOW_END", defaultFirstWindowEnd),
		ReplyDelayMinMinutes:    envInt("REPLY_DELAY_MIN_MINUTES", defaultReplyDelayMinMinute),
		ReplyDelayMaxMinutes:    envInt("REPLY_DELAY_MAX_MINUTES", defaultReplyDelayMaxMinute),
	}

	if err := settings.Validate(); err != nil {
		return nil, err
	}

	return settings, nil
}

func (s *Settings) Validate() error {
	required := map[string]string{
		"SPREADSHEET_ID":            s.SpreadsheetID,
		"CREDENTIALS_PATH":          s.CredentialsPath,
		"SENTENCES_PATH":            s.SentencesPath,
		"ACCOUNTS_SHEET_NAME":       s.AccountsSheetName,
		"COMMUNICATIONS_SHEET_NAME": s.CommunicationsSheetName,
	}

	for name, value := range required {
		if value == "" {
			return fmt.Errorf("required env var %s is missing", name)
		}
	}

	if s.ReplyDelayMinMinutes <= 0 || s.ReplyDelayMaxMinutes <= 0 {
		return fmt.Errorf("reply delay values must be positive")
	}
	if s.ReplyDelayMinMinutes > s.ReplyDelayMaxMinutes {
		s.ReplyDelayMinMinutes, s.ReplyDelayMaxMinutes = s.ReplyDelayMaxMinutes, s.ReplyDelayMinMinutes
	}
	if s.DispatchBatchSize <= 0 {
		s.DispatchBatchSize = defaultDispatchBatchSize
	}

	if _, err := time.LoadLocation(s.Timezone); err != nil {
		return fmt.Errorf("invalid BOT_TIMEZONE %q: %w", s.Timezone, err)
	}
	if _, err := time.Parse("15:04", s.FirstMessageWindowStart); err != nil {
		return fmt.Errorf("invalid FIRST_MESSAGE_WINDOW_START: %w", err)
	}
	if _, err := time.Parse("15:04", s.FirstMessageWindowEnd); err != nil {
		return fmt.Errorf("invalid FIRST_MESSAGE_WINDOW_END: %w", err)
	}

	return nil

}

func envString(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}
