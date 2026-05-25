package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"technical-specification-review-agent/internal/comment"
	"technical-specification-review-agent/internal/config"
	"technical-specification-review-agent/internal/domain"
	"technical-specification-review-agent/internal/integration/google"
	"technical-specification-review-agent/internal/integration/llm"
	"technical-specification-review-agent/internal/reviewconfig"
)

type stubReviewConfigProvider struct {
	settings reviewconfig.Settings
	err      error
}

func (p stubReviewConfigProvider) Load() (reviewconfig.Settings, error) {
	return p.settings, p.err
}

type stubDocumentRepo struct {
	saved domain.Document
	got   domain.Document
}

func (r *stubDocumentRepo) Save(_ context.Context, document domain.Document) error {
	r.saved = document
	r.got = document
	return nil
}

func (r *stubDocumentRepo) Update(_ context.Context, document domain.Document) error {
	r.saved = document
	r.got = document
	return nil
}

func (r *stubDocumentRepo) GetByID(_ context.Context, _ string) (domain.Document, error) {
	return r.got, nil
}

func (r *stubDocumentRepo) HasBySourceAndExternalID(_ context.Context, source domain.DocumentSource, externalID string) (bool, error) {
	return r.got.Source == source && r.got.ExternalID == externalID, nil
}

type stubAnalysisRepo struct {
	saved     domain.Analysis
	got       domain.Analysis
	prior     []domain.Analysis
	listCalls int
	getCalls  int
	saveCalls int
}

func (r *stubAnalysisRepo) Save(_ context.Context, analysis domain.Analysis) error {
	r.saved = analysis
	r.got = analysis
	r.saveCalls++
	return nil
}

func (r *stubAnalysisRepo) Create(_ context.Context, analysis domain.Analysis) error {
	r.saved = analysis
	r.got = analysis
	r.saveCalls++
	return nil
}

func (r *stubAnalysisRepo) MarkStatus(_ context.Context, _ string, status domain.AnalysisStatus, errorMessage string) error {
	r.got.Status = status
	r.got.ErrorMessage = errorMessage
	r.saved = r.got
	return nil
}

func (r *stubAnalysisRepo) Complete(_ context.Context, analysis domain.Analysis) error {
	r.saved = analysis
	r.got = analysis
	r.saveCalls++
	return nil
}

func (r *stubAnalysisRepo) GetByID(_ context.Context, _ string) (domain.Analysis, error) {
	r.getCalls++
	return r.got, nil
}

func (r *stubAnalysisRepo) ListByReviewKey(_ context.Context, _ string, _ int) ([]domain.Analysis, error) {
	r.listCalls++
	return r.prior, nil
}

type stubAnalysisCache struct{}

func (stubAnalysisCache) Set(context.Context, domain.Analysis) error { return nil }
func (stubAnalysisCache) Get(context.Context, string) (domain.Analysis, bool, error) {
	return domain.Analysis{}, false, nil
}
func (stubAnalysisCache) Delete(context.Context, string) error { return nil }

type stubJobRunner struct {
	enqueued []string
}

func (r *stubJobRunner) EnqueueAnalysis(_ context.Context, analysisID string) error {
	r.enqueued = append(r.enqueued, analysisID)
	return nil
}

func (r *stubJobRunner) Run(context.Context, func(context.Context, string) error) error {
	return nil
}

type recordingLLMClient struct {
	inputs []llm.AnalyzeInput
	result llm.ChunkAnalysisResult
}

func (c *recordingLLMClient) AnalyzeChunk(_ context.Context, input llm.AnalyzeInput) (llm.ChunkAnalysisResult, error) {
	c.inputs = append(c.inputs, input)
	if c.result.RawResponse != "" || len(c.result.Findings) > 0 {
		return c.result, nil
	}
	return llm.ChunkAnalysisResult{
		Findings:      nil,
		PromptVersion: "test",
		SystemPrompt:  "system",
		UserPrompt:    "user",
		RawResponse:   `{"findings":[]}`,
	}, nil
}

type recordingFormatter struct {
	mode comment.PublishMode
}

func (f *recordingFormatter) Format(_ domain.Document, _ domain.Analysis, mode comment.PublishMode) []comment.Draft {
	f.mode = mode
	return []comment.Draft{{
		Type:    "summary",
		Content: "ok",
	}}
}

