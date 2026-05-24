package handlers

import (
	"context"
	"net/http"

	"technical-specification-review-agent/internal/domain"
)

type GoogleOAuthService interface {
	BeginAuth() (string, error)
	CompleteAuth(ctx context.Context, state, code string) (domain.GoogleOAuthConnection, error)
}

type GoogleOAuthHandler struct {
	service GoogleOAuthService
}

func NewGoogleOAuthHandler(service GoogleOAuthService) *GoogleOAuthHandler {
	return &GoogleOAuthHandler{service: service}
}

func (h *GoogleOAuthHandler) Start(w http.ResponseWriter, r *http.Request) {
	url, err := h.service.BeginAuth()
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"auth_url": url,
	})
}

func (h *GoogleOAuthHandler) Callback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")

	connection, err := h.service.CompleteAuth(r.Context(), state, code)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "connected",
		"google_user_id": connection.GoogleUserID,
		"email":          connection.Email,
		"expiry":         connection.Expiry,
	})
}
