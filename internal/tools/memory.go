package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yifanes/miniclawd/internal/core"
	"github.com/yifanes/miniclawd/internal/storage"
)

// ReadMemoryTool reads file-based AGENTS.md memory.
type ReadMemoryTool struct {
	dataDir string
	db      *storage.Database
}

func NewReadMemoryTool(dataDir string, db *storage.Database) *ReadMemoryTool {
	return &ReadMemoryTool{dataDir: dataDir, db: db}
}

func (t *ReadMemoryTool) Name() string { return "read_memory" }

func (t *ReadMemoryTool) Definition() core.ToolDefinition {
	return MakeDef("read_memory",
		"Read the bot's memory file. Use scope 'global' for shared memory or 'chat' for chat-specific memory.",
		map[string]any{
			"scope": EnumProp("Memory scope", []string{"global", "chat"}),
		},
		[]string{"scope"},
	)
}

func (t *ReadMemoryTool) Execute(_ context.Context, input json.RawMessage) ToolResult {
	var params struct {
		Scope string `json:"scope"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}

	auth := ExtractAuthContext(input)

	var path string
	switch params.Scope {
	case "global":
		path = filepath.Join(t.dataDir, "runtime", "groups", "AGENTS.md")
	case "chat":
		if auth == nil {
			return Error("chat scope requires auth context")
		}
		path = filepath.Join(t.dataDir, "runtime", "groups", fmt.Sprintf("%d", auth.CallerChatID), "AGENTS.md")
	default:
		return Error("scope must be 'global' or 'chat'")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Success("No memory file found.")
		}
		return Error(fmt.Sprintf("cannot read memory: %v", err))
	}
	if len(data) == 0 {
		return Success("Memory file is empty.")
	}
	return Success(string(data))
}

// WriteMemoryTool writes file-based AGENTS.md memory.
type WriteMemoryTool struct {
	dataDir string
	db      *storage.Database
}

func NewWriteMemoryTool(dataDir string, db *storage.Database) *WriteMemoryTool {
	return &WriteMemoryTool{dataDir: dataDir, db: db}
}

func (t *WriteMemoryTool) Name() string { return "write_memory" }

func (t *WriteMemoryTool) Definition() core.ToolDefinition {
	return MakeDef("write_memory",
		"Write to the bot's memory file. Global scope requires control chat access.",
		map[string]any{
			"scope":   EnumProp("Memory scope", []string{"global", "chat"}),
			"content": StringProp("Memory content to write"),
		},
		[]string{"scope", "content"},
	)
}

func (t *WriteMemoryTool) Execute(_ context.Context, input json.RawMessage) ToolResult {
	var params struct {
		Scope   string `json:"scope"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}
	if params.Content == "" {
		return Error("content is required")
	}

	auth := ExtractAuthContext(input)

	var path string
	switch params.Scope {
	case "global":
		if auth != nil && !auth.IsControlChat() {
			return Error("only control chats can write global memory")
		}
		path = filepath.Join(t.dataDir, "runtime", "groups", "AGENTS.md")
	case "chat":
		if auth == nil {
			return Error("chat scope requires auth context")
		}
		path = filepath.Join(t.dataDir, "runtime", "groups", fmt.Sprintf("%d", auth.CallerChatID), "AGENTS.md")
	default:
		return Error("scope must be 'global' or 'chat'")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Error(fmt.Sprintf("cannot create directory: %v", err))
	}

	if err := os.WriteFile(path, []byte(params.Content), 0o644); err != nil {
		return Error(fmt.Sprintf("cannot write memory: %v", err))
	}

	// Also insert into DB memories if content is substantial.
	if len(params.Content) >= 90 && t.db != nil && auth != nil {
		chatID := auth.CallerChatID
		t.db.InsertMemoryWithMetadata(&chatID, params.Content, "KNOWLEDGE", "tool", 0.80)
	}

	return Success(fmt.Sprintf("Memory written (%d bytes)", len(params.Content)))
}
