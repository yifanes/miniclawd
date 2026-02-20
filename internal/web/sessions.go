package web

import (
	"net/http"
	"strconv"
	"time"
)

func (s *WebState) handleListSessions(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}

	metas, err := s.DB.ListSessionMeta(limit)
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}

	jsonOK(w, metas)
}

func (s *WebState) handleResetSession(w http.ResponseWriter, r *http.Request) {
	sessionKey := r.URL.Query().Get("session_key")
	if sessionKey == "" {
		sessionKey = "main"
	}

	chatID, err := s.DB.ResolveOrCreateChatID("web", sessionKey, &sessionKey, "web")
	if err != nil {
		jsonError(w, "resolve error", http.StatusInternalServerError)
		return
	}

	if err := s.DB.ClearChatContext(chatID); err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *WebState) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	// Return redacted config (no secrets).
	jsonOK(w, map[string]any{
		"llm_provider":       s.Config.LLMProvider,
		"model":              s.Config.Model,
		"max_tokens":         s.Config.MaxTokens,
		"max_tool_iterations": s.Config.MaxToolIterations,
		"web_host":           s.Config.WebHost,
		"web_port":           s.Config.WebPort,
		"timezone":           s.Config.Timezone,
	})
}

func (s *WebState) handleUsage(w http.ResponseWriter, r *http.Request) {
	sessionKey := r.URL.Query().Get("session_key")
	if sessionKey == "" {
		sessionKey = "main"
	}

	chatID, _ := s.DB.ResolveOrCreateChatID("web", sessionKey, &sessionKey, "web")
	summary, err := s.DB.GetLLMUsageSummary(chatID)
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}

	jsonOK(w, summary)
}

func (s *WebState) handleMetrics(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *WebState) handleMetricsHistory(w http.ResponseWriter, r *http.Request) {
	minutesStr := r.URL.Query().Get("minutes")
	minutes := 60
	if m, err := strconv.Atoi(minutesStr); err == nil && m > 0 {
		minutes = m
	}

	sinceMs := time.Now().Add(-time.Duration(minutes) * time.Minute).UnixMilli()
	points, err := s.DB.GetMetricsHistory(sinceMs, 1000)
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}

	jsonOK(w, points)
}
