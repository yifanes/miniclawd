package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/yifanes/miniclawd/internal/agent"
	"github.com/yifanes/miniclawd/internal/core"
	"github.com/yifanes/miniclawd/internal/llm"
	"github.com/yifanes/miniclawd/internal/storage"
)

// SpawnReflector starts the memory extraction background loop.
func SpawnReflector(ctx context.Context, db *storage.Database, provider llm.LLMProvider, intervalMins int) {
	go func() {
		interval := time.Duration(intervalMins) * time.Minute
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runReflector(ctx, db, provider, intervalMins)
			}
		}
	}()
}

func runReflector(ctx context.Context, db *storage.Database, provider llm.LLMProvider, intervalMins int) {
	// Archive stale memories (30 days).
	db.ArchiveStaleMemories(30 * 24 * time.Hour)

	// Get active chats.
	since := time.Now().UTC().Add(-time.Duration(intervalMins*2) * time.Minute).Format(time.RFC3339)
	chatIDs, err := db.GetActiveChatIDsSince(since)
	if err != nil {
		log.Printf("[reflector] error getting active chats: %v", err)
		return
	}

	for _, chatID := range chatIDs {
		if ctx.Err() != nil {
			return
		}
		reflectForChat(ctx, db, provider, chatID)
	}
}

func reflectForChat(ctx context.Context, db *storage.Database, provider llm.LLMProvider, chatID int64) {
	startedAt := time.Now().UTC()

	// Get cursor.
	cursor, hasCursor, _ := db.GetReflectorCursor(chatID)

	var messages []storage.StoredMessage
	var err error
	if hasCursor {
		messages, err = db.GetMessagesSince(chatID, cursor, 200)
	} else {
		messages, err = db.GetRecentMessages(chatID, 30)
	}
	if err != nil || len(messages) == 0 {
		return
	}

	// Format conversation.
	var conv strings.Builder
	for _, msg := range messages {
		sender := msg.SenderName
		if msg.IsFromBot {
			sender = "assistant"
		}
		conv.WriteString(fmt.Sprintf("[%s]: %s\n", sender, msg.Content))
	}

	// Get existing memories for dedup.
	existing, _ := db.GetAllMemoriesForChat(chatID)

	// Build extraction prompt.
	system := `You are a memory extraction system. Extract important facts, preferences, and context from the conversation.

Output a JSON array of objects with fields:
- "content": the fact or memory (concise, 1-2 sentences)
- "category": one of "PROFILE", "KNOWLEDGE", "EVENT"
- "supersedes_id": null, or the ID of an existing memory this replaces

Rules:
- Only extract genuinely important information worth remembering long-term
- Skip small talk, greetings, and trivial exchanges
- Deduplicate against existing memories
- If updating an existing memory, set supersedes_id to that memory's ID`

	var existingContext strings.Builder
	if len(existing) > 0 {
		existingContext.WriteString("\nExisting memories:\n")
		for _, m := range existing {
			if !m.IsArchived {
				existingContext.WriteString(fmt.Sprintf("[id=%d] [%s] %s\n", m.ID, m.Category, m.Content))
			}
		}
	}

	userMsg := fmt.Sprintf("Conversation:\n%s\n%s\nExtract memories as a JSON array:", conv.String(), existingContext.String())

	resp, err := provider.SendMessage(ctx, system, []core.Message{
		{Role: "user", Content: core.TextContent(userMsg)},
	}, nil)
	if err != nil {
		finishedAt := time.Now().UTC()
		errText := err.Error()
		db.LogReflectorRun(chatID, startedAt.Format(time.RFC3339), finishedAt.Format(time.RFC3339),
			0, 0, 0, 0, "keyword", false, &errText)
		return
	}

	// Extract text response.
	var responseText string
	for _, b := range resp.Content {
		if b.Type == "text" {
			responseText += b.Text
		}
	}

	// Parse JSON array.
	type extractedMemory struct {
		Content      string `json:"content"`
		Category     string `json:"category"`
		SupersedesID *int64 `json:"supersedes_id"`
	}

	// Find JSON array in response.
	jsonStart := strings.Index(responseText, "[")
	jsonEnd := strings.LastIndex(responseText, "]")
	if jsonStart == -1 || jsonEnd == -1 || jsonEnd <= jsonStart {
		finishedAt := time.Now().UTC()
		db.LogReflectorRun(chatID, startedAt.Format(time.RFC3339), finishedAt.Format(time.RFC3339),
			0, 0, 0, 0, "keyword", false, strPtr("no JSON array in response"))
		return
	}

	var extracted []extractedMemory
	if err := json.Unmarshal([]byte(responseText[jsonStart:jsonEnd+1]), &extracted); err != nil {
		finishedAt := time.Now().UTC()
		errText := err.Error()
		db.LogReflectorRun(chatID, startedAt.Format(time.RFC3339), finishedAt.Format(time.RFC3339),
			0, 0, 0, 0, "keyword", false, &errText)
		return
	}

	inserted, updated, skipped := 0, 0, 0

	for _, em := range extracted {
		if len(em.Content) < 20 {
			skipped++
			continue
		}

		// Check for duplicate via Jaccard similarity.
		isDup := false
		for _, ex := range existing {
			if !ex.IsArchived && jaccardSimilarity(em.Content, ex.Content) > 0.5 {
				isDup = true
				// Touch the existing memory.
				db.TouchMemoryLastSeen(ex.ID)
				break
			}
		}
		if isDup {
			skipped++
			continue
		}

		// Handle supersedes.
		if em.SupersedesID != nil && *em.SupersedesID > 0 {
			newID, err := db.InsertMemoryWithMetadata(&chatID, em.Content, em.Category, "reflector", 0.75)
			if err == nil {
				db.SupersedeMemory(*em.SupersedesID, newID, "reflector update")
				updated++
			}
			continue
		}

		// Insert new memory.
		if _, err := db.InsertMemoryWithMetadata(&chatID, em.Content, em.Category, "reflector", 0.75); err == nil {
			inserted++
		}
	}

	finishedAt := time.Now().UTC()
	db.LogReflectorRun(chatID, startedAt.Format(time.RFC3339), finishedAt.Format(time.RFC3339),
		len(extracted), inserted, updated, skipped, "keyword", true, nil)

	// Update cursor to latest message timestamp.
	if len(messages) > 0 {
		db.SetReflectorCursor(chatID, messages[len(messages)-1].Timestamp)
	}
}

// jaccardSimilarity computes the Jaccard similarity of two strings by word sets.
func jaccardSimilarity(a, b string) float64 {
	wordsA := wordSet(strings.ToLower(a))
	wordsB := wordSet(strings.ToLower(b))

	intersection := 0
	for w := range wordsA {
		if wordsB[w] {
			intersection++
		}
	}

	union := len(wordsA) + len(wordsB) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func wordSet(s string) map[string]bool {
	set := make(map[string]bool)
	for _, w := range strings.Fields(s) {
		if len(w) >= 3 {
			set[w] = true
		}
	}
	return set
}

func strPtr(s string) *string { return &s }

// Ensure agent import is used.
var _ = agent.StripThinking
