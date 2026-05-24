package service

import (
	"context"
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
	return nil
}

func (r *stubDocumentRepo) GetByID(_ context.Context, _ string) (domain.Document, error) {
	return r.got, nil
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

type recordingLLMClient struct {
	inputs            []llm.AnalyzeInput
	sectionPairInputs []llm.AnalyzeSectionPairInput
	result            llm.ChunkAnalysisResult
	sectionPairResult llm.ChunkAnalysisResult
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

func (c *recordingLLMClient) AnalyzeSectionPair(_ context.Context, input llm.AnalyzeSectionPairInput) (llm.ChunkAnalysisResult, error) {
	c.sectionPairInputs = append(c.sectionPairInputs, input)
	if c.sectionPairResult.RawResponse != "" || len(c.sectionPairResult.Findings) > 0 {
		return c.sectionPairResult, nil
	}
	return llm.ChunkAnalysisResult{
		Findings:      nil,
		PromptVersion: "test-cross",
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
				Roles:                      []domain.ReviewerRole{domain.ReviewerRoleSeniorBackendEngineer},
				CrossSectionContradictions: false,
				InlineComments:             true,
				SummaryComments:            true,
				MemoryEnabled:              false,
				ChunkSize:                  40,
				MaxChunks:                  10,
			},
		},
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
				CrossSectionContradictions: false,
				InlineComments:             false,
				SummaryComments:            true,
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
				Problem:    "Нет стратегии отката миграций",
				WhyItIsBad: "Сломанный деплой нельзя безопасно откатить",
				HowToFix:   "Добавить rollback plan",
			}},
			PromptVersion: "test",
			SystemPrompt:  "system",
			UserPrompt:    "user",
			RawResponse:   `{"findings":[{"role":"tech_lead","category":"technical_risk","severity":"CRITICAL","problem":"Нет стратегии отката миграций","why_it_is_bad":"Сломанный деплой нельзя безопасно откатить","how_to_fix":"Добавить rollback plan"}]}`,
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
				Roles:                      []domain.ReviewerRole{domain.ReviewerRoleTechLead},
				CrossSectionContradictions: false,
				InlineComments:             true,
				SummaryComments:            true,
				MemoryEnabled:              true,
				CriticalBlockMerge:         true,
				ChunkSize:                  5000,
				MaxChunks:                  10,
			},
		},
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

func TestAnalysisServicePersistsStructuredMemorySnapshot(t *testing.T) {
	documentRepo := &stubDocumentRepo{}
	analysisRepo := &stubAnalysisRepo{
		prior: []domain.Analysis{{
			ID:      "previous",
			Summary: "previous summary",
			Memory: domain.ReviewMemory{
				Modules: []string{"Legacy Billing"},
			},
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
				Roles:                      []domain.ReviewerRole{domain.ReviewerRoleSeniorBackendEngineer},
				CrossSectionContradictions: false,
				InlineComments:             true,
				SummaryComments:            true,
				MemoryEnabled:              true,
				CriticalBlockMerge:         true,
				ChunkSize:                  5000,
				MaxChunks:                  10,
			},
		},
	)

	analysis, err := service.StartAnalysis(context.Background(), StartAnalysisInput{
		Name:    "spec.md",
		Source:  domain.DocumentSourceUpload,
		Content: "1. Billing\n\nАдминистратор обновляет `invoice`.\n\n2. Integrations\n\nUser sees \"PaymentIntent\" status.",
		Mode:    domain.AnalysisModeFullReview,
	})
	if err != nil {
		t.Fatalf("StartAnalysis() error = %v", err)
	}

	if len(analysis.Memory.Modules) == 0 {
		t.Fatalf("expected structured modules in analysis memory")
	}
	if len(analysis.Memory.Entities) == 0 {
		t.Fatalf("expected structured entities in analysis memory")
	}
	if len(analysisRepo.saved.Memory.Modules) == 0 {
		t.Fatalf("expected persisted memory snapshot to include modules")
	}
}

