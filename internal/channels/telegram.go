package channels

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yifanes/miniclawd/internal/agent"
	"github.com/yifanes/miniclawd/internal/config"
	"github.com/yifanes/miniclawd/internal/core"
	"github.com/yifanes/miniclawd/internal/runcontrol"
	"github.com/yifanes/miniclawd/internal/storage"
	"github.com/yifanes/miniclawd/internal/turnqueue"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// TelegramAccount holds a single Telegram bot account's runtime state.
type TelegramAccount struct {
	Name           string
	Bot            *tgbotapi.BotAPI
	BotUsername    string
	AllowedGroups  []int64
	AllowedUserIDs []int64
	ModelOverride  *string
}

// TelegramAdapter implements ChannelAdapter for Telegram (multi-account).
type TelegramAdapter struct {
	accounts map[string]*TelegramAccount
	// defaultAccount is used for SendText/SendAttachment when no account is specified.
	defaultAccount *TelegramAccount
}

// NewTelegramAdapterMulti creates a multi-account Telegram adapter.
func NewTelegramAdapterMulti(configs map[string]config.TelegramAccountConfig) (*TelegramAdapter, error) {
	accounts := make(map[string]*TelegramAccount)
	var defaultAcct *TelegramAccount

	for name, cfg := range configs {
		if cfg.BotToken == "" {
			continue
		}
		bot, err := tgbotapi.NewBotAPI(cfg.BotToken)
		if err != nil {
			return nil, fmt.Errorf("creating telegram bot for account %q: %w", name, err)
		}
		username := cfg.BotUsername
		if username == "" {
			username = bot.Self.UserName
		}
		acct := &TelegramAccount{
			Name:           name,
			Bot:            bot,
			BotUsername:    username,
			AllowedGroups:  cfg.AllowedGroups,
			AllowedUserIDs: cfg.AllowedUserIDs,
			ModelOverride:  cfg.Model,
		}
		accounts[name] = acct
		if defaultAcct == nil {
			defaultAcct = acct
		}
	}
	if len(accounts) == 0 {
		return nil, fmt.Errorf("no valid telegram accounts configured")
	}
	return &TelegramAdapter{
		accounts:       accounts,
		defaultAccount: defaultAcct,
	}, nil
}

// NewTelegramAdapter creates a single-account adapter (legacy compatibility).
func NewTelegramAdapter(token, botUsername string, allowedGroups, allowedUserIDs []int64, topicRouting bool) (*TelegramAdapter, error) {
	return NewTelegramAdapterMulti(map[string]config.TelegramAccountConfig{
		"default": {
			Enabled:        true,
			BotToken:       token,
			BotUsername:    botUsername,
			AllowedGroups:  allowedGroups,
			AllowedUserIDs: allowedUserIDs,
		},
	})
}

func (a *TelegramAdapter) Name() string { return "telegram" }

func (a *TelegramAdapter) ChatTypeRoutes() map[string]ConversationKind {
	return map[string]ConversationKind{
		"telegram_private":    Private,
		"telegram_group":      Group,
		"telegram_supergroup": Group,
		"telegram_channel":    Group,
	}
}

func (a *TelegramAdapter) IsLocalOnly() bool     { return false }
func (a *TelegramAdapter) AllowsCrossChat() bool { return true }

// parseTelegramExternalChatID parses "chatID" or "chatID:threadID" format.
func parseTelegramExternalChatID(externalChatID string) (chatID int64, threadID int, hasThread bool) {
	if idx := strings.IndexByte(externalChatID, ':'); idx >= 0 {
		fmt.Sscanf(externalChatID[:idx], "%d", &chatID)
		fmt.Sscanf(externalChatID[idx+1:], "%d", &threadID)
		hasThread = threadID != 0
		return
	}
	fmt.Sscanf(externalChatID, "%d", &chatID)
	return
}

