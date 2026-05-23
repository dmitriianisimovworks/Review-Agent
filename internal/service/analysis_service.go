package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"technical-specification-review-agent/internal/apperrors"
	"technical-specification-review-agent/internal/domain"
	"technical-specification-review-agent/internal/integration/google"
	"technical-specification-review-agent/internal/integration/llm"
	"technical-specification-review-agent/internal/parser"
	"technical-specification-review-agent/internal/repository"
)

type AnalysisService struct {
	documentRepo     repository.DocumentRepository
	analysisRepo     repository.AnalysisRepository
	analysisCache    repository.AnalysisCache
	documentParser   parser.DocumentParser
	llmClient        llm.Client
	documentReader   google.DocumentReader
	commentPublisher google.CommentPublisher
	llmProvider      string
	llmModel         string
}

type StartAnalysisInput struct {
	Name         string
	Content      string
	GoogleDocURL string
	Source       domain.DocumentSource
	Mode         domain.AnalysisMode
}

func NewAnalysisService(
	documentRepo repository.DocumentRepository,
	analysisRepo repository.AnalysisRepository,
	analysisCache repository.AnalysisCache,
	documentParser parser.DocumentParser,
	llmClient llm.Client,
	documentReader google.DocumentReader,
	commentPublisher google.CommentPublisher,
	llmProvider string,
	llmModel string,
) *AnalysisService {
	return &AnalysisService{
		documentRepo:     documentRepo,
		analysisRepo:     analysisRepo,
		analysisCache:    analysisCache,
		documentParser:   documentParser,
		llmClient:        llmClient,
		documentReader:   documentReader,
		commentPublisher: commentPublisher,
		llmProvider:      llmProvider,
		llmModel:         llmModel,
	}
}

func (s *AnalysisService) StartAnalysis(ctx context.Context, input StartAnalysisInput) (domain.Analysis, error) {
	now := time.Now().UTC()
	source := input.Source
	if source == "" {
		source = domain.DocumentSourceUpload
	}
	mode := input.Mode
	if mode == "" {
		mode = domain.AnalysisModeFullReview
	}

	content := strings.TrimSpace(input.Content)
	documentName := strings.TrimSpace(input.Name)
	externalID := ""

	if source == domain.DocumentSourceGoogleDocs {
		if s.documentReader == nil {
			return domain.Analysis{}, apperrors.New(apperrors.KindInternal, "google docs reader is not configured")
		}
		if strings.TrimSpace(input.GoogleDocURL) == "" {
			return domain.Analysis{}, apperrors.New(apperrors.KindInvalidArgument, "google_doc_url is required for google_docs source")
		}

		doc, err := s.documentReader.Read(ctx, input.GoogleDocURL)
		if err != nil {
			return domain.Analysis{}, apperrors.Wrap(apperrors.KindDependency, "failed to read google document", err)
		}
		content = doc.Content
		externalID = doc.ExternalID
		if documentName == "" {
			documentName = doc.Title
		}
	}

	if content == "" {
		return domain.Analysis{}, apperrors.New(apperrors.KindInvalidArgument, "document content is required")
	}

	document := domain.Document{
		ID:         fmt.Sprintf("doc_%d", now.UnixNano()),
		Name:       documentName,
		Source:     source,
		ExternalID: externalID,
		RawContent: content,
		CreatedAt:  now,
	}

	parsed, err := s.documentParser.Parse(ctx, document.RawContent)
	if err != nil {
		return domain.Analysis{}, apperrors.Wrap(apperrors.KindInternal, "failed to parse document", err)
	}
	document.NormalizedContent = parsed.Text

	if err := s.documentRepo.Save(ctx, document); err != nil {
		return domain.Analysis{}, apperrors.Wrap(apperrors.KindInternal, "failed to store document", err)
	}

	aggregatedFindings := make([]domain.Finding, 0)
	chunks := make([]domain.AnalysisChunk, 0, len(parsed.Chunks))
	for idx, chunk := range parsed.Chunks {
		result, err := s.llmClient.AnalyzeChunk(ctx, llm.AnalyzeInput{
			DocumentName: document.Name,
			DocumentText: parsed.Text,
			ChunkText:    chunk,
			ChunkIndex:   idx,
			ChunkCount:   len(parsed.Chunks),
			Mode:         mode,
			Source:       document.Source,
		})
		if err != nil {
			return domain.Analysis{}, apperrors.Wrap(apperrors.KindDependency, fmt.Sprintf("failed to analyze chunk %d", idx+1), err)
		}

		chunks = append(chunks, domain.AnalysisChunk{
			ID:             fmt.Sprintf("chunk_%d_%d", now.UnixNano(), idx),
			ChunkIndex:     idx,
			ChunkText:      chunk,
			PromptVersion:  result.PromptVersion,
			SystemPrompt:   result.SystemPrompt,
			UserPrompt:     result.UserPrompt,
			RawLLMResponse: result.RawResponse,
			CreatedAt:      time.Now().UTC(),
		})
		aggregatedFindings = append(aggregatedFindings, result.Findings...)
	}

	completedAt := time.Now().UTC()
	analysis := domain.Analysis{
		ID:          fmt.Sprintf("analysis_%d", completedAt.UnixNano()),
		DocumentID:  document.ID,
		Mode:        mode,
		Status:      domain.AnalysisStatusCompleted,
		Provider:    s.llmProvider,
		Model:       s.llmModel,
		ChunkCount:  len(parsed.Chunks),
		Findings:    aggregatedFindings,
		Chunks:      chunks,
		Summary:     buildSummary(aggregatedFindings, len(parsed.Chunks)),
		CreatedAt:   now,
		CompletedAt: &completedAt,
	}
	for i := range analysis.Chunks {
		analysis.Chunks[i].AnalysisID = analysis.ID
	}

	if err := s.analysisRepo.Save(ctx, analysis); err != nil {
		return domain.Analysis{}, apperrors.Wrap(apperrors.KindInternal, "failed to store analysis", err)
	}
	if s.analysisCache != nil {
		_ = s.analysisCache.Set(ctx, analysis)
	}

	return analysis, nil
}

