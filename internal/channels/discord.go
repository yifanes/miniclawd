package channels

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/yifanes/miniclawd/internal/agent"
	"github.com/yifanes/miniclawd/internal/core"
	"github.com/yifanes/miniclawd/internal/storage"
)

// DiscordAdapter implements ChannelAdapter for Discord.
type DiscordAdapter struct {
	session         *discordgo.Session
	allowedChannels []uint64
	noMention       bool
}

// NewDiscordAdapter creates a new Discord bot session.
func NewDiscordAdapter(token string, allowedChannels []uint64, noMention bool) (*DiscordAdapter, error) {
	s, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("creating discord session: %w", err)
	}
	s.Identify.Intents = discordgo.IntentsGuildMessages |
		discordgo.IntentsDirectMessages |
		discordgo.IntentsMessageContent
	return &DiscordAdapter{
		session:         s,
		allowedChannels: allowedChannels,
		noMention:       noMention,
	}, nil
}

func (a *DiscordAdapter) Name() string { return "discord" }

func (a *DiscordAdapter) ChatTypeRoutes() map[string]ConversationKind {
	return map[string]ConversationKind{
		"discord_dm":    Private,
		"discord_guild": Group,
	}
}

func (a *DiscordAdapter) IsLocalOnly() bool    { return false }
func (a *DiscordAdapter) AllowsCrossChat() bool { return true }

