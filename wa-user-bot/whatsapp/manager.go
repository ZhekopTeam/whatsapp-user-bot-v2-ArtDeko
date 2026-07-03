package whatsapp

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"my-whatsapp-bot/wa-user-bot/domain"
)

func init() {
	store.DeviceProps.Os = proto.String("Windows")
	store.DeviceProps.PlatformType = waCompanionReg.DeviceProps_CHROME.Enum()
	store.DeviceProps.RequireFullSync = proto.Bool(false)
}

// ProxyLookup is a function that returns the proxy for a given phone number.
// It returns nil if no proxy is assigned.
type ProxyLookup func(ctx context.Context, phone string) (*domain.Proxy, error)

type Manager struct {
	sessionDBPath  string
	mu             sync.RWMutex
	container      *sqlstore.Container
	clientsByPhone map[string]*whatsmeow.Client
	clients        []*whatsmeow.Client
	// OnStatusChange is called when a connection status changes.
	// phone — normalised number from JID, status — one of domain.AccountStatus* constants.
	OnStatusChange func(phone, status string)
	// ProxyLookup optionally returns the proxy to use for a given phone.
	ProxyLookup ProxyLookup
}

func NewManager(sessionDBPath string) *Manager {
	return &Manager{
		sessionDBPath:  sessionDBPath,
		clientsByPhone: make(map[string]*whatsmeow.Client),
	}
}

func (m *Manager) GetContainer(ctx context.Context, logger waLog.Logger) (*sqlstore.Container, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.container != nil {
		return m.container, nil
	}
	container, err := sqlstore.New(ctx, "sqlite3", m.sessionDBPath, logger)
	if err != nil {
		return nil, err
	}
	m.container = container
	return container, nil
}

func (m *Manager) SessionDBPath() string {
	return m.sessionDBPath
}

func (m *Manager) Start(ctx context.Context) error {
	container, err := m.GetContainer(ctx, waLog.Stdout("Database", "INFO", true))
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

		// Apply proxy if one is assigned to this phone
		if m.ProxyLookup != nil && phone != "" {
			if proxy, err := m.ProxyLookup(ctx, phone); err != nil {
				log.Printf("proxy lookup for %s: %v (continuing without proxy)", phone, err)
			} else if proxy != nil {
				proxyURL := proxy.URL()
				if err := client.SetProxyAddress(proxyURL); err != nil {
					log.Printf("set proxy for %s: %v", phone, err)
				} else {
					log.Printf("proxy set for %s: %s://%s:%d", phone, proxy.ProxyType, proxy.Host, proxy.Port)
				}
			}
		}

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

// ConnectDevice регистрирует и подключает новое устройство в живой пул менеджера.
func (m *Manager) ConnectDevice(ctx context.Context, device *store.Device) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	phone := ""
	if device.ID != nil {
		phone = normalizePhone(device.ID.User)
	}

	// Если сессия для этого телефона уже была активна, отключаем её
	if oldClient, ok := m.clientsByPhone[phone]; ok {
		oldClient.Disconnect()
		for i, c := range m.clients {
			if c == oldClient {
				m.clients = append(m.clients[:i], m.clients[i+1:]...)
				break
			}
		}
	}

	clientLog := waLog.Stdout("Client", "INFO", true)
	client := whatsmeow.NewClient(device, clientLog)
	client.AddEventHandler(makeEventHandler(phone, m.OnStatusChange))

	// Apply proxy if one is assigned to this phone
	if m.ProxyLookup != nil && phone != "" {
		if proxy, err := m.ProxyLookup(ctx, phone); err != nil {
			log.Printf("proxy lookup for %s: %v (continuing without proxy)", phone, err)
		} else if proxy != nil {
			proxyURL := proxy.URL()
			if err := client.SetProxyAddress(proxyURL); err != nil {
				log.Printf("set proxy for %s: %v", phone, err)
			} else {
				log.Printf("proxy set for %s (dynamic): %s://%s:%d", phone, proxy.ProxyType, proxy.Host, proxy.Port)
			}
		}
	}

	if err := client.Connect(); err != nil {
		return fmt.Errorf("connect whatsapp client dynamically: %w", err)
	}

	m.clients = append(m.clients, client)
	if phone != "" {
		m.clientsByPhone[phone] = client
	}

	log.Printf("Dynamic client registered and connected for phone: %s", phone)
	return nil
}

// EnsureClientConnected проверяет, есть ли активная сессия в памяти.
// Если нет, но сессия существует в multi.db, загружает и запускает её.
func (m *Manager) EnsureClientConnected(ctx context.Context, phone string) error {
	normalized := normalizePhone(phone)

	m.mu.RLock()
	_, exists := m.clientsByPhone[normalized]
	m.mu.RUnlock()

	if exists {
		return nil
	}

	container, err := m.GetContainer(ctx, waLog.Noop)
	if err != nil {
		return err
	}

	devices, err := container.GetAllDevices(ctx)
	if err != nil {
		return err
	}

	var matchDevice *store.Device
	for _, dev := range devices {
		if dev.ID != nil && normalizePhone(dev.ID.User) == normalized {
			matchDevice = dev
			break
		}
	}

	if matchDevice == nil {
		return fmt.Errorf("device for phone %s not found in store", phone)
	}

	return m.ConnectDevice(ctx, matchDevice)
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
