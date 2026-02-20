package app

import (
	"context"

	"github.com/yifanes/miniclawd/internal/channels"
	"github.com/yifanes/miniclawd/internal/storage"
)

// registrySender bridges the ChannelRegistry to the tools.ChannelSender interface,
// translating internal chat IDs to external IDs via the database.
type registrySender struct {
	registry *channels.ChannelRegistry
	db       *storage.Database
}

func (s *registrySender) SendText(ctx context.Context, chatID int64, text string) error {
	routing, err := s.registry.GetChatRouting(s.db, chatID)
	if err != nil {
		return err
	}
	return routing.Adapter.SendText(ctx, routing.ExternalChatID, text)
}

func (s *registrySender) SendAttachment(ctx context.Context, chatID int64, filePath string, caption *string) (string, error) {
	routing, err := s.registry.GetChatRouting(s.db, chatID)
	if err != nil {
		return "", err
	}
	return routing.Adapter.SendAttachment(ctx, routing.ExternalChatID, filePath, caption)
}

func (s *registrySender) IsLocalOnly(chatID int64) bool {
	routing, err := s.registry.GetChatRouting(s.db, chatID)
	if err != nil {
		return false
	}
	return routing.Adapter.IsLocalOnly()
}
