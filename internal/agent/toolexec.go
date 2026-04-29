package agent

import (
	"context"
	"log"
	"sync"

	"github.com/yifanes/miniclawd/internal/core"
	"github.com/yifanes/miniclawd/internal/tools"
)

// ToolConcurrencyClass classifies tools by their concurrency safety.
type ToolConcurrencyClass int

const (
	ConcurrencyReadOnly   ToolConcurrencyClass = iota // Safe to run in parallel
	ConcurrencySideEffect                             // Run sequentially
	ConcurrencyExclusive                              // Run alone
)

// toolConcurrencyClass returns the concurrency class for a given tool name.
func toolConcurrencyClass(name string) ToolConcurrencyClass {
	switch name {
	case "read_file", "glob", "grep", "web_fetch", "web_search",
		"read_memory", "structured_memory_search", "get_current_time",
		"compare_time", "calculate", "export_chat", "todo_read",
		"list_scheduled_tasks", "get_scheduled_task_history",
		"knowledge_graph_query", "activate_skill", "skill_manage":
		return ConcurrencyReadOnly
	case "sub_agent", "browser", "sessions_spawn":
		return ConcurrencyExclusive
	default:
		return ConcurrencySideEffect
	}
}

// executeToolsConcurrent executes tool use blocks with concurrency based on their class.
// When multiple tools are requested: ReadOnly tools run in parallel first,
// then SideEffect tools run sequentially, and Exclusive tools run alone.
func executeToolsConcurrent(
	ctx context.Context,
	toolUses []core.ResponseContentBlock,
	registry *tools.ToolRegistry,
	auth *tools.ToolAuthContext,
	chatID int64,
	eventCh chan<- AgentEvent,
	maxConcurrency int,
) []core.ContentBlock {
	if len(toolUses) <= 1 || maxConcurrency <= 1 {
		return executeToolsSequential(ctx, toolUses, registry, auth, chatID, eventCh)
	}

	// Partition by concurrency class.
	var readOnly, sideEffect, exclusive []int
	for i, tu := range toolUses {
		switch toolConcurrencyClass(tu.Name) {
		case ConcurrencyReadOnly:
			readOnly = append(readOnly, i)
		case ConcurrencyExclusive:
			exclusive = append(exclusive, i)
		default:
			sideEffect = append(sideEffect, i)
		}
	}

	results := make([]core.ContentBlock, len(toolUses))

	// Wave 1: ReadOnly tools in parallel.
	if len(readOnly) > 0 {
		executeWaveParallel(ctx, toolUses, readOnly, registry, auth, chatID, eventCh, results, maxConcurrency)
	}

	// Wave 2: SideEffect tools sequentially.
	for _, idx := range sideEffect {
		results[idx] = executeSingleTool(ctx, toolUses[idx], registry, auth, chatID, eventCh)
	}

	// Wave 3: Exclusive tools one at a time.
	for _, idx := range exclusive {
		results[idx] = executeSingleTool(ctx, toolUses[idx], registry, auth, chatID, eventCh)
	}

	return results
}

func executeWaveParallel(
	ctx context.Context,
	toolUses []core.ResponseContentBlock,
	indices []int,
	registry *tools.ToolRegistry,
	auth *tools.ToolAuthContext,
	chatID int64,
	eventCh chan<- AgentEvent,
	results []core.ContentBlock,
	maxConcurrency int,
) {
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup

	for _, idx := range indices {
		wg.Add(1)
		sem <- struct{}{} // acquire
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }() // release
			results[i] = executeSingleTool(ctx, toolUses[i], registry, auth, chatID, eventCh)
		}(idx)
	}
	wg.Wait()
}

func executeSingleTool(
	ctx context.Context,
	tu core.ResponseContentBlock,
	registry *tools.ToolRegistry,
	auth *tools.ToolAuthContext,
	chatID int64,
	eventCh chan<- AgentEvent,
) core.ContentBlock {
	if eventCh != nil {
		eventCh <- ToolStartEvent(tu.Name)
	}

	inputPreview := string(tu.Input)
	if len(inputPreview) > 300 {
		inputPreview = inputPreview[:300] + "..."
	}
	log.Printf("[agent] chat %d: tool %s input: %s", chatID, tu.Name, inputPreview)

	result := registry.ExecuteWithAuth(ctx, tu.Name, tu.Input, auth)

	resultPreview := result.Content
	if len(resultPreview) > 300 {
		resultPreview = resultPreview[:300] + "..."
	}
	log.Printf("[agent] chat %d: tool %s result (err=%v, %dms): %s",
		chatID, tu.Name, result.IsError, derefDuration(result.DurationMs), resultPreview)

	if eventCh != nil {
		eventCh <- ToolResultEvent(tu.Name, result.IsError, resultPreview,
			derefDuration(result.DurationMs), result.StatusCode, result.Bytes, result.ErrorType)
	}

	return core.ToolResultBlock(tu.ID, result.Content, result.IsError)
}

func executeToolsSequential(
	ctx context.Context,
	toolUses []core.ResponseContentBlock,
	registry *tools.ToolRegistry,
	auth *tools.ToolAuthContext,
	chatID int64,
	eventCh chan<- AgentEvent,
) []core.ContentBlock {
	results := make([]core.ContentBlock, len(toolUses))
	for i, tu := range toolUses {
		results[i] = executeSingleTool(ctx, tu, registry, auth, chatID, eventCh)
	}
	return results
}
