package main

import (
	"context"
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

	if err := application.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("run app: %v", err)
	}
}