func (a *TelegramAdapter) SendText(ctx context.Context, externalChatID, text string) error {
	if a.defaultAccount == nil || a.defaultAccount.Bot == nil {
		return fmt.Errorf("telegram: no account configured")
	}
	bot := a.defaultAccount.Bot
	chatID, threadID, hasThread := parseTelegramExternalChatID(externalChatID)

	chunks := core.SplitText(text, 4096)
	for _, chunk := range chunks {
		msg := tgbotapi.NewMessage(chatID, chunk)
		msg.ParseMode = "MarkdownV2"
		if hasThread {
			msg.BaseChat.ReplyToMessageID = threadID
		}
		if _, err := bot.Send(msg); err != nil {
			// Fallback to plain text.
			msg.ParseMode = ""
			msg.Text = chunk
			if _, err := bot.Send(msg); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *TelegramAdapter) SendAttachment(ctx context.Context, externalChatID, filePath string, caption *string) (string, error) {
	if a.defaultAccount == nil || a.defaultAccount.Bot == nil {
		return "", fmt.Errorf("telegram: no account configured")
	}
	bot := a.defaultAccount.Bot
	chatID, _, _ := parseTelegramExternalChatID(externalChatID)

	doc := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(filePath))
	if caption != nil {
		doc.Caption = *caption
	}
	_, err := bot.Send(doc)
	if err != nil {
		return "", err
	}
	return filePath, nil
}

// StartTelegramBot runs the Telegram long-poll loop for all configured accounts.
func StartTelegramBot(ctx context.Context, adapter *TelegramAdapter, db *storage.Database, deps *agent.AgentDeps, tq *turnqueue.ChatTurnQueue) {
	MarkChannelStarted("telegram")

	if len(adapter.accounts) == 1 {
		// Single account: run blocking in current goroutine.
		for _, acct := range adapter.accounts {
			log.Printf("[telegram] bot @%s started", acct.BotUsername)
			runTelegramPolling(ctx, acct, db, deps, tq)
		}
		return
	}

	// Multi-account: spawn a goroutine per account and wait.
	var wg sync.WaitGroup
	for _, acct := range adapter.accounts {
		wg.Add(1)
		go func(acct *TelegramAccount) {
			defer wg.Done()
			log.Printf("[telegram] bot @%s (account %s) started", acct.BotUsername, acct.Name)
			runTelegramPolling(ctx, acct, db, deps, tq)
		}(acct)
	}
	wg.Wait()
}

func runTelegramPolling(ctx context.Context, acct *TelegramAccount, db *storage.Database, deps *agent.AgentDeps, tq *turnqueue.ChatTurnQueue) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := acct.Bot.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			return
		case update := <-updates:
			if update.Message == nil {
				continue
			}
			go handleTelegramMessage(ctx, acct, db, deps, tq, update.Message)
		}
	}
}

