package whatsapp

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
)

type Manager struct {
	sessionDBPath  string
	mu             sync.RWMutex
	clientsByPhone map[string]*whatsmeow.Client
	clients        []*whatsmeow.Client
	// OnStatusChange вызывается при изменении статуса подключения аккаунта.
	// phone — нормализованный номер из JID, status — одна из констант domain.AccountStatus*.
	OnStatusChange func(phone, status string)
}

func NewManager(sessionDBPath string) *Manager {
	return &Manager{
		sessionDBPath:  sessionDBPath,
		clientsByPhone: make(map[string]*whatsmeow.Client),
	}
}

func (m *Manager) Start(ctx context.Context) error {
	container, err := sqlstore.New(ctx, "sqlite3", m.sessionDBPath, waLog.Stdout("Database", "INFO", true))
	if err != nil {
		return fmt.Errorf("create whatsapp sql store: %w", err)
	}

	devices, err := container.GetAllDevices(ctx)
	if err != nil {
		return fmt.Errorf("load whatsapp devices: %w", err)
	}
	if len(devices) == 0 {
		log.Println("whatsapp manager started without existing sessions")
		return nil
	}

	clientLog := waLog.Stdout("Client", "INFO", true)
	clients := make([]*whatsmeow.Client, 0, len(devices))
	clientsByPhone := make(map[string]*whatsmeow.Client, len(devices))
	for _, device := range devices {
		phone := ""
		if device.ID != nil {
			phone = normalizePhone(device.ID.User)
		}
		client := whatsmeow.NewClient(device, clientLog)
		client.AddEventHandler(makeEventHandler(phone, m.OnStatusChange))
		if err := client.Connect(); err != nil {
			return fmt.Errorf("connect whatsapp client: %w", err)
		}
		clients = append(clients, client)
		if phone != "" {
			clientsByPhone[phone] = client
		}
	}

	m.mu.Lock()
	m.clients = clients
	m.clientsByPhone = clientsByPhone
	m.mu.Unlock()

	log.Printf("whatsapp manager connected %d session(s)", len(clients))
	return nil
}

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, client := range m.clients {
		client.Disconnect()
	}
	m.clients = nil
	m.clientsByPhone = make(map[string]*whatsmeow.Client)
}

func (m *Manager) ClientByPhone(phone string) (*whatsmeow.Client, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	client, ok := m.clientsByPhone[normalizePhone(phone)]
	return client, ok
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
