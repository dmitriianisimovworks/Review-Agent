package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"technical-specification-review-agent/internal/domain"
	"technical-specification-review-agent/internal/service"
)

type TrackedDocumentService interface {
	RegisterGoogleDoc(ctx context.Context, input service.RegisterTrackedGoogleDocInput) (domain.TrackedDocument, error)
}

type TrackedDocumentHandler struct {
	service TrackedDocumentService
}

type RegisterTrackedGoogleDocRequest struct {
	GoogleDocURL string `json:"google_doc_url"`
}

type TrackedDocumentResponse struct {
	ID          string `json:"id"`
	Source      string `json:"source"`
	ExternalID  string `json:"external_id"`
	Name        string `json:"name"`
	DocumentURL string `json:"document_url"`
	CreatedAt   string `json:"created_at"`
}

func NewTrackedDocumentHandler(service TrackedDocumentService) *TrackedDocumentHandler {
	return &TrackedDocumentHandler{service: service}
}

func (h *TrackedDocumentHandler) RegisterGoogleDoc(w http.ResponseWriter, r *http.Request) {
	var req RegisterTrackedGoogleDocRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	tracked, err := h.service.RegisterGoogleDoc(r.Context(), service.RegisterTrackedGoogleDocInput{
		DocumentURL: req.GoogleDocURL,
	})
	if err != nil {
		log.Printf("tracked google doc register: url=%q err=%v", req.GoogleDocURL, err)
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusAccepted, TrackedDocumentResponse{
		ID:          tracked.ID,
		Source:      string(tracked.Source),
		ExternalID:  tracked.ExternalID,
		Name:        tracked.Name,
		DocumentURL: tracked.DocumentURL,
		CreatedAt:   tracked.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}
