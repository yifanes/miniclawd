package agent

import (
	"fmt"
	"strings"

	"github.com/yifanes/miniclawd/internal/storage"
)

// BuildDBMemoryContext retrieves memories from the database and formats them
// for injection into the system prompt, respecting the token budget.
func BuildDBMemoryContext(db *storage.Database, chatID int64, query string, tokenBudget int) string {
	if db == nil || tokenBudget <= 0 {
		return ""
	}

	// Fetch candidate memories.
	memories, err := db.GetMemoriesForContext(chatID, 100)
	if err != nil || len(memories) == 0 {
		return ""
	}

	// Score and rank memories by keyword relevance.
	type scored struct {
		mem   storage.Memory
		score float64
	}
	var candidates []scored

	queryLower := strings.ToLower(query)
	queryWords := strings.Fields(queryLower)

	for _, m := range memories {
		score := m.Confidence
		contentLower := strings.ToLower(m.Content)

		// Keyword scoring: boost memories that match query terms.
		for _, w := range queryWords {
			if len(w) >= 3 && strings.Contains(contentLower, w) {
				score += 0.3
			}
		}

		// Boost higher-confidence memories.
		if m.Confidence >= 0.8 {
			score += 0.2
		}

		candidates = append(candidates, scored{mem: m, score: score})
	}

	// Sort by score descending.
	for i := 0; i < len(candidates)-1; i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].score > candidates[i].score {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	// Select memories within token budget (estimate ~4 chars per token).
	var selected []storage.Memory
	tokensUsed := 0
	selectedCount := 0
	omittedCount := 0

	for _, c := range candidates {
		est := len(c.mem.Content) / 4
		if tokensUsed+est > tokenBudget && selectedCount > 0 {
			omittedCount++
			continue
		}
		selected = append(selected, c.mem)
		tokensUsed += est
		selectedCount++
	}

	if len(selected) == 0 {
		return ""
	}

	// Log injection for observability.
	db.LogMemoryInjection(chatID, "keyword", len(candidates), selectedCount, omittedCount, tokensUsed)

	// Format as XML.
	var sb strings.Builder
	sb.WriteString("<structured_memories>\n")
	for _, m := range selected {
		scope := "chat"
		if m.ChatID == nil {
			scope = "global"
		}
		sb.WriteString(fmt.Sprintf("- [%s][%s] %s\n", m.Category, scope, m.Content))
	}
	sb.WriteString("</structured_memories>")

	return sb.String()
}
