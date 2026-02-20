package channels

import (
	"context"
	"fmt"
	"sync"
)

// ConversationKind distinguishes private from group chats.
type ConversationKind int

const (
	Private ConversationKind = iota
	Group
)

// ChannelAdapter is the interface that platform adapters implement.
type ChannelAdapter interface {
	Name() string
	ChatTypeRoutes() map[string]ConversationKind
	IsLocalOnly() bool
	AllowsCrossChat() bool
	SendText(ctx context.Context, externalChatID, text string) error
	SendAttachment(ctx context.Context, externalChatID, filePath string, caption *string) (string, error)
}

// ChannelRegistry manages all registered channel adapters.
type ChannelRegistry struct {
	mu                 sync.RWMutex
	adapters           map[string]ChannelAdapter
	typeToChannel      map[string]string           // "telegram_group" → "telegram"
	typeToConversation map[string]ConversationKind  // "telegram_group" → Group
}

func NewChannelRegistry() *ChannelRegistry {
	return &ChannelRegistry{
		adapters:           make(map[string]ChannelAdapter),
		typeToChannel:      make(map[string]string),
		typeToConversation: make(map[string]ConversationKind),
	}
}

// Register adds a channel adapter and its routes.
func (r *ChannelRegistry) Register(adapter ChannelAdapter) {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := adapter.Name()
	r.adapters[name] = adapter

	for chatType, kind := range adapter.ChatTypeRoutes() {
		r.typeToChannel[chatType] = name
		r.typeToConversation[chatType] = kind
	}
}

// Get returns an adapter by channel name.
func (r *ChannelRegistry) Get(name string) ChannelAdapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.adapters[name]
}

// Resolve looks up adapter and conversation kind from a DB chat_type.
func (r *ChannelRegistry) Resolve(dbChatType string) (ChannelAdapter, ConversationKind, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	channelName, ok := r.typeToChannel[dbChatType]
	if !ok {
		return nil, Private, false
	}
	adapter := r.adapters[channelName]
	kind := r.typeToConversation[dbChatType]
	return adapter, kind, true
}

// ResolveRouting returns the channel name and kind without the adapter.
func (r *ChannelRegistry) ResolveRouting(dbChatType string) (channelName string, kind ConversationKind, ok bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	channelName, ok = r.typeToChannel[dbChatType]
	if !ok {
		return "", Private, false
	}
	kind = r.typeToConversation[dbChatType]
	return channelName, kind, true
}

// ChatRouting holds resolved routing for a chat.
type ChatRouting struct {
	ChannelName    string
	Kind           ConversationKind
	Adapter        ChannelAdapter
	ExternalChatID string
}

// GetChatRouting resolves full routing for a chat ID.
func (r *ChannelRegistry) GetChatRouting(db ChatTypeResolver, chatID int64) (*ChatRouting, error) {
	chatType, err := db.GetChatType(chatID)
	if err != nil {
		return nil, fmt.Errorf("getting chat type: %w", err)
	}
	if chatType == "" {
		return nil, fmt.Errorf("chat %d not found", chatID)
	}

	adapter, kind, ok := r.Resolve(chatType)
	if !ok {
		return nil, fmt.Errorf("no adapter for chat type %q", chatType)
	}

	channel, externalID, err := db.GetChatExternalID(chatID)
	if err != nil {
		return nil, fmt.Errorf("getting external ID: %w", err)
	}
	_ = channel

	return &ChatRouting{
		ChannelName:    adapter.Name(),
		Kind:           kind,
		Adapter:        adapter,
		ExternalChatID: externalID,
	}, nil
}

// ChatTypeResolver is the DB interface needed by routing.
type ChatTypeResolver interface {
	GetChatType(chatID int64) (string, error)
	GetChatExternalID(chatID int64) (channel, externalID string, err error)
}

// SessionSourceForChat returns the channel name from a chat_type string.
func SessionSourceForChat(chatType string) string {
	if chatType == "" {
		return "unknown"
	}
	// Strip suffixes like "_group", "_dm", "_private"
	for _, suffix := range []string{"_group", "_dm", "_private", "_supergroup", "_channel"} {
		if idx := len(chatType) - len(suffix); idx > 0 && chatType[idx:] == suffix {
			return chatType[:idx]
		}
	}
	return chatType
}
