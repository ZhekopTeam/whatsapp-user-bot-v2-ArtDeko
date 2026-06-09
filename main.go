package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"my-whatsapp-bot/internal/app"
	"my-whatsapp-bot/internal/config"
	"my-whatsapp-bot/internal/whatsapp"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	settings, err := config.Load()
	if err != nil {
		log.Fatalf("load settings: %v", err)
	}

	if err := os.MkdirAll("sessions", os.ModePerm); err != nil {
		log.Fatalf("create sessions directory: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// auth-qr — лёгкая команда для Telegram-бота: QR-авторизация по номеру без зависимости от Google Sheets.
	if len(os.Args) > 1 && os.Args[1] == "auth-qr" {
		if err := runAuthQR(ctx, settings, os.Args[2:]); err != nil {
			emitAuthError(err)
			os.Exit(1)
		}
		return
	}

	// logout-wa — удаление WhatsApp-сессии по номеру (используется Telegram-ботом при удалении аккаунта).
	if len(os.Args) > 1 && os.Args[1] == "logout-wa" {
		if err := runLogout(ctx, settings, os.Args[2:]); err != nil {
			log.Fatalf("logout: %v", err)
		}
		return
	}

	application, err := app.New(ctx, settings)
	if err != nil {
		log.Fatalf("initialize app: %v", err)
	}
	defer func() {
		if err := application.Close(); err != nil {
			log.Printf("close app: %v", err)
		}
	}()

	if len(os.Args) > 1 {
		if err := runCLICommand(ctx, application, os.Args[1:]); err != nil {
			log.Fatalf("run command: %v", err)
		}
		return
	}

	if err := application.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("run app: %v", err)
	}
}

func runCLICommand(ctx context.Context, application *app.App, args []string) error {
	switch args[0] {
	case "auth":
		fs := flag.NewFlagSet("auth", flag.ContinueOnError)
		accountID := fs.Int64("account-id", 0, "account id from Google Sheets")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *accountID <= 0 {
			return flag.ErrHelp
		}
		return application.AuthAccount(ctx, *accountID)
	default:
		return flag.ErrHelp
	}
}

// runAuthQR is deprecated. Use the HTTP API.
func runAuthQR(ctx context.Context, settings *config.Settings, args []string) error {
	return fmt.Errorf("auth-qr via CLI is deprecated. Please perform auth via the Telegram Bot / HTTP API")
}

func emitAuthError(err error) {
	encoder := json.NewEncoder(os.Stdout)
	_ = encoder.Encode(whatsapp.QREvent{Type: whatsapp.QREventError, Message: err.Error()})
}

// runLogout is deprecated. Use the HTTP API.
func runLogout(ctx context.Context, settings *config.Settings, args []string) error {
	return fmt.Errorf("logout-wa via CLI is deprecated. Please perform logout via the Telegram Bot / HTTP API")
}
