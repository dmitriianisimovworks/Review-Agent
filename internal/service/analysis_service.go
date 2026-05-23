package service

import (
	"context"
	"fmt"
	"time"

	"technical-specification-review-agent/internal/domain"
	"technical-specification-review-agent/internal/integration/google"
	"technical-specification-review-agent/internal/integration/llm"
	"technical-specification-review-agent/internal/parser"
	"technical-specification-review-agent/internal/repository"
)

type AnalysisService struct {
	documentRepo     repository.DocumentRepository
	analysisRepo     repository.AnalysisRepository
	documentParser   parser.DocumentParser
	llmClient        llm.Client
	commentPublisher google.CommentPublisher
}

type StartAnalysisInput struct {
	Name    string
	Content string
	Source  domain.DocumentSource
}

func NewAnalysisService(
	documentRepo repository.DocumentRepository,
	analysisRepo repository.AnalysisRepository,
	documentParser parser.DocumentParser,
	llmClient llm.Client,
	commentPublisher google.CommentPublisher,
) *AnalysisService {
	return &AnalysisService{
		documentRepo:     documentRepo,
		analysisRepo:     analysisRepo,
		documentParser:   documentParser,
		llmClient:        llmClient,
		commentPublisher: commentPublisher,
	}
}

func (s *AnalysisService) StartAnalysis(ctx context.Context, input StartAnalysisInput) (domain.Analysis, error) {
	now := time.Now().UTC()

	document := domain.Document{
		ID:        fmt.Sprintf("doc_%d", now.UnixNano()),
		Name:      input.Name,
		Source:    input.Source,
		Content:   input.Content,
		CreatedAt: now,
	}

	if err := s.documentRepo.Save(ctx, document); err != nil {
		return domain.Analysis{}, err
	}

	parsed, err := s.documentParser.Parse(ctx, document.Content)
	if err != nil {
		return domain.Analysis{}, err
	}

	findings, summary, err := s.llmClient.Analyze(ctx, llm.PromptInput{
		DocumentText: parsed.Text,
		Chunks:       parsed.Chunks,
	})
	if err != nil {
		return domain.Analysis{}, err
	}

	completedAt := time.Now().UTC()
	analysis := domain.Analysis{
		ID:          fmt.Sprintf("analysis_%d", completedAt.UnixNano()),
		DocumentID:  document.ID,
		Status:      domain.AnalysisStatusCompleted,
		Findings:    findings,
		Summary:     summary,
		CreatedAt:   now,
		CompletedAt: &completedAt,
	}

	if err := s.analysisRepo.Save(ctx, analysis); err != nil {
		return domain.Analysis{}, err
	}

	return analysis, nil
}

func (s *AnalysisService) GetAnalysis(ctx context.Context, id string) (domain.Analysis, error) {
	return s.analysisRepo.GetByID(ctx, id)
}
