package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"technical-specification-review-agent/internal/domain"
	"technical-specification-review-agent/internal/service"
)

type AnalysisService interface {
	StartAnalysis(ctx context.Context, input service.StartAnalysisInput) (domain.Analysis, error)
	GetAnalysis(ctx context.Context, id string) (domain.Analysis, error)
}

type AnalysisHandler struct {
	analysisService AnalysisService
}

type StartAnalysisRequest struct {
	Name         string `json:"name"`
	Content      string `json:"content"`
	GoogleDocURL string `json:"google_doc_url"`
	Source       string `json:"source"`
	Mode         string `json:"mode"`
}

type AnalysisResponse struct {
	ID         string           `json:"id"`
	DocumentID string           `json:"document_id"`
	Status     string           `json:"status"`
	Summary    string           `json:"summary"`
	Findings   []FindingPayload `json:"findings"`
}

type FindingPayload struct {
	Role       string `json:"role"`
	Category   string `json:"category"`
	Severity   string `json:"severity"`
	Problem    string `json:"problem"`
	WhyItIsBad string `json:"why_it_is_bad"`
	HowToFix   string `json:"how_to_fix"`
}

func NewAnalysisHandler(analysisService AnalysisService) *AnalysisHandler {
	return &AnalysisHandler{analysisService: analysisService}
}

func (h *AnalysisHandler) Start(w http.ResponseWriter, r *http.Request) {
	var req StartAnalysisRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	analysis, err := h.analysisService.StartAnalysis(r.Context(), service.StartAnalysisInput{
		Name:         req.Name,
		Content:      req.Content,
		GoogleDocURL: req.GoogleDocURL,
		Source:       domain.DocumentSource(req.Source),
		Mode:         domain.AnalysisMode(req.Mode),
	})
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusAccepted, toAnalysisResponse(analysis))
}

func (h *AnalysisHandler) Get(w http.ResponseWriter, r *http.Request) {
	analysisID := chi.URLParam(r, "analysisID")

	analysis, err := h.analysisService.GetAnalysis(r.Context(), analysisID)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, toAnalysisResponse(analysis))
}

func toAnalysisResponse(analysis domain.Analysis) AnalysisResponse {
	findings := make([]FindingPayload, 0, len(analysis.Findings))
	for _, finding := range analysis.Findings {
		findings = append(findings, FindingPayload{
			Role:       finding.Role,
			Category:   finding.Category,
			Severity:   string(finding.Severity),
			Problem:    finding.Problem,
			WhyItIsBad: finding.WhyItIsBad,
			HowToFix:   finding.HowToFix,
		})
	}

	return AnalysisResponse{
		ID:         analysis.ID,
		DocumentID: analysis.DocumentID,
		Status:     string(analysis.Status),
		Summary:    analysis.Summary,
		Findings:   findings,
	}
}
