package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"technical-specification-review-agent/internal/comment"
	"technical-specification-review-agent/internal/domain"
	"technical-specification-review-agent/internal/service"
)

type AnalysisService interface {
	StartAnalysis(ctx context.Context, input service.StartAnalysisInput) (domain.Analysis, error)
	GetAnalysis(ctx context.Context, id string) (domain.Analysis, error)
	PublishComments(ctx context.Context, input service.PublishCommentsInput) (service.PublishCommentsResult, error)
}

type AnalysisHandler struct {
	analysisService AnalysisService
}

type StartAnalysisRequest struct {
	Name         string `json:"name"`
	Content      string `json:"content"`
	GoogleDocURL string `json:"google_doc_url"`
	ContextKey   string `json:"context_key"`
	Source       string `json:"source"`
	Mode         string `json:"mode"`
}

type AnalysisResponse struct {
	ID                 string           `json:"id"`
	DocumentID         string           `json:"document_id"`
	Status             string           `json:"status"`
	Summary            string           `json:"summary"`
	MergeBlocked       bool             `json:"merge_blocked"`
	BlockingFindings   int              `json:"blocking_findings"`
	SuppressedFindings int              `json:"suppressed_findings"`
	Findings           []FindingPayload `json:"findings"`
}

type PublishCommentsRequest struct {
	Mode string `json:"mode"`
}

type PublishCommentsResponse struct {
	AnalysisID     string `json:"analysis_id"`
	DocumentID     string `json:"document_id"`
	DocumentSource string `json:"document_source"`
	PublishedCount int    `json:"published_count"`
	PublishMode    string `json:"publish_mode"`
}

type FindingPayload struct {
	Role                string `json:"role"`
	Category            string `json:"category"`
	Severity            string `json:"severity"`
	Problem             string `json:"problem"`
	WhyItIsBad          string `json:"why_it_is_bad"`
	HowToFix            string `json:"how_to_fix"`
	SectionTitle        string `json:"section_title,omitempty"`
	RelatedSectionTitle string `json:"related_section_title,omitempty"`
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
		ContextKey:   req.ContextKey,
		Source:       domain.DocumentSource(req.Source),
		Mode:         domain.AnalysisMode(req.Mode),
	})
	if err != nil {
		log.Printf("analysis start: source=%q mode=%q google_doc=%t context_key=%q err=%v", req.Source, req.Mode, req.GoogleDocURL != "", req.ContextKey, err)
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusAccepted, toAnalysisResponse(analysis))
}

func (h *AnalysisHandler) Get(w http.ResponseWriter, r *http.Request) {
	analysisID := chi.URLParam(r, "analysisID")

	analysis, err := h.analysisService.GetAnalysis(r.Context(), analysisID)
	if err != nil {
		log.Printf("analysis get: analysis_id=%q err=%v", analysisID, err)
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, toAnalysisResponse(analysis))
}

func (h *AnalysisHandler) PublishComments(w http.ResponseWriter, r *http.Request) {
	analysisID := chi.URLParam(r, "analysisID")

	var req PublishCommentsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	result, err := h.analysisService.PublishComments(r.Context(), service.PublishCommentsInput{
		AnalysisID: analysisID,
		Mode:       serviceCommentMode(req.Mode),
	})
	if err != nil {
		log.Printf("analysis publish-comments: analysis_id=%q mode=%q err=%v", analysisID, req.Mode, err)
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusAccepted, PublishCommentsResponse{
		AnalysisID:     result.AnalysisID,
		DocumentID:     result.DocumentID,
		DocumentSource: result.DocumentSource,
		PublishedCount: result.PublishedCount,
		PublishMode:    result.PublishMode,
	})
}

func toAnalysisResponse(analysis domain.Analysis) AnalysisResponse {
	findings := make([]FindingPayload, 0, len(analysis.Findings))
	for _, finding := range analysis.Findings {
		findings = append(findings, FindingPayload{
			Role:                finding.Role,
			Category:            finding.Category,
			Severity:            string(finding.Severity),
			Problem:             finding.Problem,
			WhyItIsBad:          finding.WhyItIsBad,
			HowToFix:            finding.HowToFix,
			SectionTitle:        finding.SectionTitle,
			RelatedSectionTitle: finding.RelatedSectionTitle,
		})
	}

	return AnalysisResponse{
		ID:                 analysis.ID,
		DocumentID:         analysis.DocumentID,
		Status:             string(analysis.Status),
		Summary:            analysis.Summary,
		MergeBlocked:       analysis.MergeBlocked,
		BlockingFindings:   analysis.BlockingFindings,
		SuppressedFindings: analysis.SuppressedFindings,
		Findings:           findings,
	}
}

func serviceCommentMode(mode string) comment.PublishMode {
	switch mode {
	case string(comment.PublishModeInline):
		return comment.PublishModeInline
	case string(comment.PublishModeSummary):
		return comment.PublishModeSummary
	default:
		return comment.PublishModeBoth
	}
}
