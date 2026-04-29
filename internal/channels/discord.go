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
	"github.com/yifanes/miniclawd/internal/config"
	"github.com/yifanes/miniclawd/internal/core"
	"github.com/yifanes/miniclawd/internal/runcontrol"
	"github.com/yifanes/miniclawd/internal/storage"
	"github.com/yifanes/miniclawd/internal/turnqueue"
)

// DiscordAccount holds a single Discord bot account's runtime state.
type DiscordAccount struct {
	Name            string
	Session         *discordgo.Session
	AllowedChannels []uint64
	NoMention       bool
	ModelOverride   *string
}

// DiscordAdapter implements ChannelAdapter for Discord (multi-account).
type DiscordAdapter struct {
	accounts       map[string]*DiscordAccount
	defaultAccount *DiscordAccount
}

// NewDiscordAdapterMulti creates a multi-account Discord adapter.
func NewDiscordAdapterMulti(configs map[string]config.DiscordAccountConfig) (*DiscordAdapter, error) {
	accounts := make(map[string]*DiscordAccount)
	var defaultAcct *DiscordAccount

	for name, cfg := range configs {
		if cfg.BotToken == "" {
			continue
		}
		s, err := discordgo.New("Bot " + cfg.BotToken)
		if err != nil {
			return nil, fmt.Errorf("creating discord session for account %q: %w", name, err)
		}
		s.Identify.Intents = discordgo.IntentsGuildMessages |
			discordgo.IntentsDirectMessages |
			discordgo.IntentsMessageContent
		acct := &DiscordAccount{
			Name:            name,
			Session:         s,
			AllowedChannels: cfg.AllowedChannels,
			NoMention:       cfg.NoMention,
			ModelOverride:   cfg.Model,
		}
		accounts[name] = acct
		if defaultAcct == nil {
			defaultAcct = acct
		}
	}
	if len(accounts) == 0 {
		return nil, fmt.Errorf("no valid discord accounts configured")
	}
	return &DiscordAdapter{
		accounts:       accounts,
		defaultAccount: defaultAcct,
	}, nil
}

// NewDiscordAdapter creates a single-account adapter (legacy compatibility).
func NewDiscordAdapter(token string, allowedChannels []uint64, noMention bool) (*DiscordAdapter, error) {
	return NewDiscordAdapterMulti(map[string]config.DiscordAccountConfig{
		"default": {
			Enabled:         true,
			BotToken:        token,
			AllowedChannels: allowedChannels,
			NoMention:       noMention,
		},
	})
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
	s := a.defaultAccount.Session
	chunks := core.SplitText(text, 2000)
	for _, chunk := range chunks {
		if _, err := s.ChannelMessageSend(externalChatID, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (a *DiscordAdapter) SendAttachment(ctx context.Context, externalChatID, filePath string, caption *string) (string, error) {
	s := a.defaultAccount.Session
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	captionText := ""
	if caption != nil {
		captionText = *caption
	}
	_, err = s.ChannelFileSendWithMessage(externalChatID, captionText, filepath.Base(filePath), f)
	if err != nil {
		return "", err
	}
	return filePath, nil
}

// StartDiscordBot opens the Discord WebSocket for all accounts and blocks until ctx is cancelled.
func StartDiscordBot(ctx context.Context, adapter *DiscordAdapter, db *storage.Database, deps *agent.AgentDeps, tq *turnqueue.ChatTurnQueue) error {
	MarkChannelStarted("discord")

	for _, acct := range adapter.accounts {
		acctRef := acct
		acctRef.Session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
			if m.Author == nil || m.Author.Bot {
				return
			}
			go handleDiscordMessage(ctx, acctRef, db, deps, tq, s, m.Message)
		})

		if err := acctRef.Session.Open(); err != nil {
			return fmt.Errorf("opening discord websocket for account %q: %w", acctRef.Name, err)
		}
		log.Printf("[discord] bot @%s (account %s) started", acctRef.Session.State.User.Username, acctRef.Name)
	}

	<-ctx.Done()

	// Close all sessions.
	for _, acct := range adapter.accounts {
		acct.Session.Close()
	}
	return nil
}

func handleDiscordMessage(ctx context.Context, acct *DiscordAccount, db *storage.Database, deps *agent.AgentDeps, tq *turnqueue.ChatTurnQueue, s *discordgo.Session, msg *discordgo.Message) {
	// Startup guard: drop stale/duplicate messages.
	msgID := FormatDiscordMsgID(msg.ID)
	if ShouldDropRecentDuplicate("discord", msgID) {
		return
	}

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
	if chatType == "discord_guild" && len(acct.AllowedChannels) > 0 {
		var channelID uint64
		fmt.Sscanf(msg.ChannelID, "%d", &channelID)
		allowed := false
		for _, id := range acct.AllowedChannels {
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
			case "/stop":
				stopped := runcontrol.AbortRuns("discord", chatID)
				if stopped > 0 {
					s.ChannelMessageSend(msg.ChannelID, fmt.Sprintf("Stopping current run (%d active).", stopped))
				} else {
					s.ChannelMessageSend(msg.ChannelID, "No active run in this chat.")
				}
				return
			case "/clear":
				db.ClearChatContext(chatID)
				s.ChannelMessageSend(msg.ChannelID, "Context cleared (session + chat history, scheduled tasks kept).")
				return
			case "/status":
				summary, _ := db.GetLLMUsageSummary(chatID)
				text := fmt.Sprintf("Provider: %s\nModel: %s\nRequests: %d\nTokens: %d (in: %d, out: %d)",
					deps.LLM.ProviderName(), deps.LLM.ModelName(),
					summary.Requests, summary.TotalTokens, summary.InputTokens, summary.OutputTokens)
				s.ChannelMessageSend(msg.ChannelID, text)
				return
			}
		}
	}

	// In guild channels, require @mention unless discord_no_mention is set.
	botID := s.State.User.ID
	if chatType == "discord_guild" && !acct.NoMention {
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

	// Turn queue: serialize agent runs per chat.
	guard, acquired := tq.TryAcquire("discord", chatID, &turnqueue.PendingMessage{
		SenderName: senderName,
		Content:    content,
		MessageID:  msgID,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	})
	if !acquired {
		return // message enqueued, will be processed after current run
	}
	defer guard.Release()

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

	// Register run for cancellation support.
	runID, runCtx, runCancel := runcontrol.RegisterRun(ctx, "discord", chatID, msgID)
	defer runCancel()
	defer runcontrol.UnregisterRun("discord", chatID, runID)

	reqCtx := agent.AgentRequestContext{
		CallerChannel: "discord",
		ChatID:        chatID,
		ChatType:      chatType,
	}

	response, err := agent.ProcessWithAgent(runCtx, deps, reqCtx, nil, imageData)
	cancelTyping()

	if err != nil {
		log.Printf("[discord] agent error for chat %d: %v", chatID, err)
		s.ChannelMessageSend(msg.ChannelID, core.UserFacingError(err))
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
