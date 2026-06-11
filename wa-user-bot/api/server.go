package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"my-whatsapp-bot/wa-user-bot/whatsapp"

	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
)

type Server struct {
	manager *whatsapp.Manager
	server  *http.Server
}

func NewServer(manager *whatsapp.Manager, port string) *Server {
	s := &Server{
		manager: manager,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/auth/qr", s.handleAuthQR)
	mux.HandleFunc("/logout", s.handleLogout)
	mux.HandleFunc("/sessions", s.handleSessions)

	s.server = &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	return s
}

func (s *Server) Start(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.server.Shutdown(shutdownCtx)
	}()

	log.Printf("Starting WhatsApp API server on %s", s.server.Addr)
	if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) handleAuthQR(w http.ResponseWriter, r *http.Request) {
	phone := strings.TrimSpace(r.URL.Query().Get("phone"))
	if phone == "" {
		http.Error(w, `{"error":"phone parameter is required"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"error":"streaming not supported"}`, http.StatusInternalServerError)
		return
	}

	encoder := json.NewEncoder(w)
	emit := func(evt whatsapp.QREvent) {
		_ = encoder.Encode(evt)
		flusher.Flush()
	}

	log.Printf("API: Starting WhatsApp auth for phone: %s", phone)
	deviceJID, alreadyExists, err := s.manager.AuthorizeByPhone(r.Context(), phone, waLog.Noop, emit)
	if err != nil {
		emit(whatsapp.QREvent{Type: whatsapp.QREventError, Message: err.Error()})
		return
	}

	if !alreadyExists {
		// Подключаем новое авторизованное устройство в живой пул
		if err := s.manager.EnsureClientConnected(r.Context(), phone); err != nil {
			log.Printf("API: Failed to dynamically connect new device for phone %s: %v", phone, err)
			emit(whatsapp.QREvent{Type: whatsapp.QREventError, Message: fmt.Sprintf("failed to connect device: %v", err)})
			return
		}
		emit(whatsapp.QREvent{Type: whatsapp.QREventSuccess, JID: deviceJID})
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"only POST method is allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	phone := strings.TrimSpace(r.URL.Query().Get("phone"))
	if phone == "" {
		http.Error(w, `{"error":"phone parameter is required"}`, http.StatusBadRequest)
		return
	}

	log.Printf("API: Logging out WhatsApp phone: %s", phone)
	removed, err := s.manager.RemoveSession(r.Context(), phone)
	if err != nil {
		log.Printf("API: Failed to logout phone %s: %v", phone, err)
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"removed": removed})
}

type sessionInfo struct {
	Phone     string `json:"phone"`
	JID       string `json:"jid"`
	Connected bool   `json:"connected"`
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	// Возвращает список всех сессий и их текущее состояние подключения
	container, err := sqlstore.New(r.Context(), "sqlite3", s.manager.SessionDBPath(), waLog.Noop)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	devices, err := container.GetAllDevices(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	sessions := make([]sessionInfo, 0, len(devices))
	for _, dev := range devices {
		if dev.ID == nil {
			continue
		}
		phone := dev.ID.User
		client, isConnected := s.manager.ClientByPhone(phone)
		sessions = append(sessions, sessionInfo{
			Phone:     phone,
			JID:       dev.ID.String(),
			Connected: isConnected && client.IsConnected(),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sessions)
}
