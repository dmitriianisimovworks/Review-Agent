package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"technical-specification-review-agent/internal/apperrors"
	"technical-specification-review-agent/internal/config"
	"technical-specification-review-agent/internal/domain"
	"technical-specification-review-agent/internal/integration/google"
	"technical-specification-review-agent/internal/integration/llm"
	"technical-specification-review-agent/internal/jobs"
	"technical-specification-review-agent/internal/parser"
	"technical-specification-review-agent/internal/repository"
	"technical-specification-review-agent/internal/reviewconfig"
)

type AnalysisService struct {
	documentRepo     repository.DocumentRepository
	analysisRepo     repository.AnalysisRepository
	analysisCache    repository.AnalysisCache
	llmClient        llm.Client
	documentReader   google.DocumentReader
	commentPublisher google.CommentPublisher
	llmProvider      string
	llmModel         string
	documentConfig   config.DocumentConfig
	reviewConfig     reviewconfig.Provider
	jobRunner        jobs.Runner
}

const maxFindingsPerRole = 5

type StartAnalysisInput struct {
	Name         string
	Content      string
	GoogleDocURL string
	ContextKey   string
	Source       domain.DocumentSource
	Mode         domain.AnalysisMode
}

func NewAnalysisService(
	documentRepo repository.DocumentRepository,
	analysisRepo repository.AnalysisRepository,
	analysisCache repository.AnalysisCache,
	llmClient llm.Client,
	documentReader google.DocumentReader,
	commentPublisher google.CommentPublisher,
	llmProvider string,
	llmModel string,
	documentConfig config.DocumentConfig,
	reviewConfig reviewconfig.Provider,
	jobRunner jobs.Runner,
) *AnalysisService {
	return &AnalysisService{
		documentRepo:     documentRepo,
		analysisRepo:     analysisRepo,
		analysisCache:    analysisCache,
		llmClient:        llmClient,
		documentReader:   documentReader,
		commentPublisher: commentPublisher,
		llmProvider:      llmProvider,
		llmModel:         llmModel,
		documentConfig:   documentConfig,
		reviewConfig:     reviewConfig,
		jobRunner:        jobRunner,
	}
}

func (s *AnalysisService) StartAnalysis(ctx context.Context, input StartAnalysisInput) (domain.Analysis, error) {
	if s.jobRunner != nil {
		return s.startAnalysisAsync(ctx, input)
	}
	return s.startAnalysisSync(ctx, input)
}

