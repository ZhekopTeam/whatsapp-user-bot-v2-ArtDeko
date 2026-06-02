package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"my-whatsapp-bot/internal/app"
	"my-whatsapp-bot/internal/config"
	"my-whatsapp-bot/internal/whatsapp"

	_ "github.com/mattn/go-sqlite3"
	waLog "go.mau.fi/whatsmeow/util/log"
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

// runAuthQR выполняет QR-авторизацию WhatsApp по номеру и стримит события в stdout как NDJSON.
// Логи whatsmeow подавляются (waLog.Noop), чтобы не засорять машиночитаемый поток.
func runAuthQR(ctx context.Context, settings *config.Settings, args []string) error {
	fs := flag.NewFlagSet("auth-qr", flag.ContinueOnError)
	phone := fs.String("phone", "", "phone number in international format")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *phone == "" {
		return flag.ErrHelp
	}

	encoder := json.NewEncoder(os.Stdout)
	emit := func(evt whatsapp.QREvent) {
		_ = encoder.Encode(evt)
	}

	manager := whatsapp.NewManager(settings.SessionDBPath)
	jid, alreadyExists, err := manager.AuthorizeByPhone(ctx, *phone, waLog.Noop, emit)
	if err != nil {
		return err
	}

	// already_authorized уже отправлен внутри AuthorizeByPhone — не дублируем success.
	if !alreadyExists {
		emit(whatsapp.QREvent{Type: whatsapp.QREventSuccess, JID: jid})
	}
	return nil
}

func emitAuthError(err error) {
	encoder := json.NewEncoder(os.Stdout)
	_ = encoder.Encode(whatsapp.QREvent{Type: whatsapp.QREventError, Message: err.Error()})
}

func runLogout(ctx context.Context, settings *config.Settings, args []string) error {
	fs := flag.NewFlagSet("logout-wa", flag.ContinueOnError)
	phone := fs.String("phone", "", "phone number in international format")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *phone == "" {
		return flag.ErrHelp
	}

	manager := whatsapp.NewManager(settings.SessionDBPath)
	removed, err := manager.RemoveSession(ctx, *phone)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(os.Stdout)
	return encoder.Encode(map[string]bool{"removed": removed})
}
