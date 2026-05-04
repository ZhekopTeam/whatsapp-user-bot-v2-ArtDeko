package whatsapp

import (
	"log"

	"go.mau.fi/whatsmeow/types/events"
)

func eventHandler(evt interface{}) {
	switch value := evt.(type) {
	case *events.Message:
		log.Printf("received message: %s", value.Message.GetConversation())
	}
}