func (s *AnalysisService) startAnalysisSync(ctx context.Context, input StartAnalysisInput) (domain.Analysis, error) {
	now := time.Now().UTC()
	source := input.Source
	if source == "" {
		source = domain.DocumentSourceUpload
	}
	mode := input.Mode
	if mode == "" {
		mode = domain.AnalysisModeFullReview
	}
	settings, err := s.loadReviewConfig()
	if err != nil {
		return domain.Analysis{}, err
	}

	document, structuredDoc, err := s.buildDocumentFromInput(ctx, input, now)
	if err != nil {
		return domain.Analysis{}, err
	}
	document, parsed, promptMemory, err := s.prepareAnalysisDocument(ctx, document, structuredDoc, mode, settings)
	if err != nil {
		return domain.Analysis{}, err
	}
	if err := s.documentRepo.Save(ctx, document); err != nil {
		return domain.Analysis{}, apperrors.Wrap(apperrors.KindInternal, "failed to store document", err)
	}

	analysis := s.buildCompletedAnalysis(ctx, document, parsed, promptMemory, mode, now, settings)
	if analysis.ErrorMessage != "" {
		return domain.Analysis{}, apperrors.New(apperrors.KindDependency, analysis.ErrorMessage)
	}
	if analysis.CompletedAt == nil {
		return domain.Analysis{}, apperrors.New(apperrors.KindInternal, "analysis completed_at is missing")
	}
	analysis.ID = fmt.Sprintf("analysis_%d", analysis.CompletedAt.UnixNano())
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

func (s *AnalysisService) startAnalysisAsync(ctx context.Context, input StartAnalysisInput) (domain.Analysis, error) {
	now := time.Now().UTC()
	source := input.Source
	if source == "" {
		source = domain.DocumentSourceUpload
	}
	mode := input.Mode
	if mode == "" {
		mode = domain.AnalysisModeFullReview
	}

	document, _, err := s.buildDocumentFromInput(ctx, input, now)
	if err != nil {
		return domain.Analysis{}, err
	}
	if err := s.documentRepo.Save(ctx, document); err != nil {
		return domain.Analysis{}, apperrors.Wrap(apperrors.KindInternal, "failed to store document", err)
	}

	analysis := domain.Analysis{
		ID:         fmt.Sprintf("analysis_%d", now.UnixNano()),
		DocumentID: document.ID,
		Mode:       mode,
		Status:     domain.AnalysisStatusQueued,
		Provider:   s.llmProvider,
		Model:      s.llmModel,
		Summary:    "",
		CreatedAt:  now,
	}
	if err := s.analysisRepo.Create(ctx, analysis); err != nil {
		return domain.Analysis{}, apperrors.Wrap(apperrors.KindInternal, "failed to create queued analysis", err)
	}
	if s.analysisCache != nil {
		_ = s.analysisCache.Set(ctx, analysis)
	}
	if err := s.jobRunner.EnqueueAnalysis(ctx, analysis.ID); err != nil {
		return domain.Analysis{}, apperrors.Wrap(apperrors.KindDependency, "failed to enqueue analysis job", err)
	}
	return analysis, nil
}

func (s *AnalysisService) ProcessAnalysis(ctx context.Context, analysisID string) error {
	analysis, err := s.analysisRepo.GetByID(ctx, analysisID)
	if err != nil {
		return apperrors.Wrap(apperrors.KindNotFound, "analysis not found", err)
	}
	if analysis.Status == domain.AnalysisStatusCompleted {
		return nil
	}

	document, err := s.documentRepo.GetByID(ctx, analysis.DocumentID)
	if err != nil {
		return apperrors.Wrap(apperrors.KindNotFound, "document not found", err)
	}

	if err := s.analysisRepo.MarkStatus(ctx, analysis.ID, domain.AnalysisStatusProcessing, ""); err != nil {
		return apperrors.Wrap(apperrors.KindInternal, "failed to mark analysis as processing", err)
	}
	analysis.Status = domain.AnalysisStatusProcessing
	analysis.ErrorMessage = ""
	if s.analysisCache != nil {
		_ = s.analysisCache.Set(ctx, analysis)
	}

	settings, err := s.loadReviewConfig()
	if err != nil {
		_ = s.failAnalysis(ctx, analysis, err)
		return err
	}

	structuredDoc, err := s.loadStructuredDocument(ctx, document)
	if err != nil {
		_ = s.failAnalysis(ctx, analysis, err)
		return err
	}

	document, parsed, promptMemory, err := s.prepareAnalysisDocument(ctx, document, structuredDoc, analysis.Mode, settings)
	if err != nil {
		_ = s.failAnalysis(ctx, analysis, err)
		return err
	}
	if err := s.documentRepo.Update(ctx, document); err != nil {
		wrapped := apperrors.Wrap(apperrors.KindInternal, "failed to update document", err)
		_ = s.failAnalysis(ctx, analysis, wrapped)
		return wrapped
	}

	completed := s.buildCompletedAnalysis(ctx, document, parsed, promptMemory, analysis.Mode, analysis.CreatedAt, settings)
	completed.ID = analysis.ID
	completed.DocumentID = analysis.DocumentID
	completed.CreatedAt = analysis.CreatedAt
	if completed.ErrorMessage != "" {
		err := apperrors.New(apperrors.KindDependency, completed.ErrorMessage)
		_ = s.failAnalysis(ctx, analysis, err)
		return err
	}
	for i := range completed.Chunks {
		completed.Chunks[i].AnalysisID = completed.ID
	}

	if err := s.analysisRepo.Complete(ctx, completed); err != nil {
		wrapped := apperrors.Wrap(apperrors.KindInternal, "failed to store analysis", err)
		_ = s.failAnalysis(ctx, analysis, wrapped)
		return wrapped
	}
	if s.analysisCache != nil {
		_ = s.analysisCache.Set(ctx, completed)
	}
	return nil
}

func (s *AnalysisService) analyzeChunksByRoles(
	ctx context.Context,
	document domain.Document,
	parsed parser.ParsedDocument,
	mode domain.AnalysisMode,
	now time.Time,
	memory domain.ReviewMemory,
	roles []domain.ReviewerRole,
) ([]domain.AnalysisChunk, []domain.Finding, int, error) {
	type llmOutcome struct {
		chunk    domain.AnalysisChunk
		findings []domain.Finding
	}

	outcomes := make([]llmOutcome, 0, len(parsed.Chunks)*len(roles))
	g, groupCtx := errgroup.WithContext(ctx)
	g.SetLimit(len(roles))

	results := make(chan llmOutcome, len(parsed.Chunks)*len(roles))

	for idx, chunkText := range parsed.Chunks {
		for _, role := range roles {
			idx := idx
			chunkText := chunkText
			role := role
			descriptor := parsedChunkDescriptor(parsed, idx)

			g.Go(func() error {
				result, err := s.llmClient.AnalyzeChunk(groupCtx, llm.AnalyzeInput{
					DocumentName: document.Name,
					DocumentText: parsed.Text,
					ChunkText:    chunkText,
					ChunkIndex:   idx,
					ChunkCount:   len(parsed.Chunks),
					SectionTitle: descriptor.SectionTitle,
					SectionLevel: descriptor.SectionLevel,
					Mode:         mode,
					Source:       document.Source,
					Role:         role,
					Memory:       memory,
				})
				if err != nil {
					return apperrors.Wrap(apperrors.KindDependency, fmt.Sprintf("failed to analyze chunk %d for role %s", idx+1, role), err)
				}
				annotateChunkFindings(result.Findings, descriptor, idx)

				results <- llmOutcome{
					chunk: domain.AnalysisChunk{
						ID:             fmt.Sprintf("chunk_%d_%d_%s", now.UnixNano(), idx, role),
						Role:           role,
						ChunkIndex:     idx,
						ChunkText:      chunkText,
						PromptVersion:  result.PromptVersion,
						SystemPrompt:   result.SystemPrompt,
						UserPrompt:     result.UserPrompt,
						RawLLMResponse: result.RawResponse,
						SectionID:      descriptor.SectionID,
						SectionTitle:   descriptor.SectionTitle,
						SectionLevel:   descriptor.SectionLevel,
						Range:          descriptor.Range,
						CreatedAt:      time.Now().UTC(),
					},
					findings: result.Findings,
				}
				return nil
			})
		}
	}

	if err := g.Wait(); err != nil {
		close(results)
		return nil, nil, 0, err
	}
	close(results)

	for outcome := range results {
		outcomes = append(outcomes, outcome)
	}

	sort.SliceStable(outcomes, func(i, j int) bool {
		if outcomes[i].chunk.ChunkIndex == outcomes[j].chunk.ChunkIndex {
			return outcomes[i].chunk.Role < outcomes[j].chunk.Role
		}
		return outcomes[i].chunk.ChunkIndex < outcomes[j].chunk.ChunkIndex
	})

	chunks := make([]domain.AnalysisChunk, 0, len(outcomes))
	findings := make([]domain.Finding, 0)
	for _, outcome := range outcomes {
		chunks = append(chunks, outcome.chunk)
		findings = append(findings, outcome.findings...)
	}

	filtered, suppressedCount := filterFindingsByMode(filterRoleFindings(findings), memory, mode)
	return chunks, filtered, suppressedCount, nil
}

func (s *AnalysisService) buildDocumentFromInput(ctx context.Context, input StartAnalysisInput, now time.Time) (domain.Document, google.Document, error) {
	source := input.Source
	if source == "" {
		source = domain.DocumentSourceUpload
	}

	content := strings.TrimSpace(input.Content)
	documentName := strings.TrimSpace(input.Name)
	externalID := ""
	reviewKey := strings.TrimSpace(input.ContextKey)
	var structuredDoc google.Document

	switch source {
	case domain.DocumentSourceGoogleDocs:
		if strings.TrimSpace(input.GoogleDocURL) == "" {
			return domain.Document{}, google.Document{}, apperrors.New(apperrors.KindInvalidArgument, "google_doc_url is required for google_docs source")
		}
		documentID, err := google.ExtractDocumentID(input.GoogleDocURL)
		if err != nil {
			return domain.Document{}, google.Document{}, apperrors.Wrap(apperrors.KindInvalidArgument, "invalid google_doc_url", err)
		}
		externalID = documentID
		if documentName == "" {
			documentName = "google_doc_" + documentID
		}
		if s.jobRunner == nil {
			if s.documentReader == nil {
				return domain.Document{}, google.Document{}, apperrors.New(apperrors.KindInternal, "google docs reader is not configured")
			}
			doc, err := s.documentReader.Read(ctx, input.GoogleDocURL)
			if err != nil {
				return domain.Document{}, google.Document{}, apperrors.Wrap(apperrors.KindDependency, "failed to read google document", err)
			}
			structuredDoc = doc
			content = doc.Content
			externalID = doc.ExternalID
			if strings.TrimSpace(doc.Title) != "" {
				documentName = doc.Title
			}
		}
	case domain.DocumentSourceUpload:
		if content == "" {
			return domain.Document{}, google.Document{}, apperrors.New(apperrors.KindInvalidArgument, "document content is required")
		}
	default:
		if content == "" {
			return domain.Document{}, google.Document{}, apperrors.New(apperrors.KindInvalidArgument, "document content is required")
		}
	}

	reviewKey = deriveReviewKey(source, reviewKey, externalID, documentName)
	return domain.Document{
		ID:                fmt.Sprintf("doc_%d", now.UnixNano()),
		Name:              documentName,
		Source:            source,
		ExternalID:        externalID,
		ReviewKey:         reviewKey,
		RawContent:        content,
		NormalizedContent: content,
		Sections:          toDomainSections(structuredDoc.Sections),
		Blocks:            toDomainBlocks(structuredDoc.Blocks),
		CreatedAt:         now,
	}, structuredDoc, nil
}

func (s *AnalysisService) loadStructuredDocument(ctx context.Context, document domain.Document) (google.Document, error) {
	if document.Source != domain.DocumentSourceGoogleDocs {
		return google.Document{
			ExternalID: document.ExternalID,
			Title:      document.Name,
			Content:    document.RawContent,
		}, nil
	}
	if s.documentReader == nil {
		return google.Document{}, apperrors.New(apperrors.KindInternal, "google docs reader is not configured")
	}
	if strings.TrimSpace(document.ExternalID) == "" {
		return google.Document{}, apperrors.New(apperrors.KindInvalidArgument, "google document external id is missing")
	}
	return s.documentReader.Read(ctx, document.ExternalID)
}

func (s *AnalysisService) prepareAnalysisDocument(
	ctx context.Context,
	document domain.Document,
	structuredDoc google.Document,
	mode domain.AnalysisMode,
	settings reviewconfig.Settings,
) (domain.Document, parser.ParsedDocument, domain.ReviewMemory, error) {
	content := strings.TrimSpace(document.RawContent)
	if strings.TrimSpace(structuredDoc.Content) != "" {
		content = structuredDoc.Content
		document.RawContent = content
		document.ExternalID = structuredDoc.ExternalID
		if strings.TrimSpace(structuredDoc.Title) != "" {
			document.Name = structuredDoc.Title
		}
		document.Sections = toDomainSections(structuredDoc.Sections)
		document.Blocks = toDomainBlocks(structuredDoc.Blocks)
	}
	if content == "" {
		return domain.Document{}, parser.ParsedDocument{}, domain.ReviewMemory{}, apperrors.New(apperrors.KindInvalidArgument, "document content is required")
	}

	var previousAnalyses []domain.Analysis
	var err error
	if settings.MemoryEnabled {
		previousAnalyses, err = s.analysisRepo.ListByReviewKey(ctx, document.ReviewKey, reviewMemoryRunLimit)
		if err != nil {
			return domain.Document{}, parser.ParsedDocument{}, domain.ReviewMemory{}, apperrors.Wrap(apperrors.KindInternal, "failed to load review history", err)
		}
	}
	promptMemory := buildReviewMemory(document.ReviewKey, previousAnalyses)
	if !settings.MemoryEnabled {
		promptMemory = domain.ReviewMemory{}
	}

	documentParser := parser.NewChunkingParser(config.DocumentConfig{
		ChunkSize: settings.ChunkSize,
		MaxChunks: settings.MaxChunks,
	})
	parsed, err := documentParser.Parse(ctx, parser.ParseInput{
		Content:  content,
		Sections: document.Sections,
	})
	if err != nil {
		return domain.Document{}, parser.ParsedDocument{}, domain.ReviewMemory{}, apperrors.Wrap(apperrors.KindInternal, "failed to parse document", err)
	}
	document.NormalizedContent = parsed.Text
	return document, parsed, promptMemory, nil
}

func (s *AnalysisService) buildCompletedAnalysis(
	ctx context.Context,
	document domain.Document,
	parsed parser.ParsedDocument,
	promptMemory domain.ReviewMemory,
	mode domain.AnalysisMode,
	createdAt time.Time,
	settings reviewconfig.Settings,
) domain.Analysis {
	chunks, findings, suppressedFindings, err := s.analyzeChunksByRoles(ctx, document, parsed, mode, createdAt, promptMemory, settings.Roles)
	if err != nil {
		return domain.Analysis{ErrorMessage: err.Error()}
	}

	aggregatedFindings := deduplicateFindings(findings)
	completedAt := time.Now().UTC()
	mergeBlocked, blockingFindings := evaluateMergePolicy(aggregatedFindings, settings)
	return domain.Analysis{
		DocumentID:         document.ID,
		Mode:               mode,
		Status:             domain.AnalysisStatusCompleted,
		Provider:           s.llmProvider,
		Model:              s.llmModel,
		ChunkCount:         len(parsed.Chunks),
		MergeBlocked:       mergeBlocked,
		BlockingFindings:   blockingFindings,
		SuppressedFindings: suppressedFindings,
		Findings:           aggregatedFindings,
		Chunks:             chunks,
		Summary:            buildSummary(aggregatedFindings, len(parsed.Chunks)),
		Memory:             promptMemory,
		DocumentSections:   parsed.Sections,
		CreatedAt:          createdAt,
		CompletedAt:        &completedAt,
	}
}

func (s *AnalysisService) failAnalysis(ctx context.Context, analysis domain.Analysis, err error) error {
	message := err.Error()
	_ = s.analysisRepo.MarkStatus(ctx, analysis.ID, domain.AnalysisStatusFailed, message)
	analysis.Status = domain.AnalysisStatusFailed
	analysis.ErrorMessage = message
	now := time.Now().UTC()
	analysis.CompletedAt = &now
	if s.analysisCache != nil {
		_ = s.analysisCache.Set(ctx, analysis)
	}
	return err
}

func annotateChunkFindings(findings []domain.Finding, descriptor parser.ParsedChunk, chunkIndex int) {
	for idx := range findings {
		findings[idx].ChunkIndex = chunkIndex
		findings[idx].SectionID = descriptor.SectionID
		findings[idx].SectionTitle = descriptor.SectionTitle
	}
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
		return fmt.Sprintf("Анализ завершён. Обработано фрагментов: %d. Существенных проблем не обнаружено.", chunkCount)
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
		"Анализ завершён. Обработано фрагментов: %d. После фильтрации оставлено %d замечаний: %d CRITICAL, %d ERROR, %d WARNING. Основные категории: %s.",
		chunkCount,
		len(findings),
		severityCounts[domain.SeverityCritical],
		severityCounts[domain.SeverityError],
		severityCounts[domain.SeverityWarning],
		strings.Join(topCategories, ", "),
	)
}

