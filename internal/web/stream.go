package web

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/yifanes/miniclawd/internal/agent"
	"github.com/yifanes/miniclawd/internal/storage"
)

// handleSendStream accepts a chat message and returns a run_id for SSE streaming.
func (s *WebState) handleSendStream(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionKey string `json:"session_key"`
		Message    string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	sessionKey := body.SessionKey
	if sessionKey == "" {
		sessionKey = "main"
	}

	// Rate limiting.
	if err := s.checkRateLimit(sessionKey); err != nil {
		jsonError(w, err.Error(), http.StatusTooManyRequests)
		return
	}

	// Resolve chat ID.
	chatID, err := s.DB.ResolveOrCreateChatID("web", sessionKey, &sessionKey, "web")
	if err != nil {
		s.releaseInflight(sessionKey)
		jsonError(w, "chat resolve error", http.StatusInternalServerError)
		return
	}

	// Store user message.
	s.DB.StoreMessage(storage.StoredMessage{
		ID:         fmt.Sprintf("web_%d", time.Now().UnixNano()),
		ChatID:     chatID,
		SenderName: "user",
		Content:    body.Message,
		IsFromBot:  false,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	})

	// Start processing in background and stream via SSE.
	runID := fmt.Sprintf("run_%d", time.Now().UnixNano())

	// Process inline with SSE streaming.
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.releaseInflight(sessionKey)
		jsonError(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	eventCh := make(chan agent.AgentEvent, 100)

	go func() {
		defer s.releaseInflight(sessionKey)
		defer close(eventCh)

		reqCtx := agent.AgentRequestContext{
			CallerChannel: "web",
			ChatID:        chatID,
			ChatType:      "web",
		}

		response, err := agent.ProcessWithEvents(r.Context(), s.Deps, reqCtx, nil, nil, eventCh)
		if err != nil {
			log.Printf("[web] agent error for session %s: %v", sessionKey, err)
		}

		// Store bot response.
		if response != "" {
			s.DB.StoreMessage(storage.StoredMessage{
				ID:         fmt.Sprintf("web_bot_%d", time.Now().UnixNano()),
				ChatID:     chatID,
				SenderName: "assistant",
				Content:    response,
				IsFromBot:  true,
				Timestamp:  time.Now().UTC().Format(time.RFC3339),
			})
		}
	}()

	// Send run_id event.
	fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]string{"type": "run_id", "run_id": runID}))
	flusher.Flush()

	// Stream events.
	for event := range eventCh {
		data := map[string]any{"type": event.Type}
		switch event.Type {
		case "iteration":
			data["iteration"] = event.Iteration
		case "tool_start":
			data["name"] = event.Name
		case "tool_result":
			data["name"] = event.Name
			data["is_error"] = event.IsError
			data["preview"] = event.Preview
			data["duration_ms"] = event.DurationMs
		case "text_delta":
			data["delta"] = event.Delta
		case "final_response":
			data["text"] = event.Text
		}

		fmt.Fprintf(w, "data: %s\n\n", mustJSON(data))
		flusher.Flush()
	}

	// Done event.
	fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]string{"type": "done"}))
	flusher.Flush()
}

// handleSSEStream provides a reconnectable SSE endpoint for a run.
func (s *WebState) handleSSEStream(w http.ResponseWriter, r *http.Request) {
	// This is a simplified version. The full implementation would use RunHub
	// with history replay for reconnection.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]string{"type": "connected"}))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	// Block until client disconnects.
	<-r.Context().Done()
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
