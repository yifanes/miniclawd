package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/yifanes/miniclawd/internal/agent"
	"github.com/yifanes/miniclawd/internal/config"
	"github.com/yifanes/miniclawd/internal/storage"
)

// WebState holds web server shared state.
type WebState struct {
	Config *config.Config
	DB     *storage.Database
	Deps   *agent.AgentDeps

	// Inflight tracking per session.
	inflight   map[string]int
	inflightMu sync.Mutex

	// Rate limiting per session.
	requests   map[string][]time.Time
	requestsMu sync.Mutex
}

// StartWebServer creates and starts the web server.
func StartWebServer(ctx context.Context, cfg *config.Config, db *storage.Database, deps *agent.AgentDeps) error {
	state := &WebState{
		Config:   cfg,
		DB:       db,
		Deps:     deps,
		inflight: make(map[string]int),
		requests: make(map[string][]time.Time),
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(120 * time.Second))

	// Health check.
	r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Auth routes.
	r.Route("/api/auth", func(r chi.Router) {
		r.Get("/status", state.handleAuthStatus)
		r.Post("/password", state.handleSetPassword)
		r.Post("/login", state.handleLogin)
		r.Post("/logout", state.handleLogout)
		r.Get("/api_keys", state.handleListAPIKeys)
		r.Post("/api_keys", state.handleCreateAPIKey)
		r.Delete("/api_keys/{id}", state.handleRevokeAPIKey)
	})

	// Config routes.
	r.Get("/api/config", state.handleGetConfig)

	// Session routes.
	r.Get("/api/sessions", state.handleListSessions)
	r.Post("/api/reset", state.handleResetSession)

	// Chat routes.
	r.Post("/api/send_stream", state.handleSendStream)
	r.Get("/api/stream", state.handleSSEStream)

	// Usage.
	r.Get("/api/usage", state.handleUsage)

	// Metrics.
	r.Get("/api/metrics", state.handleMetrics)
	r.Get("/api/metrics/history", state.handleMetricsHistory)

	addr := fmt.Sprintf("%s:%d", cfg.WebHost, cfg.WebPort)
	server := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()

	log.Printf("[web] server starting on %s", addr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *WebState) checkRateLimit(sessionKey string) error {
	// Check inflight.
	s.inflightMu.Lock()
	count := s.inflight[sessionKey]
	if count >= s.Config.WebMaxInflightPerSession {
		s.inflightMu.Unlock()
		return fmt.Errorf("too many concurrent requests for session %q", sessionKey)
	}
	s.inflight[sessionKey] = count + 1
	s.inflightMu.Unlock()

	// Check rate window.
	s.requestsMu.Lock()
	defer s.requestsMu.Unlock()

	now := time.Now()
	cutoff := now.Add(-time.Duration(s.Config.WebRateWindowSeconds) * time.Second)
	var recent []time.Time
	for _, t := range s.requests[sessionKey] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	if len(recent) >= s.Config.WebMaxRequestsPerWindow {
		s.inflightMu.Lock()
		s.inflight[sessionKey]--
		s.inflightMu.Unlock()
		return fmt.Errorf("rate limit exceeded for session %q", sessionKey)
	}
	recent = append(recent, now)
	s.requests[sessionKey] = recent
	return nil
}

func (s *WebState) releaseInflight(sessionKey string) {
	s.inflightMu.Lock()
	s.inflight[sessionKey]--
	if s.inflight[sessionKey] <= 0 {
		delete(s.inflight, sessionKey)
	}
	s.inflightMu.Unlock()
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func jsonOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}