func filterRoleFindings(findings []domain.Finding) []domain.Finding {
	grouped := make(map[string][]domain.Finding)
	roleOrder := make([]string, 0)
	for _, finding := range findings {
		if finding.Role == "" {
			finding.Role = string(domain.ReviewerRoleSolutionArchitect)
		}
		if _, exists := grouped[finding.Role]; !exists {
			roleOrder = append(roleOrder, finding.Role)
		}
		grouped[finding.Role] = append(grouped[finding.Role], finding)
	}

	filtered := make([]domain.Finding, 0, len(findings))
	for _, role := range roleOrder {
		roleFindings := deduplicateFindings(grouped[role])
		sort.SliceStable(roleFindings, func(i, j int) bool {
			left := findingScore(roleFindings[i])
			right := findingScore(roleFindings[j])
			if left == right {
				if roleFindings[i].Category == roleFindings[j].Category {
					return roleFindings[i].Problem < roleFindings[j].Problem
				}
				return roleFindings[i].Category < roleFindings[j].Category
			}
			return left > right
		})
		if len(roleFindings) > maxFindingsPerRole {
			roleFindings = roleFindings[:maxFindingsPerRole]
		}
		filtered = append(filtered, roleFindings...)
	}

	return filtered
}

func (s *AnalysisService) loadReviewConfig() (reviewconfig.Settings, error) {
	if s.reviewConfig == nil {
		return reviewconfig.Settings{
			Roles:           domain.DefaultReviewerRoles(),
			InlineComments:  true,
			SummaryComments: true,
			MemoryEnabled:   true,
			ChunkSize:       s.documentConfig.ChunkSize,
			MaxChunks:       s.documentConfig.MaxChunks,
		}, nil
	}

	settings, err := s.reviewConfig.Load()
	if err != nil {
		return reviewconfig.Settings{}, apperrors.Wrap(apperrors.KindInvalidArgument, "failed to load review config", err)
	}
	return settings, nil
}