func handleTelegramMessage(ctx context.Context, acct *TelegramAccount, db *storage.Database, deps *agent.AgentDeps, tq *turnqueue.ChatTurnQueue, msg *tgbotapi.Message) {
	// Startup guard: drop stale/duplicate messages.
	msgID := FormatTelegramMsgID(msg.MessageID)
	if ShouldDropPreStartMessage("telegram", msgID, TelegramMessageTimeMs(msg.Date)) {
		return
	}
	if ShouldDropRecentDuplicate("telegram", msgID) {
		return
	}

	// Determine chat type.
	chatType := "telegram_private"
	switch msg.Chat.Type {
	case "group":
		chatType = "telegram_group"
	case "supergroup":
		chatType = "telegram_supergroup"
	case "channel":
		chatType = "telegram_channel"
	}

	externalID := fmt.Sprintf("%d", msg.Chat.ID)
	// Note: Topic routing (forum thread support) requires go-telegram-bot-api with
	// MessageThreadID field. Currently deferred until library upgrade.
	title := msg.Chat.Title
	if title == "" && msg.Chat.FirstName != "" {
		title = msg.Chat.FirstName
		if msg.Chat.LastName != "" {
			title += " " + msg.Chat.LastName
		}
	}

	// Resolve chat ID.
	chatID, err := db.ResolveOrCreateChatID("telegram", externalID, &title, chatType)
	if err != nil {
		log.Printf("[telegram] resolve chat error: %v", err)
		return
	}

	// Handle slash commands.
	if msg.IsCommand() {
		switch msg.Command() {
		case "reset":
			db.ClearChatContext(chatID)
			reply := tgbotapi.NewMessage(msg.Chat.ID, "Context cleared.")
			acct.Bot.Send(reply)
			return
		case "usage":
			summary, _ := db.GetLLMUsageSummary(chatID)
			text := fmt.Sprintf("Requests: %d\nInput: %d tokens\nOutput: %d tokens\nTotal: %d tokens",
				summary.Requests, summary.InputTokens, summary.OutputTokens, summary.TotalTokens)
			reply := tgbotapi.NewMessage(msg.Chat.ID, text)
			acct.Bot.Send(reply)
			return
		case "skills":
			reply := tgbotapi.NewMessage(msg.Chat.ID, "Skills: "+deps.Skills)
			acct.Bot.Send(reply)
			return
		case "archive":
			messages, _, _, _ := agent.LoadSession(db, chatID)
			if messages != nil && len(messages) > 0 {
				agent.ArchiveConversation(deps.Config.DataDir, "telegram", chatID, messages)
				db.ClearChatContext(chatID)
				reply := tgbotapi.NewMessage(msg.Chat.ID, "Conversation archived and context cleared.")
				acct.Bot.Send(reply)
			} else {
				reply := tgbotapi.NewMessage(msg.Chat.ID, "No active session to archive.")
				acct.Bot.Send(reply)
			}
			return
		case "stop":
			stopped := runcontrol.AbortRuns("telegram", chatID)
			var text string
			if stopped > 0 {
				text = fmt.Sprintf("Stopping current run (%d active).", stopped)
			} else {
				text = "No active run in this chat."
			}
			reply := tgbotapi.NewMessage(msg.Chat.ID, text)
			acct.Bot.Send(reply)
			return
		case "clear":
			db.ClearChatContext(chatID)
			reply := tgbotapi.NewMessage(msg.Chat.ID, "Context cleared (session + chat history, scheduled tasks kept).")
			acct.Bot.Send(reply)
			return
		case "status":
			summary, _ := db.GetLLMUsageSummary(chatID)
			text := fmt.Sprintf("Provider: %s\nModel: %s\nRequests: %d\nTokens: %d (in: %d, out: %d)",
				deps.LLM.ProviderName(), deps.LLM.ModelName(),
				summary.Requests, summary.TotalTokens, summary.InputTokens, summary.OutputTokens)
			reply := tgbotapi.NewMessage(msg.Chat.ID, text)
			acct.Bot.Send(reply)
			return
		}
	}

	// Extract content.
	content := msg.Text
	if content == "" && msg.Caption != "" {
		content = msg.Caption
	}
	hasPhoto := msg.Photo != nil && len(msg.Photo) > 0
	if content == "" && !hasPhoto {
		return
	}
	if content == "" && hasPhoto {
		content = "请描述这张图片"
	}

	senderName := msg.From.FirstName
	if msg.From.LastName != "" {
		senderName += " " + msg.From.LastName
	}

	// Store message.
	log.Printf("[telegram] chat %d (%s) from %s: %s", chatID, chatType, senderName, truncate(content, 200))
	db.StoreMessage(storage.StoredMessage{
		ID:         fmt.Sprintf("tg_%d", msg.MessageID),
		ChatID:     chatID,
		SenderName: senderName,
		Content:    content,
		IsFromBot:  false,
		Timestamp:  time.Unix(int64(msg.Date), 0).UTC().Format(time.RFC3339),
	})

	// Check allowed groups.
	if len(acct.AllowedGroups) > 0 && chatType != "telegram_private" {
		allowed := false
		for _, gid := range acct.AllowedGroups {
			if gid == msg.Chat.ID {
				allowed = true
				break
			}
		}
		if !allowed {
			return
		}
	}

	// Check allowed user IDs for DMs.
	if len(acct.AllowedUserIDs) > 0 && chatType == "telegram_private" {
		allowed := false
		for _, uid := range acct.AllowedUserIDs {
			if uid == msg.From.ID {
				allowed = true
				break
			}
		}
		if !allowed {
			return
		}
	}

	// Determine if we should respond.
	shouldRespond := chatType == "telegram_private"
	if !shouldRespond {
		// Check for @mention in groups.
		mention := "@" + acct.BotUsername
		if strings.Contains(content, mention) {
			shouldRespond = true
			content = strings.ReplaceAll(content, mention, "")
			content = strings.TrimSpace(content)
		}
	}

	if !shouldRespond {
		return
	}

	// Turn queue: serialize agent runs per chat.
	guard, acquired := tq.TryAcquire("telegram", chatID, &turnqueue.PendingMessage{
		SenderName: senderName,
		Content:    content,
		MessageID:  msgID,
		Timestamp:  time.Unix(int64(msg.Date), 0).UTC().Format(time.RFC3339),
	})
	if !acquired {
		return // message enqueued, will be processed after the current run
	}
	defer guard.Release()

	// Check for photo.
	var imageData *agent.ImageData
	if msg.Photo != nil && len(msg.Photo) > 0 {
		photo := msg.Photo[len(msg.Photo)-1]
		var imgErr error
		imageData, imgErr = downloadTelegramPhoto(acct.Bot, photo.FileID)
		if imgErr != nil {
			reply := tgbotapi.NewMessage(msg.Chat.ID, imgErr.Error())
			acct.Bot.Send(reply)
			return
		}
	}

	turn := &telegramTurn{
		ctx:            ctx,
		acct:           acct,
		db:             db,
		deps:           deps,
		chatID:         chatID,
		chatType:       chatType,
		telegramChatID: msg.Chat.ID,
		msgID:          msgID,
	}
	turn.run(nil, imageData)

	// Drain messages that arrived while this turn was running so they are not
	// silently dropped. They are already stored in the DB, so they are passed as
	// an override prompt instead of being re-appended from history.
	for ctx.Err() == nil {
		pending := tq.DrainPending("telegram", chatID)
		if len(pending) == 0 {
			return
		}
		merged := joinPendingMessages(pending)
		log.Printf("[telegram] chat %d: processing %d queued message(s)", chatID, len(pending))
		turn.run(&merged, nil)
	}
}

