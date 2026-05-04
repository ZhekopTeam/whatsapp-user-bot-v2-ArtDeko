package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"my-whatsapp-bot/internal/app"
	"my-whatsapp-bot/internal/config"

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