func evaluateMergePolicy(findings []domain.Finding, settings reviewconfig.Settings) (bool, int) {
	if !settings.CriticalBlockMerge {
		return false, 0
	}

	blockingFindings := 0
	for _, finding := range findings {
		if finding.Severity == domain.SeverityCritical {
			blockingFindings++
		}
	}

	return blockingFindings > 0, blockingFindings
}

func filterFindingsByMode(findings []domain.Finding, memory domain.ReviewMemory, mode domain.AnalysisMode) ([]domain.Finding, int) {
	if mode != domain.AnalysisModeIncrementalReview || !memory.HasContext() || len(memory.KnownFindings) == 0 {
		return findings, 0
	}

	known := make(map[string]domain.Finding, len(memory.KnownFindings))
	for _, finding := range memory.KnownFindings {
		known[memoryFindingKey(finding)] = finding
	}

	filtered := make([]domain.Finding, 0, len(findings))
	suppressedCount := 0
	for _, finding := range findings {
		existing, exists := known[memoryFindingKey(finding)]
		if !exists {
			filtered = append(filtered, finding)
			continue
		}
		if findingScore(finding) > findingScore(existing) {
			filtered = append(filtered, finding)
			continue
		}
		suppressedCount++
	}

	return filtered, suppressedCount
}