type stubCommentPublisher struct {
	documentID string
	drafts     []google.CommentDraft
}

func (p *stubCommentPublisher) Publish(_ context.Context, documentExternalID string, drafts []google.CommentDraft) error {
	p.documentID = documentExternalID
	p.drafts = drafts
	return nil
}

type stubDocumentReader struct {
	document google.Document
}

func (r *stubDocumentReader) Read(context.Context, string) (google.Document, error) {
	return r.document, nil
}

func TestAnalysisServiceUsesReviewConfigRolesChunkSizeAndMemoryToggle(t *testing.T) {
	documentRepo := &stubDocumentRepo{}
	analysisRepo := &stubAnalysisRepo{
		prior: []domain.Analysis{{
			ID:      "previous",
			Summary: "previous summary",
		}},
	}
	llmClient := &recordingLLMClient{}

	service := NewAnalysisService(
		documentRepo,
		analysisRepo,
		stubAnalysisCache{},
		llmClient,
		nil,
		nil,
		"openai_compatible",
		"test-model",
		config.DocumentConfig{ChunkSize: 5000, MaxChunks: 10},
		stubReviewConfigProvider{
			settings: reviewconfig.Settings{
				Roles:           []domain.ReviewerRole{domain.ReviewerRoleSeniorBackendEngineer},
				InlineComments:  true,
				SummaryComments: true,
				MemoryEnabled:   false,
				ChunkSize:       40,
				MaxChunks:       10,
				LLMTemperature:  0.3,
				LLMTopP:         0.8,
				LLMMaxTokens:    1100,
			},
		},
		nil,
	)

	analysis, err := service.StartAnalysis(context.Background(), StartAnalysisInput{
		Name:    "spec.md",
		Source:  domain.DocumentSourceUpload,
		Content: "First paragraph with enough text to force chunking.\n\nSecond paragraph with enough text to force chunking.",
		Mode:    domain.AnalysisModeIncrementalReview,
	})
	if err != nil {
		t.Fatalf("StartAnalysis() error = %v", err)
	}

	if analysisRepo.listCalls != 0 {
		t.Fatalf("expected memory history not to load when memory is disabled, got %d calls", analysisRepo.listCalls)
	}
	if analysis.ChunkCount != 2 {
		t.Fatalf("expected 2 chunks, got %d", analysis.ChunkCount)
	}
	if analysis.Memory.HasContext() {
		t.Fatalf("expected memory context to be disabled")
	}
	if len(llmClient.inputs) != 2 {
		t.Fatalf("expected 2 llm calls, got %d", len(llmClient.inputs))
	}
	for _, input := range llmClient.inputs {
		if input.Role != domain.ReviewerRoleSeniorBackendEngineer {
			t.Fatalf("expected only backend role, got %s", input.Role)
		}
		if input.Memory.HasContext() {
			t.Fatalf("expected no memory to be sent into prompt")
		}
		if input.Temperature != 0.3 {
			t.Fatalf("expected llm temperature 0.3, got %v", input.Temperature)
		}
		if input.TopP != 0.8 {
			t.Fatalf("expected llm top_p 0.8, got %v", input.TopP)
		}
		if input.MaxTokens != 1100 {
			t.Fatalf("expected llm max_tokens 1100, got %d", input.MaxTokens)
		}
	}
}

