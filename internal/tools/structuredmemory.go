package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yifanes/miniclawd/internal/core"
	"github.com/yifanes/miniclawd/internal/storage"
)

// StructuredMemorySearchTool searches DB-backed memories.
type StructuredMemorySearchTool struct {
	db *storage.Database
}

func NewStructuredMemorySearchTool(db *storage.Database) *StructuredMemorySearchTool {
	return &StructuredMemorySearchTool{db: db}
}

func (t *StructuredMemorySearchTool) Name() string { return "structured_memory_search" }

func (t *StructuredMemorySearchTool) Definition() core.ToolDefinition {
	return MakeDef("structured_memory_search",
		"Search stored memories by keyword. Returns matching memories with IDs for reference.",
		map[string]any{
			"query":            StringProp("Keywords to search for"),
			"limit":            IntProp("Max results (default 10, max 50)"),
			"include_archived": BoolProp("Include archived memories (default false)"),
		},
		[]string{"query"},
	)
}

func (t *StructuredMemorySearchTool) Execute(_ context.Context, input json.RawMessage) ToolResult {
	var params struct {
		Query           string `json:"query"`
		Limit           *int   `json:"limit"`
		IncludeArchived *bool  `json:"include_archived"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}
	if params.Query == "" {
		return Error("query is required")
	}

	auth := ExtractAuthContext(input)
	chatID := int64(0)
	if auth != nil {
		chatID = auth.CallerChatID
	}

	limit := 10
	if params.Limit != nil && *params.Limit > 0 {
		limit = *params.Limit
		if limit > 50 {
			limit = 50
		}
	}

	includeArchived := false
	if params.IncludeArchived != nil {
		includeArchived = *params.IncludeArchived
	}

	memories, err := t.db.SearchMemoriesWithOptions(chatID, params.Query, limit, includeArchived, false)
	if err != nil {
		return Error(fmt.Sprintf("search error: %v", err))
	}

	if len(memories) == 0 {
		return Success("No memories found matching query.")
	}

	var sb strings.Builder
	for i, m := range memories {
		if i > 0 {
			sb.WriteString("\n")
		}
		scope := "chat"
		if m.ChatID == nil {
			scope = "global"
		}
		sb.WriteString(fmt.Sprintf("[id=%d] [%s] [%s] %s", m.ID, m.Category, scope, m.Content))
	}
	return Success(sb.String())
}

// StructuredMemoryDeleteTool deletes (archives) a DB memory.
type StructuredMemoryDeleteTool struct {
	db *storage.Database
}

func NewStructuredMemoryDeleteTool(db *storage.Database) *StructuredMemoryDeleteTool {
	return &StructuredMemoryDeleteTool{db: db}
}

func (t *StructuredMemoryDeleteTool) Name() string { return "structured_memory_delete" }

func (t *StructuredMemoryDeleteTool) Definition() core.ToolDefinition {
	return MakeDef("structured_memory_delete",
		"Delete (archive) a stored memory by ID. Use structured_memory_search to find IDs first.",
		map[string]any{
			"id": IntProp("Memory ID to delete"),
		},
		[]string{"id"},
	)
}

func (t *StructuredMemoryDeleteTool) Execute(_ context.Context, input json.RawMessage) ToolResult {
	var params struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}

	auth := ExtractAuthContext(input)

	mem, err := t.db.GetMemoryByID(params.ID)
	if err != nil {
		return Error(fmt.Sprintf("database error: %v", err))
	}
	if mem == nil {
		return Error("memory not found")
	}

	// Check authorization.
	if auth != nil {
		if mem.ChatID == nil {
			// Global memory: only control chats.
			if !auth.IsControlChat() {
				return Error("only control chats can delete global memories")
			}
		} else if !auth.CanAccessChat(*mem.ChatID) {
			return Error("you don't have access to this memory")
		}
	}

	if err := t.db.ArchiveMemory(params.ID); err != nil {
		return Error(fmt.Sprintf("delete error: %v", err))
	}

	return Success(fmt.Sprintf("Memory %d archived.", params.ID))
}

// StructuredMemoryUpdateTool updates a DB memory.
type StructuredMemoryUpdateTool struct {
	db *storage.Database
}

func NewStructuredMemoryUpdateTool(db *storage.Database) *StructuredMemoryUpdateTool {
	return &StructuredMemoryUpdateTool{db: db}
}

func (t *StructuredMemoryUpdateTool) Name() string { return "structured_memory_update" }

func (t *StructuredMemoryUpdateTool) Definition() core.ToolDefinition {
	return MakeDef("structured_memory_update",
		"Update a stored memory's content and/or category by ID.",
		map[string]any{
			"id":       IntProp("Memory ID to update"),
			"content":  StringProp("New memory content"),
			"category": StringProp("New category (PROFILE, KNOWLEDGE, EVENT)"),
		},
		[]string{"id", "content"},
	)
}

func (t *StructuredMemoryUpdateTool) Execute(_ context.Context, input json.RawMessage) ToolResult {
	var params struct {
		ID       int64   `json:"id"`
		Content  string  `json:"content"`
		Category *string `json:"category"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}
	if params.Content == "" {
		return Error("content is required")
	}

	auth := ExtractAuthContext(input)

	mem, err := t.db.GetMemoryByID(params.ID)
	if err != nil {
		return Error(fmt.Sprintf("database error: %v", err))
	}
	if mem == nil {
		return Error("memory not found")
	}

	// Check authorization.
	if auth != nil {
		if mem.ChatID == nil {
			if !auth.IsControlChat() {
				return Error("only control chats can update global memories")
			}
		} else if !auth.CanAccessChat(*mem.ChatID) {
			return Error("you don't have access to this memory")
		}
	}

	category := mem.Category
	if params.Category != nil {
		category = *params.Category
	}

	if err := t.db.UpdateMemoryContent(params.ID, params.Content, category); err != nil {
		return Error(fmt.Sprintf("update error: %v", err))
	}

	return Success(fmt.Sprintf("Memory %d updated.", params.ID))
}