func (a *DiscordAdapter) SendText(ctx context.Context, externalChatID, text string) error {
	chunks := core.SplitText(text, 2000)
	for _, chunk := range chunks {
		if _, err := a.session.ChannelMessageSend(externalChatID, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (a *DiscordAdapter) SendAttachment(ctx context.Context, externalChatID, filePath string, caption *string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	captionText := ""
	if caption != nil {
		captionText = *caption
	}
	_, err = a.session.ChannelFileSendWithMessage(externalChatID, captionText, filepath.Base(filePath), f)
	if err != nil {
		return "", err
	}
	return filePath, nil
}

// StartDiscordBot opens the Discord WebSocket and blocks until ctx is cancelled.
func StartDiscordBot(ctx context.Context, adapter *DiscordAdapter, db *storage.Database, deps *agent.AgentDeps) error {
	adapter.session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if m.Author == nil || m.Author.Bot {
			return
		}
		go handleDiscordMessage(ctx, adapter, db, deps, s, m.Message)
	})

	if err := adapter.session.Open(); err != nil {
		return fmt.Errorf("opening discord websocket: %w", err)
	}

	log.Printf("[discord] bot @%s started", adapter.session.State.User.Username)
	<-ctx.Done()
	return adapter.session.Close()
}

func handleDiscordMessage(ctx context.Context, adapter *DiscordAdapter, db *storage.Database, deps *agent.AgentDeps, s *discordgo.Session, msg *discordgo.Message) {
	// Determine chat type.
	chatType := "discord_guild"
	if msg.GuildID == "" {
		chatType = "discord_dm"
	}

	externalID := msg.ChannelID
	title := msg.ChannelID
	if ch, err := s.Channel(msg.ChannelID); err == nil && ch.Name != "" {
		title = ch.Name
	}
	if chatType == "discord_dm" {
		title = msg.Author.Username
	}

	chatID, err := db.ResolveOrCreateChatID("discord", externalID, &title, chatType)
	if err != nil {
		log.Printf("[discord] resolve chat error: %v", err)
		return
	}

	content := msg.Content

	// Filter by allowed channels (guild only).
	if chatType == "discord_guild" && len(adapter.allowedChannels) > 0 {
		var channelID uint64
		fmt.Sscanf(msg.ChannelID, "%d", &channelID)
		allowed := false
		for _, id := range adapter.allowedChannels {
			if id == channelID {
				allowed = true
				break
			}
		}
		if !allowed {
			return
		}
	}

	// Handle slash commands first.
	if strings.HasPrefix(content, "/") {
		parts := strings.Fields(content)
		if len(parts) > 0 {
			switch parts[0] {
			case "/reset":
				db.ClearChatContext(chatID)
				s.ChannelMessageSend(msg.ChannelID, "Context cleared.")
				return
			case "/usage":
				summary, _ := db.GetLLMUsageSummary(chatID)
				text := fmt.Sprintf("Requests: %d\nInput: %d tokens\nOutput: %d tokens\nTotal: %d tokens",
					summary.Requests, summary.InputTokens, summary.OutputTokens, summary.TotalTokens)
				s.ChannelMessageSend(msg.ChannelID, text)
				return
			case "/skills":
				s.ChannelMessageSend(msg.ChannelID, "Skills: "+deps.Skills)
				return
			case "/archive":
				msgs, _, _, _ := agent.LoadSession(db, chatID)
				if len(msgs) > 0 {
					agent.ArchiveConversation(deps.Config.DataDir, "discord", chatID, msgs)
					db.ClearChatContext(chatID)
					s.ChannelMessageSend(msg.ChannelID, "Conversation archived and context cleared.")
				} else {
					s.ChannelMessageSend(msg.ChannelID, "No active session to archive.")
				}
				return
			}
		}
	}

	// In guild channels, require @mention unless discord_no_mention is set.
	botID := s.State.User.ID
	if chatType == "discord_guild" && !adapter.noMention {
		mention1 := "<@" + botID + ">"
		mention2 := "<@!" + botID + ">"
		if !strings.Contains(content, mention1) && !strings.Contains(content, mention2) {
			// Store message silently without responding.
			if content != "" {
				db.StoreMessage(storage.StoredMessage{
					ID:         "dc_" + msg.ID,
					ChatID:     chatID,
					SenderName: msg.Author.Username,
					Content:    content,
					IsFromBot:  false,
					Timestamp:  time.Now().UTC().Format(time.RFC3339),
				})
			}
			return
		}
		// Strip the mention from content.
		content = strings.ReplaceAll(content, mention1, "")
		content = strings.ReplaceAll(content, mention2, "")
		content = strings.TrimSpace(content)
	}

	// Check for image attachments.
	hasImage := false
	for _, att := range msg.Attachments {
		if att != nil && isDiscordImageURL(att.URL) {
			hasImage = true
			break
		}
	}

	if content == "" && !hasImage {
		return
	}
	if content == "" {
		content = "请描述这张图片"
	}

	senderName := msg.Author.Username

	log.Printf("[discord] chat %d (%s) from %s: %s", chatID, chatType, senderName, truncate(content, 200))
	db.StoreMessage(storage.StoredMessage{
		ID:         "dc_" + msg.ID,
		ChatID:     chatID,
		SenderName: senderName,
		Content:    content,
		IsFromBot:  false,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	})

	// Send typing indicator and keep refreshing it.
	typingCtx, cancelTyping := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(8 * time.Second)
		defer ticker.Stop()
		s.ChannelTyping(msg.ChannelID)
		for {
			select {
			case <-typingCtx.Done():
				return
			case <-ticker.C:
				s.ChannelTyping(msg.ChannelID)
			}
		}
	}()

	// Download the first image attachment if present.
	var imageData *agent.ImageData
	for _, att := range msg.Attachments {
		if att != nil && isDiscordImageURL(att.URL) {
			imageData = downloadDiscordImage(att.URL)
			break
		}
	}

	reqCtx := agent.AgentRequestContext{
		CallerChannel: "discord",
		ChatID:        chatID,
		ChatType:      chatType,
	}

	response, err := agent.ProcessWithAgent(ctx, deps, reqCtx, nil, imageData)
	cancelTyping()

	if err != nil {
		log.Printf("[discord] agent error for chat %d: %v", chatID, err)
		s.ChannelMessageSend(msg.ChannelID, "Sorry, I encountered an error processing your message.")
		return
	}

	log.Printf("[discord] chat %d: response (%d chars): %s", chatID, len(response), truncate(response, 200))

	chunks := core.SplitText(response, 2000)
	for _, chunk := range chunks {
		if _, err := s.ChannelMessageSend(msg.ChannelID, chunk); err != nil {
			log.Printf("[discord] send error: %v", err)
		}
	}

	db.StoreMessage(storage.StoredMessage{
		ID:         fmt.Sprintf("dc_bot_%d", time.Now().UnixNano()),
		ChatID:     chatID,
		SenderName: s.State.User.Username,
		Content:    response,
		IsFromBot:  true,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	})
}

func downloadDiscordImage(url string) *agent.ImageData {
	resp, err := http.Get(url)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	mediaType := "image/jpeg"
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		mediaType = ct
	}

	return &agent.ImageData{
		MediaType: mediaType,
		Base64:    base64.StdEncoding.EncodeToString(data),
	}
}

func isDiscordImageURL(url string) bool {
	lower := strings.ToLower(url)
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".gif", ".webp"} {
		if strings.Contains(lower, ext) {
			return true
		}
	}
	return false
}