func TestAnalysisServiceQueuesAnalysisWhenRunnerConfigured(t *testing.T) {
	documentRepo := &stubDocumentRepo{}
	analysisRepo := &stubAnalysisRepo{}
	llmClient := &recordingLLMClient{}
	jobRunner := &stubJobRunner{}

	service := NewAnalysisService(
		documentRepo,
		analysisRepo,
		stubAnalysisCache{},
		llmClient,
		nil,
		nil,
		"openai_compatible",
		"test-model",
		config.DocumentConfig{ChunkSize: 5000, MaxChunks: 10},
		stubReviewConfigProvider{
			settings: reviewconfig.Settings{
				Roles:           domain.DefaultReviewerRoles(),
				InlineComments:  true,
				SummaryComments: true,
				MemoryEnabled:   true,
				ChunkSize:       5000,
				MaxChunks:       10,
			},
		},
		jobRunner,
	)

	analysis, err := service.StartAnalysis(context.Background(), StartAnalysisInput{
		Name:    "spec.md",
		Source:  domain.DocumentSourceUpload,
		Content: "Queued analysis payload.",
		Mode:    domain.AnalysisModeFullReview,
	})
	if err != nil {
		t.Fatalf("StartAnalysis() error = %v", err)
	}

	if analysis.Status != domain.AnalysisStatusQueued {
		t.Fatalf("expected queued status, got %s", analysis.Status)
	}
	if len(jobRunner.enqueued) != 1 || jobRunner.enqueued[0] != analysis.ID {
		t.Fatalf("expected one enqueued analysis id, got %+v", jobRunner.enqueued)
	}
	if len(llmClient.inputs) != 0 {
		t.Fatalf("expected no llm calls during enqueue, got %d", len(llmClient.inputs))
	}
	if analysisRepo.saved.Status != domain.AnalysisStatusQueued {
		t.Fatalf("expected queued analysis to be persisted")
	}
}

func TestCommentServiceUsesReviewConfigDefaultPublishMode(t *testing.T) {
	documentRepo := &stubDocumentRepo{
		got: domain.Document{
			ID:         "doc_1",
			Source:     domain.DocumentSourceGoogleDocs,
			ExternalID: "google-doc-1",
		},
	}
	completedAt := time.Now().UTC()
	analysisRepo := &stubAnalysisRepo{
		got: domain.Analysis{
			ID:          "analysis_1",
			DocumentID:  "doc_1",
			Status:      domain.AnalysisStatusCompleted,
			CompletedAt: &completedAt,
		},
	}
	formatter := &recordingFormatter{}
	publisher := &stubCommentPublisher{}

	service := NewCommentService(
		documentRepo,
		analysisRepo,
		formatter,
		publisher,
		stubReviewConfigProvider{
			settings: reviewconfig.Settings{
				InlineComments:  false,
				SummaryComments: true,
			},
		},
	)

	result, err := service.PublishComments(context.Background(), PublishCommentsInput{
		AnalysisID: "analysis_1",
	})
	if err != nil {
		t.Fatalf("PublishComments() error = %v", err)
	}

	if formatter.mode != comment.PublishModeSummary {
		t.Fatalf("expected summary mode from review config, got %s", formatter.mode)
	}
	if result.PublishMode != string(comment.PublishModeSummary) {
		t.Fatalf("expected summary publish mode in result, got %s", result.PublishMode)
	}
	if publisher.documentID != "google-doc-1" {
		t.Fatalf("expected publish to google-doc-1, got %s", publisher.documentID)
	}
	if len(publisher.drafts) != 1 {
		t.Fatalf("expected one published draft, got %d", len(publisher.drafts))
	}
}

func TestAnalysisServiceBlocksMergeWhenCriticalPolicyEnabled(t *testing.T) {
	documentRepo := &stubDocumentRepo{}
	analysisRepo := &stubAnalysisRepo{}
	llmClient := &recordingLLMClient{
		result: llm.ChunkAnalysisResult{
			Findings: []domain.Finding{{
				Role:       string(domain.ReviewerRoleTechLead),
				Category:   "technical_risk",
				Severity:   domain.SeverityCritical,
				Problem:    "Нет стратегии отката миграций для core flow",
				WhyItIsBad: "Production outage нельзя безопасно остановить без потери данных",
				HowToFix:   "Добавить rollback plan для предотвращения outage и data loss",
			}},
			PromptVersion: "test",
			SystemPrompt:  "system",
			UserPrompt:    "user",
			RawResponse:   `{"findings":[{"role":"tech_lead","category":"technical_risk","severity":"CRITICAL","problem":"Нет стратегии отката миграций для core flow","why_it_is_bad":"Production outage нельзя безопасно остановить без потери данных","how_to_fix":"Добавить rollback plan для предотвращения outage и data loss"}]}`,
		},
	}

	service := NewAnalysisService(
		documentRepo,
		analysisRepo,
		stubAnalysisCache{},
		llmClient,
		nil,
		nil,
		"openai_compatible",
		"test-model",
		config.DocumentConfig{ChunkSize: 5000, MaxChunks: 10},
		stubReviewConfigProvider{
			settings: reviewconfig.Settings{
				Roles:              []domain.ReviewerRole{domain.ReviewerRoleTechLead},
				InlineComments:     true,
				SummaryComments:    true,
				MemoryEnabled:      true,
				CriticalBlockMerge: true,
				ChunkSize:          5000,
				MaxChunks:          10,
			},
		},
		nil,
	)

	analysis, err := service.StartAnalysis(context.Background(), StartAnalysisInput{
		Name:    "spec.md",
		Source:  domain.DocumentSourceUpload,
		Content: "One paragraph with a clear deployment risk.",
		Mode:    domain.AnalysisModeFullReview,
	})
	if err != nil {
		t.Fatalf("StartAnalysis() error = %v", err)
	}

	if !analysis.MergeBlocked {
		t.Fatalf("expected merge to be blocked on critical findings")
	}
	if analysis.BlockingFindings != 1 {
		t.Fatalf("expected 1 blocking finding, got %d", analysis.BlockingFindings)
	}
	if !analysisRepo.saved.MergeBlocked {
		t.Fatalf("expected persisted analysis to be merge blocked")
	}
}

