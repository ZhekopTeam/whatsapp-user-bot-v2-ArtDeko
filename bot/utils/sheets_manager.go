package utils

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

const communicationDateLayout = "2006-01-02"

type Account struct {
	AccountID int64
	Phone     string
}

type CommunicationTask struct {
	TaskID     int64
	AccountA   int64
	AccountB   int64
	StartDate  string
	EndDate    string
	Enabled    bool
	CountDays  int64
	SheetRowID int64
}

func InitGoogleSheetsService(ctx context.Context, credentialsPath string) (*sheets.Service, error) {
	service_client, err := sheets.NewService(
		ctx,
		option.WithCredentialsFile(credentialsPath),
		option.WithScopes(sheets.SpreadsheetsScope),
	)
	if err != nil {
		log.Output(2, fmt.Sprintf("failed to initialize google sheets client: %v", err))
		return nil, err
	}
	return service_client, nil
}

func GetAccounts(
	ctx context.Context,
	service_client *sheets.Service,
	spreadsheetID string,
	sheetName string,
) (map[int64]Account, error) {
	response, err := service_client.Spreadsheets.Values.Get(spreadsheetID, sheetName+"!A:B").Context(ctx).Do()
	if err != nil || len(response.Values) == 0 {
		log.Output(2, fmt.Sprintf("failed to get accounts from google sheets: %v, sheet: %s", err, sheetName))
		return nil, err
	}

	headers := parseHeaders(response.Values[0])
	accountIDIdx, ok := headers["account_id"]
	if !ok {
		return nil, fmt.Errorf("accounts sheet %q must contain column account_id", sheetName)
	}
	phoneIdx, ok := headers["ph_number"]
	if !ok {
		return nil, fmt.Errorf("accounts sheet %q must contain column ph_number", sheetName)
	}

	accounts := make(map[int64]Account)
	for row := 1; row < len(response.Values); row++ {
		line := response.Values[row]
		accountID, err := getIntCell(line, accountIDIdx)
		if err != nil {
			continue
		}
		phone := getStringCell(line, phoneIdx)
		if phone == "" {
			continue
		}
		accounts[accountID] = Account{AccountID: accountID, Phone: normalizePhone(phone)}
	}

	return accounts, nil
}

func getIntCell(row []interface{}, idx int) (int64, error) {
	value := getStringCell(row, idx)
	if value == "" {
		return 0, fmt.Errorf("empty integer value")
	}
	number, err := strconv.ParseInt(value, 10, 64)
	if err == nil {
		return number, nil
	}
	floatValue, floatErr := strconv.ParseFloat(value, 64)
	if floatErr != nil {
		return 0, fmt.Errorf("invalid integer value %q", value)
	}
	return int64(floatValue), nil
}

func parseHeaders(headerRow []interface{}) map[string]int {
	headers := make(map[string]int)
	for idx, cell := range headerRow {
		headers[strings.ToLower(strings.TrimSpace(fmt.Sprint(cell)))] = idx
	}
	return headers
}

func getStringCell(row []interface{}, idx int) string {
	if idx < 0 || idx >= len(row) {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(row[idx]))
}

func normalizePhone(phone string) string {
	builder := strings.Builder{}
	for _, ch := range phone {
		if ch >= '0' && ch <= '9' {
			builder.WriteRune(ch)
		}
	}
	return builder.String()
}
