package bot

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Settings struct {
	SpreadsheetID   string
	CredentialsPath string
	SentencesPath   string
	AccountsSheetName string
	CommunicationsSheetName string
}

var SettingsInstance *Settings

func LoadSettings() {
	if err := godotenv.Load(); err != nil {
		log.Fatal("Не найден .env файл")
	}

	SettingsInstance = &Settings{
		SpreadsheetID:   os.Getenv("SPREADSHEET_ID"),
		CredentialsPath: os.Getenv("CREDENTIALS_PATH"),
		SentencesPath:   os.Getenv("SENTENCES_PATH"),
		AccountsSheetName: os.Getenv("ACCOUNTS_SHEET_NAME"),
		CommunicationsSheetName: os.Getenv("COMMUNICATIONS_SHEET_NAME"),
	}

	if SettingsInstance.SpreadsheetID == "" || SettingsInstance.CredentialsPath == "" || SettingsInstance.SentencesPath == "" || SettingsInstance.AccountsSheetName == "" || SettingsInstance.CommunicationsSheetName == "" {
		log.Fatal("One or more required environment variables are missing")
	}
}

func getEnv(key, defaltValue string) string {
	if value := os.Getenv(key); value != "" {
        return value
    }
    return defaltValue
}