func TestAnalysisServicePersistsHistoryAwareMemorySnapshot(t *testing.T) {
	documentRepo := &stubDocumentRepo{}
	analysisRepo := &stubAnalysisRepo{
		prior: []domain.Analysis{{
			ID:      "previous",
			Summary: "previous summary",
		}},
	}
	llmClient := &recordingLLMClient{}

	service := NewAnalysisService(
		documentRepo,
		analysisRepo,
		stubAnalysisCache{},
		llmClient,
		nil,
		nil,
		"openai_compatible",
		"test-model",
		config.DocumentConfig{ChunkSize: 5000, MaxChunks: 10},
		stubReviewConfigProvider{
			settings: reviewconfig.Settings{
				Roles:              []domain.ReviewerRole{domain.ReviewerRoleSeniorBackendEngineer},
				InlineComments:     true,
				SummaryComments:    true,
				MemoryEnabled:      true,
				CriticalBlockMerge: true,
				ChunkSize:          5000,
				MaxChunks:          10,
			},
		},
		nil,
	)

	analysis, err := service.StartAnalysis(context.Background(), StartAnalysisInput{
		Name:    "spec.md",
		Source:  domain.DocumentSourceUpload,
		Content: "Billing paragraph.\n\nIntegration paragraph.",
		Mode:    domain.AnalysisModeFullReview,
	})
	if err != nil {
		t.Fatalf("StartAnalysis() error = %v", err)
	}

	if analysis.Memory.ReviewKey == "" {
		t.Fatalf("expected review key in memory snapshot")
	}
	if len(analysis.Memory.PriorSummaries) == 0 {
		t.Fatalf("expected prior summaries in memory snapshot")
	}
	if len(analysisRepo.saved.Memory.PriorSummaries) == 0 {
		t.Fatalf("expected persisted memory snapshot to include history")
	}
}

func TestAnalysisServiceIncrementalReviewTargetsSingleSection(t *testing.T) {
	documentRepo := &stubDocumentRepo{}
	analysisRepo := &stubAnalysisRepo{}
	llmClient := &recordingLLMClient{}
	reader := &stubDocumentReader{
		document: google.Document{
			Title:      "spec",
			ExternalID: "doc-1",
			Content:    "ignored",
			Sections: []google.Section{
				{ID: "scope", Title: "Scope", Level: 1, Content: "Описание области."},
				{ID: "sla", Title: "SLA", Level: 1, Content: "Описание SLA и дедлайнов."},
			},
		},
	}

	service := NewAnalysisService(
		documentRepo,
		analysisRepo,
		stubAnalysisCache{},
		llmClient,
		reader,
		nil,
		"openai_compatible",
		"test-model",
		config.DocumentConfig{ChunkSize: 5000, MaxChunks: 10},
		stubReviewConfigProvider{
			settings: reviewconfig.Settings{
				Roles:           []domain.ReviewerRole{domain.ReviewerRoleTechLead},
				InlineComments:  true,
				SummaryComments: true,
				MemoryEnabled:   true,
				ChunkSize:       5000,
				MaxChunks:       10,
			},
		},
		nil,
	)

	analysis, err := service.StartAnalysis(context.Background(), StartAnalysisInput{
		Source:             domain.DocumentSourceGoogleDocs,
		GoogleDocURL:       "https://docs.google.com/document/d/doc-1/edit",
		Mode:               domain.AnalysisModeIncrementalReview,
		TargetSectionID:    "sla",
		TargetSectionTitle: "SLA",
	})
	if err != nil {
		t.Fatalf("StartAnalysis() error = %v", err)
	}

	if analysis.TargetSectionID != "sla" {
		t.Fatalf("expected target section id to be preserved, got %q", analysis.TargetSectionID)
	}
	if analysis.ChunkCount != 1 {
		t.Fatalf("expected only one targeted chunk, got %d", analysis.ChunkCount)
	}
	if len(llmClient.inputs) != 1 {
		t.Fatalf("expected one llm call for targeted section, got %d", len(llmClient.inputs))
	}
	if llmClient.inputs[0].SectionTitle != "SLA" {
		t.Fatalf("expected targeted section title SLA, got %q", llmClient.inputs[0].SectionTitle)
	}
}