func deduplicateFindings(findings []domain.Finding) []domain.Finding {
	result := make([]domain.Finding, 0, len(findings))
	seen := make(map[string]struct{}, len(findings))
	for _, finding := range findings {
		key := strings.ToLower(strings.TrimSpace(finding.Role)) + "|" +
			strings.ToLower(strings.TrimSpace(finding.Category)) + "|" +
			strings.ToLower(strings.TrimSpace(finding.Problem))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, finding)
	}
	return result
}

func memoryFindingKey(finding domain.Finding) string {
	return strings.ToLower(strings.TrimSpace(finding.Role)) + "|" +
		strings.ToLower(strings.TrimSpace(finding.Category)) + "|" +
		strings.ToLower(strings.TrimSpace(finding.Problem))
}

func findingScore(finding domain.Finding) int {
	score := 0
	switch finding.Severity {
	case domain.SeverityCritical:
		score += 400
	case domain.SeverityError:
		score += 300
	case domain.SeverityWarning:
		score += 200
	default:
		score += 100
	}

	switch finding.Category {
	case "contradiction":
		score += 40
	case "security_risk":
		score += 35
	case "technical_risk", "scalability_risk", "devops_risk":
		score += 30
	case "missing_requirement":
		score += 20
	case "ambiguity":
		score += 10
	}

	return score
}