// telegramTurn runs one agent turn for a Telegram chat.
type telegramTurn struct {
	ctx            context.Context
	acct           *TelegramAccount
	db             *storage.Database
	deps           *agent.AgentDeps
	chatID         int64
	chatType       string
	telegramChatID int64
	msgID          string
}

func (t *telegramTurn) run(overridePrompt *string, imageData *agent.ImageData) {
	ctx := t.ctx
	acct := t.acct

	// Start typing indicator.
	typingCtx, cancelTyping := context.WithCancel(ctx)
	defer cancelTyping()
	go func() {
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-typingCtx.Done():
				return
			case <-ticker.C:
				acct.Bot.Send(tgbotapi.NewChatAction(t.telegramChatID, tgbotapi.ChatTyping))
			}
		}
	}()
	acct.Bot.Send(tgbotapi.NewChatAction(t.telegramChatID, tgbotapi.ChatTyping))

	// Register run for cancellation support.
	runID, runCtx, runCancel := runcontrol.RegisterRun(ctx, "telegram", t.chatID, t.msgID)
	defer runCancel()
	defer runcontrol.UnregisterRun("telegram", t.chatID, runID)

	reqCtx := agent.AgentRequestContext{
		CallerChannel: "telegram",
		ChatID:        t.chatID,
		ChatType:      t.chatType,
	}

	response, err := agent.ProcessWithAgent(runCtx, t.deps, reqCtx, overridePrompt, imageData)
	cancelTyping()

	if err != nil {
		log.Printf("[telegram] agent error for chat %d: %v", t.chatID, err)
		acct.Bot.Send(tgbotapi.NewMessage(t.telegramChatID, core.UserFacingError(err)))
		return
	}

	log.Printf("[telegram] chat %d: response (%d chars): %s", t.chatID, len(response), truncate(response, 200))

	for _, chunk := range core.SplitText(response, 4096) {
		reply := tgbotapi.NewMessage(t.telegramChatID, escapeMarkdownV2(chunk))
		reply.ParseMode = "MarkdownV2"
		if _, err := acct.Bot.Send(reply); err != nil {
			// Fallback to plain text.
			reply.ParseMode = ""
			reply.Text = chunk
			acct.Bot.Send(reply)
		}
	}

	t.db.StoreMessage(storage.StoredMessage{
		ID:         fmt.Sprintf("tg_bot_%d", time.Now().UnixNano()),
		ChatID:     t.chatID,
		SenderName: acct.BotUsername,
		Content:    response,
		IsFromBot:  true,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	})
}

var supportedImageTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
}

func downloadTelegramPhoto(bot *tgbotapi.BotAPI, fileID string) (*agent.ImageData, error) {
	file, err := bot.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		return nil, fmt.Errorf("无法获取图片: %v", err)
	}
	url := file.Link(bot.Token)
	resp, err := imageHTTPClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("无法下载图片: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("无法下载图片: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes))
	if err != nil {
		return nil, fmt.Errorf("读取图片失败: %v", err)
	}

	mediaType := "image/jpeg"
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		if idx := strings.IndexByte(ct, ';'); idx >= 0 {
			ct = ct[:idx]
		}
		mediaType = strings.TrimSpace(ct)
	}

	if !supportedImageTypes[mediaType] {
		return nil, fmt.Errorf("不支持的图片格式: %s，请使用 JPEG/PNG/GIF/WebP 格式的图片。", mediaType)
	}

	return &agent.ImageData{
		MediaType: mediaType,
		Base64:    base64.StdEncoding.EncodeToString(data),
	}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// escapeMarkdownV2 converts text to Telegram MarkdownV2 format.
// Fenced code blocks (```) and inline code spans (`) are preserved with their
// content correctly escaped per Telegram spec (only \ and ` inside code).
// All other MarkdownV2 special characters are escaped in regular text regions.
func escapeMarkdownV2(text string) string {
	var buf strings.Builder
	i := 0
	n := len(text)
	for i < n {
		// Fenced code block: ```[lang]\n...\n```
		if i+2 < n && text[i] == '`' && text[i+1] == '`' && text[i+2] == '`' {
			end := strings.Index(text[i+3:], "```")
			if end >= 0 {
				// Per Telegram spec, inside pre/code only \ and ` must be escaped.
				content := text[i+3 : i+3+end]
				content = strings.ReplaceAll(content, `\`, `\\`)
				content = strings.ReplaceAll(content, "`", "\\`")
				buf.WriteString("```")
				buf.WriteString(content)
				buf.WriteString("```")
				i += 3 + end + 3
				continue
			}
			// Unclosed block: escape each backtick individually and move on.
			buf.WriteString("\\`\\`\\`")
			i += 3
			continue
		}
		// Inline code span: `...`
		if text[i] == '`' {
			end := strings.IndexByte(text[i+1:], '`')
			if end >= 0 {
				content := text[i+1 : i+1+end]
				content = strings.ReplaceAll(content, `\`, `\\`)
				content = strings.ReplaceAll(content, "`", "\\`")
				buf.WriteByte('`')
				buf.WriteString(content)
				buf.WriteByte('`')
				i += 2 + end
				continue
			}
			buf.WriteString("\\`")
			i++
			continue
		}
		// Regular text: escape all MarkdownV2 special characters.
		// \ must be escaped first to avoid double-escaping subsequent chars.
		c := text[i]
		switch c {
		case '\\', '_', '*', '[', ']', '(', ')', '~', '>', '#', '+', '-', '=', '|', '{', '}', '.', '!':
			buf.WriteByte('\\')
		}
		buf.WriteByte(c)
		i++
	}
	return buf.String()
}