func TestAnalysisServiceShapesFindingsAcrossRoles(t *testing.T) {
	documentRepo := &stubDocumentRepo{}
	analysisRepo := &stubAnalysisRepo{}
	llmClient := &recordingLLMClient{
		result: llm.ChunkAnalysisResult{
			Findings: []domain.Finding{
				{
					Role:       string(domain.ReviewerRoleTechLead),
					Category:   "missing_requirement",
					Severity:   domain.SeverityCritical,
					Problem:    "Не описано, кто может отменять подтвержденный refund.",
					WhyItIsBad: "Нарушается бизнес-процесс.",
					HowToFix:   "Указать роли и правила отмены refund.",
				},
				{
					Role:       string(domain.ReviewerRoleSolutionArchitect),
					Category:   "missing_requirement",
					Severity:   domain.SeverityCritical,
					Problem:    "Отсутствует описание, кто имеет право отменять уже подтвержденный refund.",
					WhyItIsBad: "Появляется риск конфликтов ролей.",
					HowToFix:   "Зафиксировать полномочия на отмену refund.",
				},
				{
					Role:       string(domain.ReviewerRoleSecurityLead),
					Category:   "security_risk",
					Severity:   domain.SeverityCritical,
					Problem:    "Не описана проверка прав на отмену refund.",
					WhyItIsBad: "Возможны неавторизованные действия и финансовые потери.",
					HowToFix:   "Добавить RBAC и аудит доступа.",
				},
			},
			PromptVersion: "test",
			SystemPrompt:  "system",
			UserPrompt:    "user",
			RawResponse:   `{"findings":[]}`,
		},
	}

	service := NewAnalysisService(
		documentRepo,
		analysisRepo,
		stubAnalysisCache{},
		llmClient,
		nil,
		nil,
		"openai_compatible",
		"test-model",
		config.DocumentConfig{ChunkSize: 5000, MaxChunks: 10},
		stubReviewConfigProvider{
			settings: reviewconfig.Settings{
				Roles: []domain.ReviewerRole{
					domain.ReviewerRoleTechLead,
					domain.ReviewerRoleSolutionArchitect,
					domain.ReviewerRoleSecurityLead,
				},
				InlineComments:  true,
				SummaryComments: true,
				MemoryEnabled:   true,
				ChunkSize:       5000,
				MaxChunks:       10,
			},
		},
		nil,
	)

	analysis, err := service.StartAnalysis(context.Background(), StartAnalysisInput{
		Name:    "spec.md",
		Source:  domain.DocumentSourceUpload,
		Content: "Refund flow paragraph.",
		Mode:    domain.AnalysisModeFullReview,
	})
	if err != nil {
		t.Fatalf("StartAnalysis() error = %v", err)
	}

	if len(analysis.Findings) > 3 {
		t.Fatalf("expected shaped findings to stay compact, got %d", len(analysis.Findings))
	}
	criticalCount := 0
	for _, finding := range analysis.Findings {
		if finding.Severity == domain.SeverityCritical {
			criticalCount++
		}
	}
	if criticalCount > 1 {
		t.Fatalf("expected most generic criticals to be downgraded, got %d criticals", criticalCount)
	}
	if strings.Contains(analysis.Summary, "Найдено") {
		t.Fatalf("expected summary to be cluster-based, got %q", analysis.Summary)
	}
}