func (s *AnalysisService) GetAnalysis(ctx context.Context, id string) (domain.Analysis, error) {
	if s.analysisCache != nil {
		analysis, found, err := s.analysisCache.Get(ctx, id)
		if err == nil && found {
			return analysis, nil
		}
		if err != nil {
			_ = s.analysisCache.Delete(ctx, id)
		}
	}

	analysis, err := s.analysisRepo.GetByID(ctx, id)
	if err != nil {
		return domain.Analysis{}, apperrors.Wrap(apperrors.KindNotFound, "analysis not found", err)
	}
	if s.analysisCache != nil {
		_ = s.analysisCache.Set(ctx, analysis)
	}
	return analysis, nil
}

func buildSummary(findings []domain.Finding, chunkCount int) string {
	if len(findings) == 0 {
		return fmt.Sprintf("Review completed across %d chunks. No substantial issues were detected.", chunkCount)
	}

	severityCounts := map[domain.Severity]int{}
	categoryCounts := map[string]int{}
	for _, finding := range findings {
		severityCounts[finding.Severity]++
		categoryCounts[finding.Category]++
	}

	topCategories := make([]string, 0, len(categoryCounts))
	for category := range categoryCounts {
		topCategories = append(topCategories, category)
	}

	sort.Slice(topCategories, func(i, j int) bool {
		if categoryCounts[topCategories[i]] == categoryCounts[topCategories[j]] {
			return topCategories[i] < topCategories[j]
		}
		return categoryCounts[topCategories[i]] > categoryCounts[topCategories[j]]
	})

	if len(topCategories) > 3 {
		topCategories = topCategories[:3]
	}

	return fmt.Sprintf(
		"Review completed across %d chunks. Findings: %d total, %d critical, %d errors, %d warnings. Main categories: %s.",
		chunkCount,
		len(findings),
		severityCounts[domain.SeverityCritical],
		severityCounts[domain.SeverityError],
		severityCounts[domain.SeverityWarning],
		strings.Join(topCategories, ", "),
	)
}
