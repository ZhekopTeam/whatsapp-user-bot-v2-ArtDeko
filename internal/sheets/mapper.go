package sheets

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"my-whatsapp-bot/internal/domain"
)

func MapAccounts(rows [][]interface{}) ([]domain.Account, error) {
	if len(rows) == 0 {
		return nil, fmt.Errorf("accounts sheet is empty")
	}

	headers := parseHeaders(rows[0])
	accountIdx, ok := headers["account_id"]
	if !ok {
		return nil, fmt.Errorf("accounts sheet must contain account_id")
	}
	phoneIdx, ok := headers["ph_number"]
	if !ok {
		return nil, fmt.Errorf("accounts sheet must contain ph_number")
	}

	accounts := make([]domain.Account, 0, len(rows)-1)
	now := time.Now().UTC()
	for _, row := range rows[1:] {
		accountID, err := getIntCell(row, accountIdx)
		if err != nil {
			continue
		}
		phone := normalizePhone(getStringCell(row, phoneIdx))
		if phone == "" {
			continue
		}
		accounts = append(accounts, domain.Account{
			AccountID:  accountID,
			Phone:      phone,
			Status:     domain.AccountStatusAuthRequired,
			CreatedAt:  now,
			UpdatedAt:  now,
			LastSeenAt: now,
		})
	}

	return accounts, nil
}

func MapCommunications(rows [][]interface{}) ([]domain.Communication, error) {
	if len(rows) == 0 {
		return nil, fmt.Errorf("communications sheet is empty")
	}

	headers := parseHeaders(rows[0])
	columns := []string{"task_id", "account_1", "account_2", "start_date", "end_date", "enabled", "count_days"}
	for _, column := range columns {
		if _, ok := headers[column]; !ok {
			return nil, fmt.Errorf("communications sheet must contain %s", column)
		}
	}

	communications := make([]domain.Communication, 0, len(rows)-1)
	now := time.Now().UTC()
	for _, row := range rows[1:] {
		taskID, err := getIntCell(row, headers["task_id"])
		if err != nil {
			continue
		}
		account1, err := getIntCell(row, headers["account_1"])
		if err != nil {
			continue
		}
		account2, err := getIntCell(row, headers["account_2"])
		if err != nil {
			continue
		}
		startDate, err := parseSheetDate(getStringCell(row, headers["start_date"]))
		if err != nil {
			continue
		}
		endDate, err := parseSheetDate(getStringCell(row, headers["end_date"]))
		if err != nil || endDate.Before(startDate) {
			continue
		}
		countDays, err := getIntCell(row, headers["count_days"])
		if err != nil {
			countDays = 0
		}

		communications = append(communications, domain.Communication{
			TaskID:    taskID,
			Account1:  account1,
			Account2:  account2,
			StartDate: startDate,
			EndDate:   endDate,
			Enabled:   parseBoolCell(row, headers["enabled"]),
			CountDays: int(countDays),
			SheetHash: hashRow(row),
			SyncedAt:  now,
			CreatedAt: now,
			UpdatedAt: now,
		})
	}

	return communications, nil
}

func parseHeaders(row []interface{}) map[string]int {
	headers := make(map[string]int, len(row))
	for index, cell := range row {
		headers[strings.ToLower(strings.TrimSpace(fmt.Sprint(cell)))] = index
	}
	return headers
}

func getStringCell(row []interface{}, index int) string {
	if index < 0 || index >= len(row) {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(row[index]))
}

func getIntCell(row []interface{}, index int) (int64, error) {
	value := getStringCell(row, index)
	if value == "" {
		return 0, fmt.Errorf("empty int cell")
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err == nil {
		return parsed, nil
	}
	floatValue, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid int cell %q", value)
	}
	return int64(floatValue), nil
}

func parseBoolCell(row []interface{}, index int) bool {
	value := strings.ToLower(getStringCell(row, index))
	return value == "true" || value == "1" || value == "yes"
}

func parseSheetDate(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, fmt.Errorf("empty date value")
	}
	if parsed, err := time.Parse(domain.CommunicationDateLayout, value); err == nil {
		return parsed, nil
	}
	floatValue, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q", value)
	}
	base := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
	return base.AddDate(0, 0, int(floatValue)), nil
}

func normalizePhone(phone string) string {
	builder := strings.Builder{}
	for _, char := range phone {
		if char >= '0' && char <= '9' {
			builder.WriteRune(char)
		}
	}
	return builder.String()
}

func hashRow(row []interface{}) string {
	parts := make([]string, 0, len(row))
	for _, cell := range row {
		parts = append(parts, strings.TrimSpace(fmt.Sprint(cell)))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}