func deriveReviewKey(source domain.DocumentSource, contextKey, externalID, name string) string {
	if trimmed := normalizeKeyPart(contextKey); trimmed != "" {
		return trimmed
	}
	if trimmed := normalizeKeyPart(externalID); trimmed != "" {
		return string(source) + ":" + trimmed
	}
	if trimmed := normalizeKeyPart(name); trimmed != "" {
		return string(source) + ":" + trimmed
	}
	return string(source) + ":unnamed_document"
}

func normalizeKeyPart(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\t", " ")
	value = strings.Join(strings.Fields(value), "_")
	return value
}

func parsedChunkDescriptor(parsed parser.ParsedDocument, idx int) parser.ParsedChunk {
	if idx >= 0 && idx < len(parsed.ChunkDescriptors) {
		return parsed.ChunkDescriptors[idx]
	}
	return parser.ParsedChunk{}
}

func toDomainSections(sections []google.Section) []domain.DocumentSection {
	result := make([]domain.DocumentSection, 0, len(sections))
	for _, section := range sections {
		result = append(result, domain.DocumentSection{
			ID:    section.ID,
			Title: section.Title,
			Level: section.Level,
			Range: domain.DocumentRange{
				StartIndex: section.Range.StartIndex,
				EndIndex:   section.Range.EndIndex,
			},
			Content: section.Content,
		})
	}
	return result
}

func toDomainBlocks(blocks []google.Block) []domain.DocumentBlock {
	result := make([]domain.DocumentBlock, 0, len(blocks))
	for _, block := range blocks {
		result = append(result, domain.DocumentBlock{
			Kind:         block.Kind,
			Text:         block.Text,
			Range:        domain.DocumentRange{StartIndex: block.Range.StartIndex, EndIndex: block.Range.EndIndex},
			HeadingLevel: block.HeadingLevel,
			ListLevel:    block.ListLevel,
			SectionID:    block.SectionID,
			SectionTitle: block.SectionTitle,
		})
	}
	return result
}
