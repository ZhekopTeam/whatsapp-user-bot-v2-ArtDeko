package whatsapp

import (
	"context"
	"fmt"
	"os"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
)

func (m *Manager) EnsureSession(ctx context.Context, phone string) (deviceJID string, alreadyExists bool, err error) {
	container, err := sqlstore.New(ctx, "sqlite3", m.sessionDBPath, waLog.Stdout("Database", "INFO", true))
	if err != nil {
		return "", false, fmt.Errorf("create whatsapp sql store: %w", err)
	}

	devices, err := container.GetAllDevices(ctx)
	if err != nil {
		return "", false, fmt.Errorf("load whatsapp devices: %w", err)
	}
	for _, device := range devices {
		if device.ID == nil {
			continue
		}
		if normalizePhone(device.ID.User) == normalizePhone(phone) {
			return fmt.Sprint(device.ID), true, nil
		}
	}

	device := container.NewDevice()
	client := whatsmeow.NewClient(device, waLog.Stdout("AuthClient", "WARN", true))
	client.AddEventHandler(eventHandler)

	qrChan, err := client.GetQRChannel(ctx)
	if err != nil {
		return "", false, fmt.Errorf("get qr channel: %w", err)
	}

	if err := client.Connect(); err != nil {
		return "", false, fmt.Errorf("connect auth client: %w", err)
	}
	defer client.Disconnect()

	for evt := range qrChan {
		switch evt.Event {
		case "code":
			fmt.Fprintln(os.Stdout, "Scan this QR with the WhatsApp account:")
			qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
		case "success":
			fmt.Fprintln(os.Stdout, "WhatsApp login succeeded")
		case "timeout":
			return "", false, fmt.Errorf("qr code timed out")
		case "err-client-outdated":
			return "", false, fmt.Errorf("client outdated during auth")
		case "err-scanned-without-multidevice":
			return "", false, fmt.Errorf("qr scanned without multidevice enabled")
		default:
			fmt.Fprintf(os.Stdout, "Auth event: %s\n", evt.Event)
		}
	}

	if client.Store == nil || client.Store.ID == nil {
		return "", false, fmt.Errorf("auth completed without stored device id")
	}
	if normalizePhone(client.Store.ID.User) != normalizePhone(phone) {
		return "", false, fmt.Errorf("authorized phone %s does not match requested account phone %s", normalizePhone(client.Store.ID.User), normalizePhone(phone))
	}

	return fmt.Sprint(client.Store.ID), false, nil
}
