package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yifanes/miniclawd/internal/core"
	"github.com/yifanes/miniclawd/internal/storage"
)

type ExportChatTool struct {
	db      *storage.Database
	dataDir string
}

func NewExportChatTool(db *storage.Database, dataDir string) *ExportChatTool {
	return &ExportChatTool{db: db, dataDir: dataDir}
}

func (t *ExportChatTool) Name() string { return "export_chat" }

func (t *ExportChatTool) Definition() core.ToolDefinition {
	return MakeDef("export_chat",
		"Export a chat's message history to a markdown file.",
		map[string]any{
			"chat_id": IntProp("Chat ID to export"),
			"path":    StringProp("Output file path (default: data_dir/exports/{chat_id}_{timestamp}.md)"),
		},
		[]string{"chat_id"},
	)
}

func (t *ExportChatTool) Execute(_ context.Context, input json.RawMessage) ToolResult {
	var params struct {
		ChatID int64   `json:"chat_id"`
		Path   *string `json:"path"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}

	auth := ExtractAuthContext(input)
	if auth != nil && !auth.CanAccessChat(params.ChatID) {
		return Error("you don't have access to this chat")
	}

	messages, err := t.db.GetAllMessages(params.ChatID)
	if err != nil {
		return Error(fmt.Sprintf("database error: %v", err))
	}

	if len(messages) == 0 {
		return Success("No messages found for this chat.")
	}

	outputPath := ""
	if params.Path != nil && *params.Path != "" {
		outputPath = *params.Path
	} else {
		ts := time.Now().UTC().Format("20060102_150405")
		outputPath = filepath.Join(t.dataDir, "exports", fmt.Sprintf("%d_%s.md", params.ChatID, ts))
	}

	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Error(fmt.Sprintf("cannot create directory: %v", err))
	}

	var sb strings.Builder
	for _, msg := range messages {
		sb.WriteString(fmt.Sprintf("**%s** (%s)\n\n%s\n\n---\n\n", msg.SenderName, msg.Timestamp, msg.Content))
	}

	if err := os.WriteFile(outputPath, []byte(sb.String()), 0o644); err != nil {
		return Error(fmt.Sprintf("write error: %v", err))
	}

	return Success(fmt.Sprintf("Exported %d messages to %s", len(messages), outputPath))
}
