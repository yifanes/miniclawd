package channels

import "context"

// WebAdapter implements ChannelAdapter for the built-in web UI.
type WebAdapter struct{}

func NewWebAdapter() *WebAdapter {
	return &WebAdapter{}
}

func (a *WebAdapter) Name() string { return "web" }

func (a *WebAdapter) ChatTypeRoutes() map[string]ConversationKind {
	return map[string]ConversationKind{
		"web": Private,
	}
}

func (a *WebAdapter) IsLocalOnly() bool    { return true }
func (a *WebAdapter) AllowsCrossChat() bool { return false }

func (a *WebAdapter) SendText(_ context.Context, externalChatID, text string) error {
	// Web messages are delivered via SSE, not push. No-op here.
	return nil
}

func (a *WebAdapter) SendAttachment(_ context.Context, externalChatID, filePath string, caption *string) (string, error) {
	return "", nil
}
