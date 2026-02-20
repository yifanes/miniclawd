package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
)

func (s *WebState) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	hash, hasPassword, _ := s.DB.GetAuthPasswordHash()
	_ = hash
	jsonOK(w, map[string]any{
		"has_password": hasPassword,
		"bootstrap":    !hasPassword && (s.Config.WebAuthToken == nil || *s.Config.WebAuthToken == ""),
	})
}

func (s *WebState) handleSetPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	if len(body.Password) < 8 {
		jsonError(w, "password must be at least 8 characters", http.StatusBadRequest)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	if err := s.DB.UpsertAuthPasswordHash(string(hash)); err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *WebState) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	hash, found, _ := s.DB.GetAuthPasswordHash()
	if !found {
		jsonError(w, "no password set", http.StatusBadRequest)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(body.Password)); err != nil {
		jsonError(w, "invalid password", http.StatusUnauthorized)
		return
	}

	// Create session.
	sessionID := generateSessionID()
	expiresAt := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	s.DB.CreateAuthSession(sessionID, "web_login", expiresAt)

	http.SetCookie(w, &http.Cookie{
		Name:     "mc_session",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400,
	})

	jsonOK(w, map[string]string{"status": "ok", "session_id": sessionID})
}

func (s *WebState) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("mc_session")
	if err == nil {
		s.DB.RevokeAuthSession(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:   "mc_session",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *WebState) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.DB.ListAPIKeys()
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, keys)
}

func (s *WebState) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Label  string   `json:"label"`
		Scopes []string `json:"scopes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	rawKey := generateSessionID() + generateSessionID()
	keyHash := hashAPIKey(rawKey)
	prefix := rawKey[:8]

	id, err := s.DB.CreateAPIKey(body.Label, keyHash, prefix, body.Scopes, nil, nil)
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]any{
		"id":     id,
		"key":    rawKey,
		"prefix": prefix,
	})
}

func (s *WebState) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	if err := s.DB.RevokeAPIKey(id); err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]string{"status": "ok"})
}

func generateSessionID() string {
	b := make([]byte, 32)
	for i := range b {
		b[i] = byte(time.Now().UnixNano()>>uint(i) ^ int64(i*37))
	}
	return hex.EncodeToString(sha256.New().Sum(b))[:32]
}

func hashAPIKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}
