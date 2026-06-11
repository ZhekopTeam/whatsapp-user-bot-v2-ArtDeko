package whatsapp

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
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

type QREvent struct {
	Type    QREventType `json:"event"`
	Code    string      `json:"code,omitempty"`
	JID     string      `json:"jid,omitempty"`
	Message string      `json:"message,omitempty"`
}

func (m *Manager) AuthorizeByPhone(
	ctx context.Context,
	phone string,
	logger waLog.Logger,
	emit func(QREvent),
) (deviceJID string, alreadyExists bool, err error) {
	if logger == nil {
		logger = waLog.Noop
	}

	container, err := m.GetContainer(ctx, logger)
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

	// connectedCh закрывается, когда WhatsApp подтверждает сессию (events.Connected).
	// loggedOutCh получает причину, если сервер сразу же отклоняет нас (401).
	connectedCh := make(chan struct{})
	loggedOutCh := make(chan string, 1)
	client.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Connected:
			select {
			case <-connectedCh:
			default:
				close(connectedCh)
			}
		case *events.LoggedOut:
			select {
			case loggedOutCh <- fmt.Sprintf("%v", v.Reason):
			default:
			}
		}
	})

	qrChan, err := client.GetQRChannel(ctx)
	if err != nil {
		return "", false, fmt.Errorf("get qr channel: %w", err)
	}

	if err := client.Connect(); err != nil {
		return "", false, fmt.Errorf("connect auth client: %w", err)
	}
	defer client.Disconnect()

	// Шаг 1: ждём сканирования QR-кода.
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

	// QR-канал закрылся — код был отсканирован. Проверяем, что данные устройства сохранены.
	if client.Store == nil || client.Store.ID == nil {
		return "", false, fmt.Errorf("auth completed without stored device id")
	}

	if normalizePhone(client.Store.ID.User) != normalizePhone(phone) {
		scanned := normalizePhone(client.Store.ID.User)
		_ = client.Store.Delete(ctx)
		return "", false, fmt.Errorf(
			"authorized phone %s does not match requested account phone %s",
			scanned, normalizePhone(phone),
		)
	}

	// Шаг 2: ждём подтверждения сессии от сервера WhatsApp (events.Connected).
	// Если сервер отклонит сессию (401 LoggedOut) или истечёт таймаут — возвращаем ошибку.
	connCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	select {
	case <-connectedCh:
		// Сессия подтверждена!
	case reason := <-loggedOutCh:
		_ = client.Store.Delete(ctx)
		return "", false, fmt.Errorf("whatsapp rejected session: %s", reason)
	case <-connCtx.Done():
		_ = client.Store.Delete(ctx)
		return "", false, fmt.Errorf("timeout waiting for whatsapp to confirm session")
	}

	return fmt.Sprint(client.Store.ID), false, nil
}

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

func (m *Manager) RemoveSession(ctx context.Context, phone string) (bool, error) {
	// 1. Отключаем и удаляем клиента из памяти в первую очередь
	normalized := normalizePhone(phone)
	m.mu.Lock()
	client, exists := m.clientsByPhone[normalized]
	if exists {
		client.Disconnect()
		delete(m.clientsByPhone, normalized)
		for i, c := range m.clients {
			if c == client {
				m.clients = append(m.clients[:i], m.clients[i+1:]...)
				break
			}
		}
	}
	m.mu.Unlock()

	// 2. Удаляем устройство из базы
	container, err := m.GetContainer(ctx, waLog.Noop)
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
		if normalizePhone(device.ID.User) == normalized {
			if err := device.Delete(ctx); err != nil {
				return false, fmt.Errorf("delete device: %w", err)
			}
			return true, nil
		}
	}

	return false, nil
}