func TestAnalysisServiceAddsCrossSectionContradictionFindings(t *testing.T) {
	documentRepo := &stubDocumentRepo{}
	analysisRepo := &stubAnalysisRepo{}
	llmClient := &recordingLLMClient{
		sectionPairResult: llm.ChunkAnalysisResult{
			Findings: []domain.Finding{{
				Role:       string(domain.ReviewerRoleSolutionArchitect),
				Category:   "contradiction",
				Severity:   domain.SeverityError,
				Problem:    "Разделы Scope и Permissions противоречат друг другу по правам удаления.",
				WhyItIsBad: "Команда реализует несовместимые правила доступа.",
				HowToFix:   "Выбрать единое правило удаления и синхронизировать оба раздела.",
			}},
			PromptVersion: "test-cross",
			SystemPrompt:  "system",
			UserPrompt:    "user",
			RawResponse:   `{"findings":[{"role":"solution_architect","category":"contradiction","severity":"ERROR","problem":"Разделы Scope и Permissions противоречат друг другу по правам удаления.","why_it_is_bad":"Команда реализует несовместимые правила доступа.","how_to_fix":"Выбрать единое правило удаления и синхронизировать оба раздела."}]}`,
		},
	}
	reader := &stubDocumentReader{
		document: google.Document{
			Title:      "spec",
			ExternalID: "doc-1",
			Content:    "ignored",
			Sections: []google.Section{
				{ID: "scope", Title: "Scope", Level: 1, Content: "Пользователь может удалять объект."},
				{ID: "permissions", Title: "Permissions", Level: 1, Content: "Удалять объект может только администратор."},
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
				Roles:                      []domain.ReviewerRole{domain.ReviewerRoleSolutionArchitect},
				CrossSectionContradictions: true,
				InlineComments:             true,
				SummaryComments:            true,
				MemoryEnabled:              true,
				CriticalBlockMerge:         true,
				ChunkSize:                  5000,
				MaxChunks:                  10,
			},
		},
	)

	analysis, err := service.StartAnalysis(context.Background(), StartAnalysisInput{
		Source:       domain.DocumentSourceGoogleDocs,
		GoogleDocURL: "https://docs.google.com/document/d/doc-1/edit",
		Mode:         domain.AnalysisModeFullReview,
	})
	if err != nil {
		t.Fatalf("StartAnalysis() error = %v", err)
	}

	if len(llmClient.sectionPairInputs) == 0 {
		t.Fatalf("expected cross-section contradiction pass to run")
	}
	found := false
	for _, finding := range analysis.Findings {
		if finding.Category != "contradiction" {
			continue
		}
		found = true
		if finding.SectionTitle == "" || finding.RelatedSectionTitle == "" {
			t.Fatalf("expected contradiction finding to include both section titles, got %+v", finding)
		}
	}
	if !found {
		t.Fatalf("expected contradiction finding in analysis result")
	}
}

func TestAnalysisServiceSkipsCrossSectionContradictionPassWhenDisabled(t *testing.T) {
	documentRepo := &stubDocumentRepo{}
	analysisRepo := &stubAnalysisRepo{}
	llmClient := &recordingLLMClient{}
	reader := &stubDocumentReader{
		document: google.Document{
			Title:      "spec",
			ExternalID: "doc-1",
			Content:    "ignored",
			Sections: []google.Section{
				{ID: "scope", Title: "Scope", Level: 1, Content: "Пользователь может удалять объект."},
				{ID: "permissions", Title: "Permissions", Level: 1, Content: "Удалять объект может только администратор."},
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
				Roles:                      []domain.ReviewerRole{domain.ReviewerRoleSolutionArchitect},
				CrossSectionContradictions: false,
				InlineComments:             true,
				SummaryComments:            true,
				MemoryEnabled:              true,
				CriticalBlockMerge:         true,
				ChunkSize:                  5000,
				MaxChunks:                  10,
			},
		},
	)

	if _, err := service.StartAnalysis(context.Background(), StartAnalysisInput{
		Source:       domain.DocumentSourceGoogleDocs,
		GoogleDocURL: "https://docs.google.com/document/d/doc-1/edit",
		Mode:         domain.AnalysisModeFullReview,
	}); err != nil {
		t.Fatalf("StartAnalysis() error = %v", err)
	}

	if len(llmClient.sectionPairInputs) != 0 {
		t.Fatalf("expected cross-section contradiction pass to stay disabled")
	}
}

func TestCompactGlobalFindingsKeepsOnlyStrongestCrossRoleDuplicate(t *testing.T) {
	findings := []domain.Finding{
		{
			Role:       string(domain.ReviewerRoleTechLead),
			Category:   "missing_requirement",
			Severity:   domain.SeverityCritical,
			Problem:    "Не указано поведение при одновременном захвате кейса двумя пользователями.",
			WhyItIsBad: "Возникает race condition.",
			HowToFix:   "Добавить атомарность захвата.",
		},
		{
			Role:       string(domain.ReviewerRoleSeniorBackendEngineer),
			Category:   "missing_requirement",
			Severity:   domain.SeverityError,
			Problem:    "Отсутствует описание поведения при конкурентном захвате кейса двумя пользователями.",
			WhyItIsBad: "Возможна двойная выдача кейса.",
			HowToFix:   "Описать optimistic locking.",
		},
		{
			Role:       string(domain.ReviewerRoleQAReviewer),
			Category:   "api_problem",
			Severity:   domain.SeverityError,
			Problem:    "Не описаны негативные сценарии обработки ошибок.",
			WhyItIsBad: "Нельзя полноценно тестировать отказоустойчивость.",
			HowToFix:   "Добавить error flows.",
		},
	}

	compacted := compactGlobalFindings(findings)
	if len(compacted) != 2 {
		t.Fatalf("expected 2 findings after global compaction, got %d", len(compacted))
	}
	if compacted[0].Role != string(domain.ReviewerRoleTechLead) {
		t.Fatalf("expected strongest duplicate to survive, got role %s", compacted[0].Role)
	}
}
