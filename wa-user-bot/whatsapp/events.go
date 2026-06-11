package whatsapp

import (
	"log"

	"my-whatsapp-bot/wa-user-bot/domain"

	"go.mau.fi/whatsmeow/types/events"
)

// eventHandler используется только в процессе авторизации (QR).
func eventHandler(evt interface{}) {
	switch value := evt.(type) {
	case *events.Message:
		log.Printf("received message: %s", value.Message.GetConversation())
	}
}

// makeEventHandler создаёт обработчик для конкретного аккаунта.
// onStatusChange вызывается при изменении состояния подключения.
func makeEventHandler(phone string, onStatusChange func(phone, status string)) func(interface{}) {
	return func(evt interface{}) {
		switch value := evt.(type) {
		case *events.Message:
			log.Printf("[%s] received message: %s", phone, value.Message.GetConversation())
		case *events.Connected:
			log.Printf("[%s] connected", phone)
			if onStatusChange != nil {
				onStatusChange(phone, domain.AccountStatusReady)
			}
		case *events.Disconnected:
			log.Printf("[%s] disconnected", phone)
			if onStatusChange != nil {
				onStatusChange(phone, domain.AccountStatusDisconnected)
			}
		case *events.LoggedOut:
			log.Printf("[%s] logged out (reason: %v)", phone, value.Reason)
			if onStatusChange != nil {
				onStatusChange(phone, domain.AccountStatusBlocked)
			}
		}
	}
}
