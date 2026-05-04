package whatsapp

import (
	"context"
	"fmt"

	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

type Sender struct {
	manager *Manager
}

func NewSender(manager *Manager) *Sender {
	return &Sender{manager: manager}
}

func (s *Sender) SendText(ctx context.Context, senderPhone string, receiverPhone string, text string) error {
	client, ok := s.manager.ClientByPhone(senderPhone)
	if !ok {
		return fmt.Errorf("no active whatsapp session for sender %s", senderPhone)
	}

	recipientJID := types.NewJID(receiverPhone, types.DefaultUserServer)
	message := &waProto.Message{Conversation: proto.String(text)}
	if _, err := client.SendMessage(ctx, recipientJID, message); err != nil {
		return fmt.Errorf("send whatsapp message: %w", err)
	}

	return nil
}
