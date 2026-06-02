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

// QREventType описывает тип события в процессе QR-авторизации.
type QREventType string

const (
	QREventCode              QREventType = "qr"
	QREventSuccess           QREventType = "success"
	QREventTimeout           QREventType = "timeout"
	QREventError             QREventType = "error"
	QREventAlreadyAuthorized QREventType = "already_authorized"
)

// QREvent — событие авторизации, пригодное для сериализации (NDJSON) и для терминального рендера.
type QREvent struct {
	Type    QREventType `json:"event"`
	Code    string      `json:"code,omitempty"`
	JID     string      `json:"jid,omitempty"`
	Message string      `json:"message,omitempty"`
}

// AuthorizeByPhone выполняет QR-авторизацию WhatsApp-аккаунта по номеру телефона.
// Все события (QR-коды, успех, ошибки) передаются через emit. Логи whatsmeow пишутся через logger.
// Возвращает device JID и флаг, что сессия для номера уже существовала.
func (m *Manager) AuthorizeByPhone(
	ctx context.Context,
	phone string,
	logger waLog.Logger,
	emit func(QREvent),
) (deviceJID string, alreadyExists bool, err error) {
	if logger == nil {
		logger = waLog.Noop
	}

	container, err := sqlstore.New(ctx, "sqlite3", m.sessionDBPath, logger)
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
			jid := fmt.Sprint(device.ID)
			emit(QREvent{Type: QREventAlreadyAuthorized, JID: jid})
			return jid, true, nil
		}
	}

	device := container.NewDevice()
	client := whatsmeow.NewClient(device, logger)
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
			emit(QREvent{Type: QREventCode, Code: evt.Code})
		case "timeout":
			return "", false, fmt.Errorf("qr code timed out")
		case "err-client-outdated":
			return "", false, fmt.Errorf("client outdated during auth")
		case "err-scanned-without-multidevice":
			return "", false, fmt.Errorf("qr scanned without multidevice enabled")
		}
	}

	if client.Store == nil || client.Store.ID == nil {
		return "", false, fmt.Errorf("auth completed without stored device id")
	}

	if normalizePhone(client.Store.ID.User) != normalizePhone(phone) {
		scanned := normalizePhone(client.Store.ID.User)
		// Удаляем ошибочно привязанное устройство, чтобы не засорять multi.db.
		_ = client.Store.Delete(ctx)
		return "", false, fmt.Errorf(
			"authorized phone %s does not match requested account phone %s",
			scanned, normalizePhone(phone),
		)
	}

	return fmt.Sprint(client.Store.ID), false, nil
}

// EnsureSession сохраняет терминальное поведение CLI-команды `auth`: рендерит QR в stdout.
func (m *Manager) EnsureSession(ctx context.Context, phone string) (deviceJID string, alreadyExists bool, err error) {
	logger := waLog.Stdout("AuthClient", "WARN", true)
	emit := func(evt QREvent) {
		if evt.Type == QREventCode {
			fmt.Fprintln(os.Stdout, "Scan this QR with the WhatsApp account:")
			qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
		}
	}
	jid, exists, err := m.AuthorizeByPhone(ctx, phone, logger, emit)
	if err == nil && !exists {
		fmt.Fprintln(os.Stdout, "WhatsApp login succeeded")
	}
	return jid, exists, err
}

// RemoveSession удаляет устройство WhatsApp по номеру телефона из multi.db.
// Возвращает true, если устройство было найдено и удалено.
func (m *Manager) RemoveSession(ctx context.Context, phone string) (bool, error) {
	container, err := sqlstore.New(ctx, "sqlite3", m.sessionDBPath, waLog.Noop)
	if err != nil {
		return false, fmt.Errorf("create whatsapp sql store: %w", err)
	}

	devices, err := container.GetAllDevices(ctx)
	if err != nil {
		return false, fmt.Errorf("load whatsapp devices: %w", err)
	}

	for _, device := range devices {
		if device.ID == nil {
			continue
		}
		if normalizePhone(device.ID.User) == normalizePhone(phone) {
			if err := device.Delete(ctx); err != nil {
				return false, fmt.Errorf("delete device: %w", err)
			}
			return true, nil
		}
	}

	return false, nil
}
